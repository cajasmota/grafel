package engine

import (
	"testing"
)

// #4749 — Clojure producer-side route synthesis. Proves Compojure macro routes
// and Reitit data routes emit canonical http_endpoint_definition entities
// (`http:<VERB>:<canonical-path>`) in the same shape axum/Vapor/Express emit, so
// the shared resolver and the e2e route-test linker can bind to them.

// TestClojure_CompojureRoutes covers the Compojure verb-macro shape, including
// `:id` colon path params folded to `{id}`.
func TestClojure_CompojureRoutes(t *testing.T) {
	src := `(ns myapp.routes
  (:require [compojure.core :refer [defroutes GET POST PUT DELETE]]))

(defroutes app
  (GET    "/todos"      []  list-todos)
  (POST   "/todos"      req (create-todo req))
  (GET    "/todos/:id"  [id] (get-todo id))
  (DELETE "/todos/:id"  [id] (delete-todo id)))
`
	ids, _ := runDetect(t, "clojure", "routes.clj", src)
	requireContains(t, ids, []string{
		"http:GET:/todos",
		"http:POST:/todos",
		"http:GET:/todos/{id}",
		"http:DELETE:/todos/{id}",
	}, "compojure-routes")
}

// TestClojure_ReititRoutes covers the Reitit data-route shape — a vector whose
// head is a string path and whose second element is a verb→handler map.
func TestClojure_ReititRoutes(t *testing.T) {
	src := `(ns myapp.handler
  (:require [reitit.ring :as ring]))

(def app
  (ring/ring-handler
    (ring/router
      [["/users"      {:get list-users :post create-user}]
       ["/users/:id"  {:get get-user :delete delete-user}]])))
`
	ids, _ := runDetect(t, "clojure", "handler.clj", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/users",
		"http:GET:/users/{id}",
		"http:DELETE:/users/{id}",
	}, "reitit-routes")
}

// TestClojure_PedestalRoutes covers the Pedestal table-routing shape — a vector
// whose head is a string path and whose second element is the verb keyword
// (#5362). Fills the previously-empty Pedestal (0/1) routing cell.
func TestClojure_PedestalRoutes(t *testing.T) {
	src := `(ns myapp.service
  (:require [io.pedestal.http :as http]
            [io.pedestal.http.route :as route]))

(def routes
  #{["/greet"        :get  greet-handler  :route-name :greet]
    ["/users"        :post create-user]
    ["/users/:id"    :get  get-user       :route-name :get-user]
    ["/users/:id"    :delete delete-user]})
`
	ids, _ := runDetect(t, "clojure", "service.clj", src)
	requireContains(t, ids, []string{
		"http:GET:/greet",
		"http:POST:/users",
		"http:GET:/users/{id}",
		"http:DELETE:/users/{id}",
	}, "pedestal-routes")
}

// TestClojure_NonRouteStringIgnored is the negative guard: a verb-named local or
// a bare string elsewhere must not forge an endpoint.
func TestClojure_NonRouteStringIgnored(t *testing.T) {
	src := `(ns myapp.util)

(defn greet [name]
  (str "/hello " name))

(def label "POST something")
`
	ids, _ := runDetect(t, "clojure", "util.clj", src)
	for _, id := range ids {
		if id == "http:POST:/hello" || id == "http:GET:/hello" {
			t.Fatalf("non-route string forged an endpoint: %s", id)
		}
	}
}
