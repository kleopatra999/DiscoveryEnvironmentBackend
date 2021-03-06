(use '[clojure.java.shell :only (sh)])
(require '[clojure.string :as string])

(defn git-ref
  []
  (or (System/getenv "GIT_COMMIT")
      (string/trim (:out (sh "git" "rev-parse" "HEAD")))
      ""))

(defproject org.iplantc/donkey "5.0.0-SNAPSHOT"
  :description "Framework for hosting DiscoveryEnvironment metadata services."
  :url "https://github.com/iPlantCollaborativeOpenSource/Donkey"
  :license {:name "BSD Standard License"
            :url "http://www.iplantcollaborative.org/sites/default/files/iPLANT-LICENSE.txt"}
  :manifest {"Git-Ref" ~(git-ref)}
  :uberjar-name "donkey-standalone.jar"
  :dependencies [[org.clojure/clojure "1.6.0"]
                 [org.clojure/core.memoize "0.5.7"]
                 [org.clojure/data.codec "0.1.0"]
                 [org.clojure/java.classpath "0.2.2"]
                 [byte-streams "0.2.0"]
                 [org.apache.tika/tika-core "1.8"]
                 [org.iplantc/authy "5.0.0"]
                 [org.iplantc/clj-cas "5.0.0"]
                 [org.iplantc/clj-jargon "5.0.0"
                  :exclusions [[xerces/xmlParserAPIs]
                               [org.slf4j/slf4j-api]
                               [org.slf4j/slf4j-log4j12]
                               [log4j]]]
                 [org.iplantc/service-logging "5.0.0"]
                 [org.iplantc/clojure-commons "5.0.0"]
                 [org.iplantc/kameleon "5.0.0"]
                 [org.iplantc/heuristomancer "5.0.0"]
                 [org.iplantc/clj-icat-direct "5.0.0"]
                 [org.iplantc/common-cli "5.0.0"]
                 [org.iplantc/common-cfg "5.0.0"]
                 [org/forester "1.005" ]
                 [org.nexml.model/nexml "1.5-SNAPSHOT"]
                 [cheshire "5.4.0"]
                 [clj-http "1.1.1"]
                 [clj-time "0.9.0"]
                 [com.cemerick/url "0.1.1"]
                 [ring "1.3.2"]
                 [compojure "1.3.3"]
                 [clojurewerkz/elastisch "2.1.0"]
                 [com.fasterxml.jackson.core/jackson-core "2.5.1"]
                 [com.fasterxml.jackson.core/jackson-databind "2.5.1"]
                 [com.fasterxml.jackson.core/jackson-annotations "2.5.1"]
                 [com.novemberain/welle "3.0.0"]
                 [commons-net "3.3"]
                 [org.clojure/tools.nrepl "0.2.10"]
                 [net.sf.opencsv/opencsv "2.3"]
                 [de.ubercode.clostache/clostache "1.4.0"]
                 [me.raynes/fs "1.4.6"]
                 [medley "0.6.0"]
                 [dire "0.5.3"]
                 [prismatic/schema "0.4.1"]
                 [slingshot "0.12.2"]]
  :plugins [[org.iplantc/lein-iplant-rpm "5.0.0"]
            [lein-ring "0.8.8"]
            [swank-clojure "1.4.2"]]
  :profiles {:dev     {:resource-paths ["conf/test"]}
             :uberjar {:aot :all}}
  :main ^:skip-aot donkey.core
  :ring {:handler donkey.core/app
         :init donkey.core/lein-ring-init
         :port 31325
         :auto-reload? false}
  :iplant-rpm {:summary "iPlant Discovery Environment Business Layer Services"
               :provides "donkey"
               :dependencies ["iplant-service-config >= 0.1.0-5" "java-1.7.0-openjdk"]
               :exe-files ["resources/scripts/filetypes/guess-2.pl"]
               :config-files ["log4j2.xml"]
               :config-path "conf/main"}
  :uberjar-exclusions [#".*[.]SF" #"LICENSE" #"NOTICE"]
  :repositories [["sonatype-nexus-snapshots"
                  {:url "https://oss.sonatype.org/content/repositories/snapshots"}]
                 ["biojava"
                  {:url "http://www.biojava.org/download/maven"}]
                 ["nexml"
                  {:url "http://nexml-dev.nescent.org/.m2/repository"
                   :checksum :ignore
                   :update :never}]])
