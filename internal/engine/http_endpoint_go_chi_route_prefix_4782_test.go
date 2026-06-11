package engine

import "testing"

// #4782 — chi `r.Route("/api", func(r chi.Router){ ... })` is a CLOSURE-based
// sub-router prefix mount (distinct from gin/echo `.Group(`). Before this fix
// synthesizeGoRouters dropped the chi sub-router prefix and emitted the route's
// own path (`/users`) instead of the fully-nested `/api/v1/users`.
//
// These tests reuse the collectGoRoutes / assertRoute helpers from the #4408
// group-prefix test file (same package).

// Fixture A: single chi Route closure → route prefixed with the mount path.
func TestChiRoutePrefix4782_SingleRoute(t *testing.T) {
	src := `package main

import "github.com/go-chi/chi/v5"

func setup() {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Get("/users", listUsers)
	})
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /api/users")
	assertNoRoute(t, routes, "GET /users")
}

// Fixture B: nested chi Route closures → transitively composed prefix.
func TestChiRoutePrefix4782_NestedRoute(t *testing.T) {
	src := `package main

import "github.com/go-chi/chi/v5"

func setup() {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/users", listUsers)
		})
	})
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /api/v1/users")
	assertNoRoute(t, routes, "GET /users")
	assertNoRoute(t, routes, "GET /v1/users")
}

// Fixture C: a route OUTSIDE the Route closure keeps its own (unprefixed) path,
// while a sibling route inside is prefixed — proves the prefix is scoped to the
// closure body span, not the whole file.
func TestChiRoutePrefix4782_ScopedToClosure(t *testing.T) {
	src := `package main

import "github.com/go-chi/chi/v5"

func setup() {
	r := chi.NewRouter()
	r.Get("/health", healthCheck)
	r.Route("/api", func(r chi.Router) {
		r.Post("/login", loginHandler)
	})
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /health")
	assertRoute(t, routes, "POST /api/login")
	assertNoRoute(t, routes, "POST /login")
}

// Fixture D: sibling (non-nested) Route closures do not bleed prefixes into
// each other.
func TestChiRoutePrefix4782_SiblingClosures(t *testing.T) {
	src := `package main

import "github.com/go-chi/chi/v5"

func setup() {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Get("/users", listUsers)
	})
	r.Route("/admin", func(r chi.Router) {
		r.Get("/stats", adminStats)
	})
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /api/users")
	assertRoute(t, routes, "GET /admin/stats")
	assertNoRoute(t, routes, "GET /api/stats")
	assertNoRoute(t, routes, "GET /admin/users")
}

// Fixture E: arbitrary (non-`r`) closure param name still resolves, since the
// prefix is positional, not name-keyed.
func TestChiRoutePrefix4782_ArbitraryParamName(t *testing.T) {
	src := `package main

import "github.com/go-chi/chi/v5"

func setup() {
	router := chi.NewRouter()
	router.Route("/api", func(sub chi.Router) {
		sub.Get("/ping", pingHandler)
	})
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /api/ping")
	assertNoRoute(t, routes, "GET /ping")
}
