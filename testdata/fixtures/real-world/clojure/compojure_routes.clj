;; Source: https://github.com/weavejester/compojure (synthetic based on real Compojure/Ring patterns) | License: EPL-1.0

(ns myapp.routes
  (:require
   [compojure.core :refer [defroutes GET POST PUT DELETE context routes]]
   [compojure.route :as route]
   [ring.middleware.json :refer [wrap-json-body wrap-json-response]]
   [ring.middleware.defaults :refer [wrap-defaults api-defaults]]
   [ring.middleware.cors :refer [wrap-cors]]
   [ring.util.response :refer [response status created not-found]]
   [myapp.db :as db]
   [myapp.auth :as auth]
   [clojure.tools.logging :as log]))

;; ============================================================
;; Middleware
;; ============================================================

(defn wrap-auth [handler]
  (fn [request]
    (let [token (get-in request [:headers "authorization"])]
      (if-let [user (auth/verify-token token)]
        (handler (assoc request :user user))
        {:status 401
         :headers {"Content-Type" "application/json"}
         :body {:error "Unauthorized"}}))))

(defn wrap-error-handler [handler]
  (fn [request]
    (try
      (handler request)
      (catch Exception e
        (log/error e "Unhandled exception")
        {:status 500
         :headers {"Content-Type" "application/json"}
         :body {:error "Internal server error"}}))))

;; ============================================================
;; Handlers
;; ============================================================

(defn list-posts [request]
  (let [user (:user request)
        page (Integer/parseInt (get-in request [:params :page] "1"))
        per-page 20
        posts (db/find-posts {:user-id (:id user) :page page :per-page per-page})]
    (response {:posts posts :page page :per-page per-page})))

(defn get-post [request]
  (let [id (get-in request [:route-params :id])
        post (db/find-post-by-id id)]
    (if post
      (response post)
      (not-found {:error "Post not found"}))))

(defn create-post [request]
  (let [user (:user request)
        body (:body request)
        post (db/create-post! (assoc body :author-id (:id user)))]
    (-> (response post)
        (status 201))))

(defn update-post [request]
  (let [id (get-in request [:route-params :id])
        body (:body request)
        updated (db/update-post! id body)]
    (if updated
      (response updated)
      (not-found {:error "Post not found"}))))

(defn delete-post [request]
  (let [id (get-in request [:route-params :id])]
    (db/delete-post! id)
    (status (response nil) 204)))

;; ============================================================
;; Routes
;; ============================================================

(defroutes api-routes
  (context "/api" []
    (context "/v1" []
      ;; Health check — no auth required
      (GET "/health" []
        (response {:status "ok" :version "1.0.0"}))

      ;; Auth endpoints
      (POST "/auth/login" request
        (let [{:keys [email password]} (:body request)
              result (auth/login email password)]
          (if result
            (response result)
            {:status 401 :body {:error "Invalid credentials"}})))

      ;; Protected routes
      (context "" []
        (wrap-auth
         (routes
          (context "/posts" []
            (GET "/" request (list-posts request))
            (POST "/" request (create-post request))
            (context "/:id" [id]
              (GET "/" request (get-post (assoc-in request [:route-params :id] id)))
              (PUT "/" request (update-post (assoc-in request [:route-params :id] id)))
              (DELETE "/" request (delete-post (assoc-in request [:route-params :id] id)))))))))))

  (route/not-found {:error "Route not found"}))

;; ============================================================
;; Application
;; ============================================================

(def app
  (-> api-routes
      wrap-json-response
      (wrap-json-body {:keywords? true})
      wrap-error-handler
      (wrap-cors
       :access-control-allow-origin [#".*"]
       :access-control-allow-methods [:get :put :post :delete :options]
       :access-control-allow-headers ["Content-Type" "Authorization"])
      (wrap-defaults api-defaults)))
