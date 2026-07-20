package main

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestAudit2851_JSBackendRouting is the real-data integration guard for #2851.
// It indexes a multi-framework backend-HTTP corpus (AdonisJS, Hapi, Feathers,
// Marble.js, Polka, Restify, Sails) through the full indexer pipeline — not a
// single-file unit fixture — and asserts:
//
//   - endpoint_synthesis: every framework's routes surface as
//     http_endpoint_definition entities with the canonical verb+path.
//   - handler_attribution: where the framework references a NAMED handler in a
//     separate file (Polka, Restify, Hapi), the endpoint's source_file is
//     rebound to the handler file by the resolve pass — not left at the
//     registration site.
func TestAudit2851_JSBackendRouting(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2851_js", "audit2851_js", nil)

	endpoints := collectHTTPEndpointDefs(doc)
	if len(endpoints) == 0 {
		t.Fatalf("audit2851_js: no http_endpoint_definition entities emitted")
	}

	requireEndpoint := func(t *testing.T, fw, verb, path string) {
		t.Helper()
		if ep := findEndpointBySuffix(endpoints, verb, path); ep == nil {
			t.Errorf("%s: missing endpoint %s %s", fw, verb, path)
		}
	}

	t.Run("AdonisJS", func(t *testing.T) {
		requireEndpoint(t, "adonisjs", "GET", "/users")
		requireEndpoint(t, "adonisjs", "POST", "/users")
		requireEndpoint(t, "adonisjs", "GET", "/users/{id}")
		// Route.resource('posts', ...) expansion.
		requireEndpoint(t, "adonisjs", "GET", "/posts")
		requireEndpoint(t, "adonisjs", "POST", "/posts")
		requireEndpoint(t, "adonisjs", "GET", "/posts/{id}")
		requireEndpoint(t, "adonisjs", "PUT", "/posts/{id}")
		requireEndpoint(t, "adonisjs", "DELETE", "/posts/{id}")
	})

	t.Run("Hapi", func(t *testing.T) {
		requireEndpoint(t, "hapi", "GET", "/books")
		requireEndpoint(t, "hapi", "GET", "/books/{id}")
		// Named handlers (listBooks / getBook) live in hapi/handlers.ts —
		// handler_attribution must rebind source_file there.
		if ep := findEndpointBySuffix(endpoints, "GET", "/books"); ep != nil {
			assertHandlerFile(t, ep, "hapi/handlers.ts")
		}
	})

	t.Run("Feathers", func(t *testing.T) {
		requireEndpoint(t, "feathers", "GET", "/messages")
		requireEndpoint(t, "feathers", "POST", "/messages")
		requireEndpoint(t, "feathers", "GET", "/messages/{id}")
		requireEndpoint(t, "feathers", "PUT", "/messages/{id}")
		requireEndpoint(t, "feathers", "PATCH", "/messages/{id}")
		requireEndpoint(t, "feathers", "DELETE", "/messages/{id}")
	})

	t.Run("Marble", func(t *testing.T) {
		requireEndpoint(t, "marblejs", "GET", "/users")
		requireEndpoint(t, "marblejs", "GET", "/users/{id}")
		requireEndpoint(t, "marblejs", "POST", "/users")
	})

	t.Run("Polka", func(t *testing.T) {
		requireEndpoint(t, "polka", "GET", "/users")
		requireEndpoint(t, "polka", "GET", "/users/{id}")
		requireEndpoint(t, "polka", "POST", "/users")
		// listUsers / getUser / createUser live in polka/handlers.ts.
		if ep := pickEndpointFromFramework(endpoints, "polka", "GET", "/users"); ep != nil {
			assertHandlerFile(t, ep, "polka/handlers.ts")
		}
	})

	t.Run("Restify", func(t *testing.T) {
		requireEndpoint(t, "restify", "GET", "/items")
		requireEndpoint(t, "restify", "GET", "/items/{id}")
		requireEndpoint(t, "restify", "DELETE", "/items/{id}")
		if ep := pickEndpointFromFramework(endpoints, "restify", "GET", "/items"); ep != nil {
			assertHandlerFile(t, ep, "restify/handlers.ts")
		}
	})

	t.Run("Sails", func(t *testing.T) {
		requireEndpoint(t, "sails", "GET", "/widgets")
		requireEndpoint(t, "sails", "GET", "/widgets/{id}")
		requireEndpoint(t, "sails", "POST", "/widgets")
		requireEndpoint(t, "sails", "DELETE", "/widgets/{id}")
	})
}

// pickEndpointFromFramework returns the endpoint matching verb+path-suffix that
// was emitted by the given framework (disambiguates shared paths like /users
// across Polka and Marble). Returns nil if none match.
func pickEndpointFromFramework(endpoints []*graph.Entity, framework, verb, pathSuffix string) *graph.Entity {
	verb = strings.ToUpper(verb)
	for _, e := range endpoints {
		if e.PropLen() == 0 {
			continue
		}
		if e.PropGet("framework") != framework {
			continue
		}
		if strings.ToUpper(e.PropGet("verb")) != verb {
			continue
		}
		p := e.PropGet("path")
		if p == pathSuffix || strings.HasSuffix(p, pathSuffix) {
			return e
		}
	}
	return nil
}
