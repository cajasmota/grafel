(ns sample-api.handler
  "Sample Clojure handler — golden fixture source."
  (:require [clojure.string :as str]))

(def users (atom [{:id 1 :name "Alice" :email "alice@example.com"}]))
(def next-id (atom 2))

(defn find-all []
  @users)

(defn find-by-id [id]
  (first (filter #(= (:id %) id) @users)))

(defn create-user [name email]
  (let [id @next-id
        user {:id id :name name :email email}]
    (swap! next-id inc)
    (swap! users conj user)
    user))

(defn delete-user [id]
  (let [before (count @users)]
    (swap! users (fn [us] (filterv #(not= (:id %) id) us)))
    (< (count @users) before)))

(defn validate-email [email]
  (boolean (re-matches #"^[^@]+@[^@]+\.[^@]+$" email)))

(defn handle-request [method path params]
  (cond
    (and (= method :get) (= path "/health"))
    {:status 200 :body {:status "ok"}}

    (and (= method :get) (= path "/users"))
    {:status 200 :body (find-all)}

    (and (= method :post) (= path "/users"))
    (let [{:keys [name email]} params]
      (if (and name email (validate-email email))
        {:status 201 :body (create-user name email)}
        {:status 400 :body {:error "invalid params"}}))

    :else
    {:status 404 :body {:error "not found"}}))
