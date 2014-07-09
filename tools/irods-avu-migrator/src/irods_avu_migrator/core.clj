(ns irods-avu-migrator.core
  (:gen-class)
  (:use [irods-avu-migrator.ipc-units]
        [irods-avu-migrator.templates])
  (:require [irods-avu-migrator.db :as db]
            [common-cli.version :as version]
            [common-cli.core :as ccli]
            [clojure.tools.cli :as cli]
            [taoensso.timbre :as timbre]
            [taoensso.timbre.appenders.rotor :as rotor]))

(def base-options
  [["-v" "--version"]
   ["-h" "--help"]
   ["-l" "--log-file PATH/TO/FILE" "The path to a file for logging."
    :default "/var/log/iplant/irods-avu-migrator.log"]])

(def db-options
  [["-d" "--db-host HOST" "The hostname for the DE database"]

   ["-b" "--db-port PORT" "The port for the DE database"
    :default "5432"]

   ["-u" "--db-user USER" "The username for the DE datbase"
    :default "de"]

   ["-n" "--db-name DB" "The name of the DE database"
    :default "de"]

   ["-m" "--db-metadata-name DB" "The name of the Metadata database"
    :default "metadata"]])

(def icat-options
  [["-i" "--icat-host HOST" "The hostname for the ICAT database"]

   ["-c" "--icat-port PORT" "The port for the ICAT database"
    :default "5432"]

   ["-a" "--icat-user USER" "The username for the ICAT database"]

   ["-t" "--icat-name DB" "The name of the ICAT database"
    :default "ICAT"]])

(def options
  (concat
   base-options
   db-options
   icat-options))

(def app-info
  {:desc "DE tool for migrating AVU metadata from iRODS to PostgreSQL"
   :app-name "irods-avu-migrator"
   :group-id "org.iplantc"
   :art-id "irods-avu-migrator"})

(defn- configure-logging
  [options]
  (timbre/set-config! [:appenders :rotor]
                      {:enabled? true
                       :async? false
                       :max-messages-per-msecs nil
                       :fn rotor/appender-fn})
  (timbre/set-config! [:shared-appender-config :rotor]
                      {:path (:log-file options)}))

(defn -main
  [& args]
  (let [cmd      (first args)
        {:keys [desc app-name group-id art-id]}    app-info
        {:keys [options arguments errors summary]} (cli/parse-opts args options)]
    (cond
     (= cmd "--version")
     (ccli/exit 0 (version/version-info group-id art-id))

     (:help options)
     (ccli/exit 0 (ccli/usage desc app-name summary))

     (:version options)
     (ccli/exit 0 (version/version-info group-id art-id))

     errors
     (ccli/exit 1 (ccli/error-msg errors))

     (not (:db-host options))
     (ccli/exit 1 "You must specify a --db-host.")

     (not (:icat-host options))
     (ccli/exit 1 "You must specify an --icat-host.")

     (not (:icat-user options))
     (ccli/exit 1 "You must specify an --icat-user"))

    (configure-logging options)
    (db/connect-dbs options)
    (convert-ipc-units options)
    (convert-template-avus options)))
