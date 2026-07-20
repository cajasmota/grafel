package main

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestAudit2853_JSBackendMiddleware is the real-data integration guard for
// #2853. It indexes a multi-framework backend-HTTP corpus exercising the full
// middleware picture (the superset of auth) through the complete indexer
// pipeline — not single-file unit fixtures — and asserts the resolved
// middleware chain survives onto the http_endpoint_definition entities the
// pipeline emits, covering:
//
//   - Express  — app.use global chain + per-route middleware array.
//   - Koa      — app.use global chain + koa-router per-route middleware.
//   - Fastify  — addHook global lifecycle hooks + per-route hook chain.
//   - NestJS   — interceptor/pipe/filter/guard pipeline triad (class + method).
//   - Hapi     — server.ext point + per-route options.pre.
//   - AdonisJS — named middleware chained onto routes.
//   - Feathers — service hooks (before/after/error).
//   - Marble   — use(...) middleware effects piped into the route.
func TestAudit2853_JSBackendMiddleware(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2853_js", "audit2853_js", nil)
	endpoints := collectHTTPEndpointDefs(doc)
	if len(endpoints) == 0 {
		t.Fatalf("audit2853_js: no http_endpoint_definition entities emitted")
	}

	requireMW := func(t *testing.T, verb, path string, wantNames ...string) *graph.Entity {
		t.Helper()
		ep := findEndpointBySuffix(endpoints, verb, path)
		if ep == nil {
			t.Fatalf("missing endpoint %s %s", verb, path)
		}
		if ep.PropGet("middleware_count") == "" || ep.PropGet("middleware_count") == "0" {
			t.Errorf("%s %s: middleware_count=%q, want >0", verb, path, ep.PropGet("middleware_count"))
		}
		if ep.PropGet("middleware_chain") == "" {
			t.Errorf("%s %s: middleware_chain not stamped", verb, path)
		}
		names := ep.PropGet("middleware_names")
		for _, w := range wantNames {
			if !strings.Contains(names, w) {
				t.Errorf("%s %s: middleware_names=%q, want to contain %q", verb, path, names, w)
			}
		}
		return ep
	}
	requireNoMW := func(t *testing.T, verb, path string) {
		t.Helper()
		ep := findEndpointBySuffix(endpoints, verb, path)
		if ep == nil {
			t.Fatalf("missing endpoint %s %s", verb, path)
		}
		if c := ep.PropGet("middleware_count"); c != "" && c != "0" {
			t.Errorf("%s %s: middleware_count=%q, want empty", verb, path, c)
		}
	}

	t.Run("Express", func(t *testing.T) {
		requireMW(t, "GET", "/users", "rateLimit", "validateQuery", "cors", "requestLogger")
		requireMW(t, "POST", "/users", "validateBody")
		// /health has no per-route chain but inherits the app-level chain.
		requireMW(t, "GET", "/health", "cors", "requestLogger")
	})

	t.Run("Koa", func(t *testing.T) {
		requireMW(t, "GET", "/profile", "rateLimit", "requestLogger")
		requireMW(t, "GET", "/ping", "requestLogger")
	})

	t.Run("Fastify", func(t *testing.T) {
		requireMW(t, "GET", "/account", "preHandlerGuard", "onRequest", "preHandler")
		requireMW(t, "GET", "/status", "onRequest", "preHandler")
	})

	t.Run("NestJS", func(t *testing.T) {
		requireMW(t, "GET", "/orders", "@UseInterceptors(LoggingInterceptor)", "@UseFilters(HttpExceptionFilter)")
		requireMW(t, "POST", "/orders", "@UsePipes(ValidationPipe)", "@UseGuards(RolesGuard)")
	})

	t.Run("Hapi", func(t *testing.T) {
		requireMW(t, "GET", "/private", "route pre", "onPreHandler")
		requireMW(t, "POST", "/login", "onPreHandler")
	})

	t.Run("AdonisJS", func(t *testing.T) {
		requireMW(t, "GET", "/dashboard", "auth", "throttle")
		requireNoMW(t, "GET", "/about")
	})

	t.Run("Feathers", func(t *testing.T) {
		requireMW(t, "GET", "/messages", "before", "after", "error")
		requireMW(t, "POST", "/messages", "before")
	})

	t.Run("Marble", func(t *testing.T) {
		requireMW(t, "GET", "/marble/me", "use(logger$)", "use(validate$)")
		requireNoMW(t, "GET", "/marble/status")
	})
}
