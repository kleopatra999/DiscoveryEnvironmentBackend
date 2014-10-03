// condor-log-monitor
//
// Tails the configured EVENT_LOG on a condor submission node, parses events from it,
// and pushes events out to an AMQP broker.
//
// condor-log-monitor will make an attempt at detecting rolling over log files and
// recovering from extended downtime, but whether or not a full recovery is possible
// depends on how the Condor logging is configured. It is still possible to lose
// messages if condor-log-monitor is down for a while and Condor rotates files
// out too many times.
//
// Condor attempts to recover from downtime by recording a tombstone file that
// records the inode number, last modified date, processing date, and last
// processed position. At start up clm will look for the tombstoned file and will
// attempt to start processing from that point forward.
//
// If the inode of the new file doesn't match the inode contained in the tombstone,
// then scan the directory for all of the old log files and collect their inodes
// and last modified dates. Sort the old log files from oldest to newest -- based
// on the last modified date -- and iterate through them. Find the file that matches
// the inode of the file from the tombstone and process it starting from the position
// recorded in the tombstone. Then, process all of remaining files until you reach
// the current log file. Process the current log file and record a new tombstone.
// Do not delete the old tombstone until you're ready to record a new one.
//
// If condor-log-monitor has been down for so long that the tombstoned log file no
// longer exists, process all of the log file in order from oldest to newest. Record
// a new tombstone when you reach the end of the newest log file.

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ActiveState/tail"
	"github.com/streadway/amqp"
)

var (
	cfgPath = flag.String("config", "", "Path to the config file.")
	logPath = flag.String("event-log", "", "Path to the log file.")
)

func init() {
	flag.Parse()
}

// Configuration contains the setting read from a config file.
type Configuration struct {
	EventLog                               string
	AMQPURI                                string
	ExchangeName, ExchangeType, RoutingKey string
	Durable, Autodelete, Internal, NoWait  bool
}

// ReadConfig reads JSON from 'path' and returns a pointer to a Configuration
// instance. Hopefully.
func ReadConfig(path string) (*Configuration, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fileInfo.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fileData, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	var config Configuration
	err = json.Unmarshal(fileData, &config)
	if err != nil {
		return &config, err
	}
	return &config, nil
}

// AMQPPublisher contains the state information for a connection to an AMQP
// broker that is capable of publishing data to an exchange.
type AMQPPublisher struct {
	URI          string
	ExchangeName string
	ExchangeType string
	RoutingKey   string
	Durable      bool
	Autodelete   bool
	Internal     bool
	NoWait       bool
	connection   *amqp.Connection
	channel      *amqp.Channel
}

// NewAMQPPublisher creates a new instance of AMQPPublisher and returns a
// pointer to it. The connection is not established at this point.
func NewAMQPPublisher(cfg *Configuration) *AMQPPublisher {
	return &AMQPPublisher{
		URI:          cfg.AMQPURI,
		ExchangeName: cfg.ExchangeName,
		ExchangeType: cfg.ExchangeType,
		RoutingKey:   cfg.RoutingKey,
		Durable:      cfg.Durable,
		Autodelete:   cfg.Autodelete,
		Internal:     cfg.Internal,
		NoWait:       cfg.NoWait,
	}
}

// ConnectionErrorChan is used to send error channels to goroutines.
type ConnectionErrorChan struct {
	channel chan *amqp.Error
}

// Connect will attempt to connect to the AMQP broker, create/use the configured
// exchange, and create a new channel. Make sure you call the Close method when
// you are done, most likely with a defer statement.
func (p *AMQPPublisher) Connect(errorChan chan ConnectionErrorChan) error {
	connection, err := amqp.Dial(p.URI)
	if err != nil {
		return err
	}
	p.connection = connection

	channel, err := p.connection.Channel()
	if err != nil {
		return err
	}

	err = channel.ExchangeDeclare(
		p.ExchangeName,
		p.ExchangeType,
		p.Durable,
		p.Autodelete,
		p.Internal,
		p.NoWait,
		nil, //arguments
	)
	if err != nil {
		return err
	}
	p.channel = channel
	errors := p.connection.NotifyClose(make(chan *amqp.Error))
	msg := ConnectionErrorChan{
		channel: errors,
	}
	errorChan <- msg
	return nil
}

// SetupReconnection fires up a goroutine that listens for Close() errors and
// reconnects to the AMQP server if they're encountered.
func (p *AMQPPublisher) SetupReconnection(errorChan chan ConnectionErrorChan) {
	//errors := p.connection.NotifyClose(make(chan *amqp.Error))
	go func() {
		var exitChan chan *amqp.Error
		reconfig := true
		for {
			if reconfig {
				msg := <-errorChan
				exitChan = msg.channel
			}
			select {
			case exitError, ok := <-exitChan:
				if !ok {
					log.Println("Exit channel closed.")
					reconfig = true
				} else {
					log.Println(exitError)
					p.Connect(errorChan)
					reconfig = false
				}
			}
		}
	}()
}

// PublishString sends the body off to the configured AMQP exchange.
func (p *AMQPPublisher) PublishString(body string) error {
	return p.PublishBytes([]byte(body))
}

// PublishBytes sends off the bytes to the AMQP broker.
func (p *AMQPPublisher) PublishBytes(body []byte) error {
	if err := p.channel.Publish(
		p.ExchangeName,
		p.RoutingKey,
		false, //mandatory?
		false, //immediate?
		amqp.Publishing{
			Headers:         amqp.Table{},
			ContentType:     "text/plain",
			ContentEncoding: "",
			Body:            body,
			DeliveryMode:    amqp.Transient,
			Priority:        0,
		},
	); err != nil {
		return err
	}
	return nil
}

// Close calls Close() on the underlying AMQP connection.
func (p *AMQPPublisher) Close() {
	p.connection.Close()
}

// PublishableEvent is a type that contains the information that gets sent to
// the AMQP broker. It's meant to be marshalled into JSON or some other format.
type PublishableEvent struct {
	Event string
	Hash  string
}

// NewPublishableEvent creates returns a pointer to a newly created instance
// of PublishableEvent.
func NewPublishableEvent(event string) *PublishableEvent {
	hashBytes := sha256.Sum256([]byte(event))
	return &PublishableEvent{
		Event: event,
		Hash:  string(hashBytes[:]),
	}
}

// ParseEvent will tail a file and print out each event as it comes through.
// The AMQPPublisher that is passed in should already have its connection
// established. This function does not call Close() on it.
func ParseEvent(filepath string, pub *AMQPPublisher) error {
	startRegex := "^[\\d][\\d][\\d]\\s.*"
	endRegex := "^\\.\\.\\..*"
	foundStart := false
	var eventlines string //accumulates lines in an event entry

	t, err := tail.TailFile(filepath, tail.Config{
		ReOpen: true,
		Follow: true,
		Poll:   true,
	})
	for line := range t.Lines {
		text := line.Text
		if !foundStart {
			matchedStart, err := regexp.MatchString(startRegex, text)
			if err != nil {
				return err
			}
			if matchedStart {
				foundStart = true
				eventlines = eventlines + text + "\n"
				if err != nil {
					return err
				}
			}
		} else {
			matchedEnd, err := regexp.MatchString(endRegex, text)
			if err != nil {
				return err
			}
			eventlines = eventlines + text + "\n"
			if matchedEnd {
				fmt.Println(eventlines)
				pubEvent := NewPublishableEvent(eventlines)
				pubJSON, err := json.Marshal(pubEvent)
				if err != nil {
					return err
				}
				if err = pub.PublishBytes(pubJSON); err != nil {
					fmt.Println(err)
				}
				eventlines = ""
				foundStart = false
			}
		}
	}
	return err
}

// ParseEventFile parses an entire file and sends it to the AMQP broker.
func ParseEventFile(filepath string, pub *AMQPPublisher) error {
	startRegex := "^[\\d][\\d][\\d]\\s.*"
	endRegex := "^\\.\\.\\..*"
	foundStart := false
	var eventlines string //accumulates lines in an event entry

	openFile, err := os.Open(filepath)
	if err != nil {
		return err
	}
	var prefixBuffer []byte
	reader := bufio.NewReader(openFile)
	for {
		line, prefix, err := reader.ReadLine()
		if err != nil {
			break
		}
		if prefix { //a partial line was read.
			prefixBuffer = line
			continue
		}
		if len(prefixBuffer) > 0 {
			line = append(prefixBuffer, line...) //concats line onto the prefix
		}
		prefixBuffer = []byte{} //reset the prefix for later iterations
		text := string(line[:])
		if !foundStart {
			matchedStart, err := regexp.MatchString(startRegex, text)
			if err != nil {
				return err
			}
			if matchedStart {
				foundStart = true
				eventlines = eventlines + text + "\n"
				if err != nil {
					return err
				}
			}
		} else {
			matchedEnd, err := regexp.MatchString(endRegex, text)
			if err != nil {
				return err
			}
			eventlines = eventlines + text + "\n"
			if matchedEnd {
				fmt.Println(eventlines)
				pubEvent := NewPublishableEvent(eventlines)
				pubJSON, err := json.Marshal(pubEvent)
				if err != nil {
					return err
				}
				if err = pub.PublishBytes(pubJSON); err != nil {
					fmt.Println(err)
				}
				eventlines = ""
				foundStart = false
			}
		}
	}
	return err
}

//TombstoneAction denotes the kind of action a TombstoneMsg represents.
type TombstoneAction int

const (
	//Set says that the TombstoneMsg contains a set action.
	Set TombstoneAction = iota

	//Get says that the TombstoneMsg contains a get action.
	Get

	//Quit says that the TombstoneMsg contains a quit action.
	Quit
)

//TombstoneMsg represents a message sent to a goroutine that processes tombstone
//related operations. The Data field contains information that the tombstone
//goroutine may take action on, depending on the Action. Set messages will set
//the current value of the tombstone to the value in the Data field. Get messages
//will return the current value of the tombstone on the Reply channel. Quit
//messages tell the goroutine to shut down as cleanly as possible. The Reply
//channel may be used on certain operations to pass back data from the goroutine
//in response to a received TombstoneMsg.
type TombstoneMsg struct {
	Action TombstoneAction
	Data   Tombstone
	Reply  chan interface{}
}

// Tombstone is a type that contains the information stored in a tombstone file.
// It tracks the current position, last modified data, and inode number of the
// log file that was parsed and the date that the tombstone was created.
type Tombstone struct {
	CurrentPos int64
	Date       time.Time
	LogLastMod time.Time
	Inode      uint64
}

// TombstonePath is the path to the tombstone file.
const TombstonePath = "/tmp/condor-log-monitor.tombstone"

// TombstoneExists returns true if the tombstone file is present.
func TombstoneExists() bool {
	_, err := os.Stat(TombstonePath)
	if err != nil {
		return false
	}
	return true
}

// InodeFromPath will return the inode number for the given path.
func InodeFromPath(path string) (uint64, error) {
	openFile, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	ino, err := InodeFromFile(openFile)
	if err != nil {
		return 0, err
	}
	return ino, nil
}

// InodeFromFile will return the inode number for the opened file.
func InodeFromFile(openFile *os.File) (uint64, error) {
	fileInfo, err := openFile.Stat()
	if err != nil {
		return 0, err
	}
	sys := fileInfo.Sys().(*syscall.Stat_t)
	return sys.Ino, nil
}

// InodeFromFileInfo will return the inode number from the provided FileInfo
// instance.
func InodeFromFileInfo(info *os.FileInfo) uint64 {
	i := *info
	sys := i.Sys().(*syscall.Stat_t)
	return sys.Ino
}

// NewTombstoneFromPath will create a *Tombstone for the provided path.
func NewTombstoneFromPath(path string) (*Tombstone, error) {
	openFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	tombstone, err := NewTombstoneFromFile(openFile)
	if err != nil {
		return nil, err
	}
	return tombstone, nil
}

// NewTombstoneFromFile will create a *Tombstone from an open file.
func NewTombstoneFromFile(openFile *os.File) (*Tombstone, error) {
	fileInfo, err := openFile.Stat()
	if err != nil {
		return nil, err
	}
	inode, err := InodeFromFile(openFile)
	if err != nil {
		return nil, err
	}
	currentPos, err := openFile.Seek(0, os.SEEK_CUR)
	if err != nil {
		return nil, err
	}
	tombstone := &Tombstone{
		CurrentPos: currentPos,
		Date:       time.Now(),
		LogLastMod: fileInfo.ModTime(),
		Inode:      inode,
	}
	return tombstone, nil
}

// WriteToFile will persist the Tombstone to a file.
func (t *Tombstone) WriteToFile() error {
	tombstoneJSON, err := json.Marshal(t)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(TombstonePath, tombstoneJSON, 0644)
	return err
}

// UnmodifiedTombstone is the tombstone as it was read from the JSON in the
// tombstone file. It hasn't been turned into an actual Tombstone instance yet
// because some of the fields need to be manually converted to a different type.
type UnmodifiedTombstone struct {
	CurrentPos int64
	Date       string
	LogLastMod string
	Inode      uint64
}

// Convert returns a *Tombstone based on the values contained in the
// UnmodifiedTombstone.
func (u *UnmodifiedTombstone) Convert() (*Tombstone, error) {
	parsedDate, err := time.Parse(time.RFC3339Nano, u.Date)
	if err != nil {
		return nil, err
	}
	parsedLogLastMod, err := time.Parse(time.RFC3339, u.LogLastMod)
	if err != nil {
		return nil, err
	}
	tombstone := &Tombstone{
		CurrentPos: u.CurrentPos,
		Date:       parsedDate,
		LogLastMod: parsedLogLastMod,
		Inode:      u.Inode,
	}
	return tombstone, nil
}

// ReadTombstone will read a marshalled tombstone from a file and return a
// pointer to it.
func ReadTombstone() (*Tombstone, error) {
	contents, err := ioutil.ReadFile(TombstonePath)
	if err != nil {
		return nil, err
	}
	var t *UnmodifiedTombstone
	err = json.Unmarshal(contents, &t)
	if err != nil {
		return nil, err
	}
	tombstone, err := t.Convert()
	if err != nil {
		return nil, err
	}
	return tombstone, nil
}

// Logfile contains a pointer to a os.FileInfo instance and the base directory
// for a particular log file.
type Logfile struct {
	Info    os.FileInfo
	BaseDir string
}

// LogfileList contains a list of Logfiles.
type LogfileList []Logfile

// NewLogfileList returns a list of FileInfo instances to files that start with
// the name of the configured log file.
func NewLogfileList(dir string, logname string) (LogfileList, error) {
	startingList, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var filtered []Logfile
	for _, fi := range startingList {
		if strings.HasPrefix(fi.Name(), logname) {
			lf := Logfile{
				Info:    fi,
				BaseDir: dir,
			}
			filtered = append(filtered, lf)
		}
	}
	filtered = LogfileList(filtered)
	return filtered, nil
}

func (l LogfileList) Len() int {
	return len(l)
}

func (l LogfileList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l LogfileList) Less(i, j int) bool {
	re := regexp.MustCompile("\\.\\d+$") //extracts the suffix with a leading '.'
	logfile1 := l[i].Info
	logfile2 := l[j].Info
	logname1 := logfile1.Name()
	logname2 := logfile2.Name()
	match1 := re.Find([]byte(logname1))
	match2 := re.Find([]byte(logname2))

	//filenames without a suffix are effectively equal
	if match1 == nil && match2 == nil {
		return false
	}

	//filenames without a suffix have a lower value than basically anything.
	//this means that the most current log file will get processed last if
	//the monitor has been down for a while.
	if match1 == nil && match2 != nil {
		return false
	}

	//again, filenames without a suffix have a lower value than files with a
	//suffix.
	if match1 != nil && match2 == nil {
		return true
	}

	//the suffix is assumed to be a number. if it's not it has a lower value.
	match1int, err := strconv.Atoi(string(match1[1:])) //have to drop the '.'
	if err != nil {
		return false
	}

	//the suffix is assumed to be a number again. if it doesn't it's assumed to
	//have a lower value.
	match2int, err := strconv.Atoi(string(match2[1:])) //have to drop the '.'
	if err != nil {
		return true
	}

	return match1int > match2int
}

// SliceByInode trims the LogfileList by looking for the log file that has the
// matching inode and returning a list of log files that starts at that point.
func (l LogfileList) SliceByInode(inode uint64) LogfileList {
	foundIdx := 0
	for idx, logfile := range l {
		fiInode := InodeFromFileInfo(&logfile.Info)
		if fiInode == inode {
			foundIdx = idx
			break
		}
	}
	return l[foundIdx:]
}

/*
On start up, look for tombstone and read it if it's present.
List the log files.
Sort the log files.
Trim the list based on the tombstoned inode number.
If the inode is not present in the list, trim the list based on last-modified date.
If all of the files were modified after the recorded last-modified date, then parse
and send all of the files.
After all of the files are parsed, record a new tombstone and tail the latest log file, looking for updates.
*/
func main() {
	if *cfgPath == "" {
		fmt.Printf("--config must be set.")
		os.Exit(-1)
	}
	cfg, err := ReadConfig(*cfgPath)
	if err != nil {
		fmt.Println(err)
	}
	errChan := make(chan ConnectionErrorChan)
	pub := NewAMQPPublisher(cfg)
	pub.SetupReconnection(errChan)
	if err = pub.Connect(errChan); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}

	// reading from errChan should prevent the application from exiting before
	// it should.
	exitChan := make(chan int)
	go func() {
		// First, we need to read the tombstone file if it exists.
		var tombstone *Tombstone
		if TombstoneExists() {
			log.Printf("Attempting to read tombstone from %s\n", TombstonePath)
			tombstone, err = ReadTombstone()
			if err != nil {
				log.Println("Couldn't read Tombstone file.")
				log.Println(err)
				tombstone = nil
			}
			log.Printf("Done reading tombstone file from %s\n", TombstonePath)
		} else {
			tombstone = nil
		}
		logDir := filepath.Dir(cfg.EventLog)
		log.Printf("Log directory: %s\n", logDir)
		logFilename := filepath.Base(cfg.EventLog)
		log.Printf("Log filename: %s\n", logFilename)

		// Now we need to find all of the rotated out log files and parse them for
		// potentially missed updates.
		logList, err := NewLogfileList(logDir, logFilename)
		if err != nil {
			fmt.Println("Couldn't get list of log files.")
			logList = LogfileList{}
		}

		// We need to sort the rotated log files in order from oldest to newest.
		sort.Sort(logList)

		// If there aren't any rotated log files or a tombstone file, then there
		// isn't a reason to truncate the list of rotated log files. Hopefully, we'd
		// trim the list of log files to prevent reprocessing, which could save us
		// a significant amount of time at start up.
		if len(logList) > 0 && tombstone != nil {
			log.Printf("Slicing log list by inode number %d\n", tombstone.Inode)
			logList = logList.SliceByInode(tombstone.Inode)
		}

		// Iterate through the list of log files, parse them, and ultimately send the
		// events out to the AMQP broker. Skip the latest log file, we'll be handling
		// that further down.
		for _, logFile := range logList {
			if logFile.Info.Name() == logFilename { //the current log file will get parsed later
				continue
			}
			logfilePath := path.Join(logFile.BaseDir, logFile.Info.Name())
			log.Printf("Parsing %s\n", logfilePath)
			err = ParseEventFile(logfilePath, pub)
			if err != nil {
				fmt.Println(err)
			}
		}

		// Okay, we're done with the start up processing part at this point. Now we're
		// at the part where clm will spend most of its time, tailing the log and
		// emitting events.
		log.Println("Done parsing event logs.")
		err = ParseEvent(cfg.EventLog, pub)
		if err != nil {
			fmt.Println(err)
		}
		exitChan <- 1
	}()

	fmt.Println(cfg)
	<-exitChan
}
