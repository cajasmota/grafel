package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestAudit2852_JSBackendAuth is the real-data integration guard for #2852.
// It indexes a multi-framework backend-HTTP corpus with auth middleware /
// guards / route config through the full indexer pipeline (not single-file
// unit fixtures) and asserts that the resolved auth_policy survives onto the
// http_endpoint_definition entities the pipeline emits, covering:
//
//   - Express  — app-level passport.authenticate + route-level requireAuth.
//   - NestJS   — class-level @UseGuards (medium) + method-level guard+roles.
//   - Hapi     — per-route options.auth (protected) + auth:false (public).
//   - AdonisJS — route-chain .middleware('auth').
//   - Feathers — app-level authenticate() gating mounted services.
//   - Marble   — authorize$ effect piped into the route.
func TestAudit2852_JSBackendAuth(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2852_js", "audit2852_js", nil)
	endpoints := collectHTTPEndpointDefs(doc)
	if len(endpoints) == 0 {
		t.Fatalf("audit2852_js: no http_endpoint_definition entities emitted")
	}

	requireAuthed := func(t *testing.T, verb, path, wantMethod string) *graph.Entity {
		t.Helper()
		ep := findEndpointBySuffix(endpoints, verb, path)
		if ep == nil {
			t.Fatalf("missing endpoint %s %s", verb, path)
		}
		if ep.PropGet("auth_required") != "true" {
			t.Errorf("%s %s: auth_required=%q, want true", verb, path, ep.PropGet("auth_required"))
		}
		if wantMethod != "" && ep.PropGet("auth_method") != wantMethod {
			t.Errorf("%s %s: auth_method=%q, want %q", verb, path, ep.PropGet("auth_method"), wantMethod)
		}
		if ep.PropGet("auth_middleware") == "" && ep.PropGet("auth_guard") == "" {
			t.Errorf("%s %s: no MCP signal-1 key (auth_middleware/auth_guard) stamped", verb, path)
		}
		return ep
	}
	requireOpen := func(t *testing.T, verb, path string) {
		t.Helper()
		ep := findEndpointBySuffix(endpoints, verb, path)
		if ep == nil {
			t.Fatalf("missing endpoint %s %s", verb, path)
		}
		if ep.PropGet("auth_required") == "true" {
			t.Errorf("%s %s: auth_required=true, want public/unknown", verb, path)
		}
	}

	t.Run("Express", func(t *testing.T) {
		requireAuthed(t, "GET", "/account", "middleware")
		requireAuthed(t, "POST", "/account", "middleware")
		// /ping inherits the app-level passport gate.
		requireAuthed(t, "GET", "/ping", "middleware")
	})

	t.Run("NestJS", func(t *testing.T) {
		requireAuthed(t, "GET", "/orders", "guard")
		create := requireAuthed(t, "POST", "/orders", "guard")
		if create.PropGet("auth_roles") != "admin" {
			t.Errorf("POST /orders: auth_roles=%q, want admin", create.PropGet("auth_roles"))
		}
	})

	t.Run("Hapi", func(t *testing.T) {
		requireAuthed(t, "GET", "/private", "config")
		// auth: false → explicit public.
		ep := findEndpointBySuffix(endpoints, "POST", "/login")
		if ep == nil {
			t.Fatal("missing endpoint POST /login")
		}
		if ep.PropGet("auth_required") != "false" {
			t.Errorf("POST /login: auth_required=%q, want false", ep.PropGet("auth_required"))
		}
	})

	t.Run("AdonisJS", func(t *testing.T) {
		requireAuthed(t, "GET", "/dashboard", "middleware")
		requireOpen(t, "GET", "/about")
	})

	t.Run("Feathers", func(t *testing.T) {
		requireAuthed(t, "GET", "/messages", "middleware")
		requireAuthed(t, "POST", "/messages", "middleware")
	})

	t.Run("Marble", func(t *testing.T) {
		requireAuthed(t, "GET", "/me", "middleware")
		requireOpen(t, "GET", "/status")
	})
}
