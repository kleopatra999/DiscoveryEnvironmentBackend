(use '[clojure.java.shell :only (sh)])
(require '[clojure.string :as string])

(defn git-ref
  []
  (or (System/getenv "GIT_COMMIT")
      (string/trim (:out (sh "git" "rev-parse" "HEAD")))
      ""))

(defproject org.iplantc/saved-searches "5.0.0"
  :description "DE API for managing saved searches."
  :url "https://github.com/iPlantCollaborativeOpenSource/DiscoveryEnvironmentBackend"
  :license {:name "BSD"}
  :manifest {"Git-Ref" ~(git-ref)}
  :aot [saved-searches.core]
  :main saved-searches.core
  :uberjar-name "saved-searches-standalone.jar"
  :dependencies [[org.clojure/clojure "1.5.1"]
                 [org.iplantc/common-cli "5.0.0"]
                 [org.iplantc/common-cfg "5.0.0"]
                 [org.iplantc/kameleon "5.0.0"]
                 [org.iplantc/service-logging "5.0.0"]
                 [io.aviso/pretty "0.1.17"]
                 [me.raynes/fs "1.4.6"]
                 [cheshire "5.3.1"
                   :exclusions [[com.fasterxml.jackson.dataformat/jackson-dataformat-cbor]
                                [com.fasterxml.jackson.dataformat/jackson-dataformat-smile]
                                [com.fasterxml.jackson.core/jackson-annotations]
                                [com.fasterxml.jackson.core/jackson-databind]
                                [com.fasterxml.jackson.core/jackson-core]]]
                 [compojure "1.1.6"]
                 [midje "1.6.3"]
                 [ring "1.2.1"]
                 [ring/ring-json "0.3.1"]]
  :iplant-rpm {:summary "DE API for managing saved searches."
               :provides "saved-searches"
               :dependencies ["iplant-service-config >= 0.1.0-5" "java-1.7.0-openjdk"]}
  :plugins [[lein-ring "0.8.10"]
            [lein-midje "3.1.1"]
            [org.iplantc/lein-iplant-rpm "5.0.0"]])
