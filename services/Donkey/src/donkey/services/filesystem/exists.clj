(ns donkey.services.filesystem.exists
  (:use [clojure-commons.validators]
        [donkey.services.filesystem.common-paths]
        [donkey.services.filesystem.validators]
        [clj-jargon.init :only [with-jargon]]
        [clj-jargon.item-info :only [exists?]])
  (:require [clj-http.client :as http]
            [clojure-commons.file-utils :as ft]
            [cheshire.core :as json]
            [cemerick.url :as url]
            [dire.core :refer [with-pre-hook! with-post-hook!]]
            [donkey.util.config :as cfg]
            [donkey.services.filesystem.icat :as icat]))


(defn- url-encoded?
  [string-to-check]
  (re-seq #"\%[A-Fa-f0-9]{2}" string-to-check))

(defn- url-decode
  [string-to-decode]
  (if (url-encoded? string-to-decode)
    (url/url-decode string-to-decode)
    string-to-decode))

(defn path-exists?
  ([path]
     (path-exists? "" path))
  ([user path]
    (let [path (ft/rm-last-slash path)]
      (with-jargon (icat/jargon-cfg) [cm]
        (exists? cm (url-decode path))))))


(defn do-exists
  [params body]
  (let [url     (url/url (cfg/data-info-base) "existence-marker")
        req-map {:query-params (select-keys params [:user])
                 :content-type :json
                 :body         (json/encode body)}]
    (-> (http/post (str url) req-map)
      :body
      json/decode
      (select-keys ["paths"]))))

(with-pre-hook! #'do-exists
  (fn [params body]
    (log-call "do-exists" params)
    (validate-map params {:user string?})
    (validate-map body {:paths vector?})
    (validate-num-paths (:paths body))))

(with-post-hook! #'do-exists (log-func "do-exists"))
