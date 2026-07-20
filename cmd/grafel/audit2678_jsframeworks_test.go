package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestAudit2678_JSFrameworks_EndpointAttribution is the integration guard
// for the #2678 audit (JS/TS subset). It indexes a multi-framework fixture
// and asserts that every emitted http_endpoint_definition attributes its
// SourceFile to the file where the handler body lives — NOT to the
// registration / routing file.
//
// Coverage:
//
//   - Express: named-reference handlers in handlers.ts registered from
//     routes.ts. Before the fix, source_file == routes.ts (BUG). After:
//     source_file == handlers.ts.
//   - NestJS: @Get / @Post decorators on methods in orders.controller.ts.
//     source_file is naturally correct; the fix also stamps a non-zero
//     source_line bound to the method def line.
//   - Fastify: fastify.<verb> handlers in handlers.ts registered from
//     routes.ts. Before the fix, no endpoint was emitted at all (the
//     Express receiver allowlist excludes "fastify"). After: emitted by
//     synthesizeFastify and rewritten by resolve to handlers.ts.
//   - Next.js pages router: pages/api/widgets.ts → /api/widgets. Handler
//     lives in the same file by construction; verb=ANY.
//   - Next.js app router: app/api/items/[id]/route.ts → /api/items/{id}.
//     One endpoint per exported verb (GET, DELETE).
func TestAudit2678_JSFrameworks_EndpointAttribution(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_js", "audit2678_js", nil)

	endpoints := collectHTTPEndpointDefs(doc)
	if len(endpoints) == 0 {
		t.Fatalf("audit2678_js: no http_endpoint_definition entities emitted")
	}

	t.Run("Express", func(t *testing.T) {
		// GET /api/users — after Express path-prefix composition is
		// applied. The synthesizer emits the raw registration path
		// ("/users") because the router→app mount isn't AST-composed
		// for Express; we therefore look up by verb + path-suffix.
		for _, verb := range []string{"GET", "POST"} {
			ep := findEndpointBySuffix(endpoints, verb, "/users")
			if ep == nil {
				t.Errorf("Express: missing endpoint %s .../users", verb)
				continue
			}
			assertHandlerFile(t, ep, "express/handlers.ts")
		}
		ep := findEndpointBySuffix(endpoints, "GET", "/users/{id}")
		if ep == nil {
			ep = findEndpointBySuffix(endpoints, "GET", "/users/:id")
		}
		if ep == nil {
			t.Errorf("Express: missing GET /users/:id endpoint")
		} else {
			assertHandlerFile(t, ep, "express/handlers.ts")
		}
	})

	t.Run("NestJS", func(t *testing.T) {
		for _, tc := range []struct {
			verb     string
			pathHint string
		}{
			{"GET", "/orders"},
			{"POST", "/orders"},
			{"GET", "/orders/:id"},
		} {
			ep := findEndpointBySuffix(endpoints, tc.verb, tc.pathHint)
			if ep == nil {
				// NestJS canonicalisation may already have rewritten
				// :id → {id}.
				ep = findEndpointBySuffix(endpoints, tc.verb,
					strings.ReplaceAll(tc.pathHint, ":id", "{id}"))
			}
			if ep == nil {
				t.Errorf("NestJS: missing endpoint %s %s", tc.verb, tc.pathHint)
				continue
			}
			assertHandlerFile(t, ep, "nestjs/orders.controller.ts")
			// #2678 — NestJS endpoints must carry a non-zero source_line
			// bound to the method def, not the @Controller line.
			if ep.StartLine == 0 {
				t.Errorf("NestJS %s %s: source_line=0, want method def line",
					tc.verb, tc.pathHint)
			}
		}
	})

	t.Run("Fastify", func(t *testing.T) {
		for _, verb := range []string{"GET", "POST"} {
			ep := findEndpointBySuffix(endpoints, verb, "/products")
			if ep == nil {
				t.Errorf("Fastify: missing endpoint %s /products "+
					"(synthesizeFastify did not fire)", verb)
				continue
			}
			assertHandlerFile(t, ep, "fastify/handlers.ts")
		}
	})

	t.Run("NextPagesRouter", func(t *testing.T) {
		ep := findEndpointBySuffix(endpoints, "ANY", "/api/widgets")
		if ep == nil {
			t.Fatalf("NextJS pages: missing ANY /api/widgets endpoint")
		}
		// Pages-router handler IS in widgets.ts (export default).
		assertHandlerFile(t, ep, "nextjs_pages/pages/api/widgets.ts")
	})

	t.Run("NextAppRouter", func(t *testing.T) {
		for _, verb := range []string{"GET", "DELETE"} {
			ep := findEndpointBySuffix(endpoints, verb, "/api/items/{id}")
			if ep == nil {
				t.Errorf("NextJS app: missing %s /api/items/{id}", verb)
				continue
			}
			assertHandlerFile(t, ep,
				"nextjs_app/app/api/items/[id]/route.ts")
		}
	})
}

func collectHTTPEndpointDefs(doc *graph.Document) []*graph.Entity {
	out := make([]*graph.Entity, 0, 32)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == "http_endpoint_definition" || e.Kind == "http_endpoint" {
			out = append(out, e)
		}
	}
	return out
}

// findEndpointBySuffix searches the endpoints slice for an entity whose
// path property ends with pathSuffix (matching either the raw or the
// canonical form) and whose verb matches. Returns nil if not found.
func findEndpointBySuffix(endpoints []*graph.Entity, verb, pathSuffix string) *graph.Entity {
	verb = strings.ToUpper(verb)
	for _, e := range endpoints {
		if e.PropLen() == 0 {
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

// assertHandlerFile fails the test unless e.SourceFile (the indexer-relative
// path) ends with the given suffix. Uses forward-slash normalisation so the
// assertion is portable across OSes.
func assertHandlerFile(t *testing.T, e *graph.Entity, suffix string) {
	t.Helper()
	got := filepath.ToSlash(e.SourceFile)
	if !strings.HasSuffix(got, suffix) {
		t.Errorf("endpoint %s (%s %s): source_file=%q, want suffix %q",
			e.Name,
			propOrEmpty(e, "verb"),
			propOrEmpty(e, "path"),
			got, suffix)
	}
}

func propOrEmpty(e *graph.Entity, k string) string {
	if e.PropLen() == 0 {
		return ""
	}
	return e.PropGet(k)
}
