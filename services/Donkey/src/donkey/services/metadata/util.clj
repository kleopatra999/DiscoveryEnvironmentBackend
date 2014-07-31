(ns donkey.services.metadata.util
  (:use [donkey.auth.user-attributes :only [current-user]])
  (:require [clj-time.core :as t]
            [clj-time.format :as tf]
            [clojure.string :as string]
            [clojure.tools.logging :as log]
            [clojure-commons.file-utils :as ft]
            [donkey.clients.notifications :as dn]
            [donkey.clients.user-info :as du]
            [donkey.util.db :as db]))

(def failed-status "Failed")
(def completed-status "Completed")
(def submitted-status "Submitted")
(def idle-status "Idle")
(def running-status "Running")
(def completed-status-codes #{failed-status completed-status})

(defn- current-timestamp
  []
  (tf/unparse (tf/formatter "yyyy-MM-dd-HH-mm-ss.S") (t/now)))

(defn is-completed?
  [job-status]
  (completed-status-codes job-status))

(def not-completed? (complement is-completed?))

(defn- job-name-to-path
  "Converts a job name to a string suitable for inclusion in a path."
  [path]
  (string/replace path #"[\s@]" "_"))

(defn build-result-folder-path
  [submission]
  (let [build-path (comp ft/rm-last-slash ft/path-join)]
    (if (:create_output_subdir submission true)
      (build-path (:outputDirectory submission)
                  (str (job-name-to-path (:name submission)) "-" (current-timestamp)))
      (build-path (:outputDirectory submission)))))

(defn update-submission-result-folder
  [submission result-folder-path]
  (assoc submission
    :outputDirectory      result-folder-path
    :create_output_subdir false))

(defn- job-timestamp
  [timestamp]
  (str (or (db/millis-from-timestamp timestamp) 0)))

(defn format-job
  [app-tables job]
  {:analysis_details (:app-description job)
   :analysis_id      (:app-id job)
   :analysis_name    (:app-name job)
   :description      (:description job)
   :enddate          (job-timestamp (:end-date job))
   :id               (:id job)
   :name             (:job-name job)
   :resultfolderid   (:result-folder-path job)
   :startdate        (job-timestamp (:start-date job))
   :status           (:status job)
   :username         (:username job)
   :deleted          (:deleted job)
   :wiki_url         (:app-wiki-url job)
   :app_disabled     (:disabled (first (remove nil? (map #(% (:app-id job)) app-tables))))})

(defn- get-email-address
  [username]
  (if current-user
    (:email current-user)
    (:email (du/get-user-details username))))

(defn send-job-status-notification
  "Sends a job status change notification."
  [{:keys [username startdate] :as job} job-step status end-time]
  (let [username     (string/replace username #"@.*" "")
        end-millis   (db/timestamp-str end-time)
        start-millis (db/timestamp-str startdate)
        email        (get-email-address username)]
    (dn/send-job-status-update username email (assoc (format-job [] job)
                                                :status    status
                                                :enddate   end-millis
                                                :startdate start-millis))))