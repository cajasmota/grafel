package engine

import (
	"strings"
	"testing"
)

// mwProps reuses the auth pass test harness to run detection and return the
// synthetic http_endpoint_definition entities keyed by "<VERB> <path>".
func mwProps(t *testing.T, language, path, content string) map[string]middlewareEndpoint {
	t.Helper()
	raw := authProps(t, language, path, content)
	out := map[string]middlewareEndpoint{}
	for k, e := range raw {
		out[k] = middlewareEndpoint{
			count: e.Properties["middleware_count"],
			names: e.Properties["middleware_names"],
			scope: e.Properties["middleware_scope"],
			chain: e.Properties["middleware_chain"],
		}
	}
	return out
}

type middlewareEndpoint struct {
	count, names, scope, chain string
}

// requireMiddleware asserts the endpoint at key carries a non-empty middleware
// chain whose names contain each wantName substring.
func requireMiddleware(t *testing.T, eps map[string]middlewareEndpoint, key string, wantNames ...string) middlewareEndpoint {
	t.Helper()
	e, ok := eps[key]
	if !ok {
		keys := make([]string, 0, len(eps))
		for k := range eps {
			keys = append(keys, k)
		}
		t.Fatalf("endpoint %q not synthesised (got: %v)", key, keys)
	}
	if e.count == "" || e.count == "0" {
		t.Fatalf("%s: middleware_count=%q, want >0 (names=%q)", key, e.count, e.names)
	}
	if e.chain == "" {
		t.Errorf("%s: middleware_chain not stamped", key)
	}
	for _, w := range wantNames {
		if !strings.Contains(e.names, w) {
			t.Errorf("%s: middleware_names=%q, want to contain %q", key, e.names, w)
		}
	}
	return e
}

// requireNoMiddleware asserts the endpoint has no middleware chain.
func requireNoMiddleware(t *testing.T, eps map[string]middlewareEndpoint, key string) {
	t.Helper()
	e, ok := eps[key]
	if !ok {
		t.Fatalf("endpoint %q not synthesised", key)
	}
	if e.count != "" && e.count != "0" {
		t.Errorf("%s: middleware_count=%q, want empty (names=%q)", key, e.count, e.names)
	}
}

func TestMiddleware_Express(t *testing.T) {
	eps := mwProps(t, "typescript", "app.ts", readBackendFixture(t, "express_middleware.ts"))
	// Route-level chain + inherited app-level chain.
	users := requireMiddleware(t, eps, "GET /users", "rateLimit", "validateQuery", "cors", "requestLogger")
	if users.scope != middlewareScopeRouteApp {
		t.Errorf("GET /users: scope=%q, want route+app", users.scope)
	}
	requireMiddleware(t, eps, "POST /users", "validateBody")
	// /health has no route-level chain → app-level only.
	health := requireMiddleware(t, eps, "GET /health", "cors", "requestLogger")
	if health.scope != middlewareScopeApp {
		t.Errorf("GET /health: scope=%q, want app", health.scope)
	}
}

func TestMiddleware_Koa(t *testing.T) {
	eps := mwProps(t, "typescript", "routes.ts", readBackendFixture(t, "koa_middleware.ts"))
	requireMiddleware(t, eps, "GET /profile", "rateLimit", "requestLogger")
	requireMiddleware(t, eps, "PUT /profile", "validateBody")
	// /ping inherits the app-level chain (app.use), no route-level middleware.
	ping := requireMiddleware(t, eps, "GET /ping", "requestLogger")
	if ping.scope != middlewareScopeApp {
		t.Errorf("GET /ping: scope=%q, want app", ping.scope)
	}
}

func TestMiddleware_Hono(t *testing.T) {
	eps := mwProps(t, "typescript", "app.ts", readBackendFixture(t, "hono_middleware.ts"))
	requireMiddleware(t, eps, "GET /secure", "verifyToken", "logger")
	requireMiddleware(t, eps, "GET /items", "cacheControl")
}

func TestMiddleware_Fastify(t *testing.T) {
	eps := mwProps(t, "typescript", "server.ts", readBackendFixture(t, "fastify_middleware.ts"))
	// Global hooks (onRequest/preHandler) inherited by every route + per-route mw.
	acct := requireMiddleware(t, eps, "GET /account", "preHandlerGuard", "onRequest", "preHandler")
	if acct.scope != middlewareScopeRouteApp {
		t.Errorf("GET /account: scope=%q, want route+app", acct.scope)
	}
	requireMiddleware(t, eps, "POST /account", "validateBody")
	// /status has no per-route mw but inherits the global hooks.
	requireMiddleware(t, eps, "GET /status", "onRequest", "preHandler")
}

func TestMiddleware_Nest(t *testing.T) {
	eps := mwProps(t, "typescript", "orders.controller.ts", readBackendFixture(t, "nestjs_middleware.ts"))
	// Class-level @UseInterceptors + @UseFilters applies to every handler.
	findAll := requireMiddleware(t, eps, "GET /orders", "@UseInterceptors(LoggingInterceptor)", "@UseFilters(HttpExceptionFilter)")
	if findAll.scope != middlewareScopeApp {
		t.Errorf("GET /orders: scope=%q, want app (class-level)", findAll.scope)
	}
	// Method-level @UsePipes + @UseGuards on create() in addition to class-level.
	create := requireMiddleware(t, eps, "POST /orders", "@UsePipes(ValidationPipe)", "@UseGuards(RolesGuard)", "@UseInterceptors(LoggingInterceptor)")
	if create.scope != middlewareScopeRouteApp {
		t.Errorf("POST /orders: scope=%q, want route+app", create.scope)
	}
}

func TestMiddleware_Hapi(t *testing.T) {
	eps := mwProps(t, "typescript", "server.ts", readBackendFixture(t, "hapi_middleware.ts"))
	// Server.ext applies to every route; /private also has options.pre.
	priv := requireMiddleware(t, eps, "GET /private", "route pre", "onPreHandler")
	if priv.scope != middlewareScopeRouteApp {
		t.Errorf("GET /private: scope=%q, want route+app", priv.scope)
	}
	// /login has no per-route mw but inherits the server.ext point.
	requireMiddleware(t, eps, "POST /login", "onPreHandler")
}

func TestMiddleware_Adonis(t *testing.T) {
	eps := mwProps(t, "typescript", "start/routes.ts", readBackendFixture(t, "adonisjs_middleware.ts"))
	requireMiddleware(t, eps, "GET /dashboard", "auth", "throttle")
	requireMiddleware(t, eps, "POST /posts", "auth")
	requireNoMiddleware(t, eps, "GET /about")
}

func TestMiddleware_Feathers(t *testing.T) {
	eps := mwProps(t, "typescript", "app.ts", readBackendFixture(t, "feathers_middleware.ts"))
	// Service hooks (before/after/error) apply to every verb of the service.
	requireMiddleware(t, eps, "GET /messages", "before", "after", "error")
	requireMiddleware(t, eps, "POST /messages", "before")
	requireMiddleware(t, eps, "GET /messages/{id}", "before")
}

func TestMiddleware_Marble(t *testing.T) {
	eps := mwProps(t, "typescript", "user.effects.ts", readBackendFixture(t, "marblejs_middleware.ts"))
	me := requireMiddleware(t, eps, "GET /me", "use(logger$)", "use(validate$)")
	if me.scope != middlewareScopeRoute {
		t.Errorf("GET /me: scope=%q, want route", me.scope)
	}
	requireNoMiddleware(t, eps, "GET /status")
}

func TestMiddleware_Polka(t *testing.T) {
	eps := mwProps(t, "typescript", "server.ts", readBackendFixture(t, "polka_middleware.ts"))
	requireMiddleware(t, eps, "GET /private", "requireAuth", "compression", "requestLogger")
	// /public has no per-route mw but inherits the app-level chain.
	requireMiddleware(t, eps, "GET /public", "requestLogger")
}

func TestMiddleware_Restify(t *testing.T) {
	eps := mwProps(t, "typescript", "server.ts", readBackendFixture(t, "restify_middleware.ts"))
	requireMiddleware(t, eps, "GET /secrets", "requireAuth", "requestLogger")
	requireMiddleware(t, eps, "GET /info", "requestLogger")
}

// TestMiddleware_SailsHTTPOrder — config/http.js middleware order array
// (framework_specific idiom). Proves the order-array recogniser.
func TestMiddleware_SailsHTTPOrder(t *testing.T) {
	order, ok := ParseSailsMiddlewareOrder(readBackendFixture(t, "sails_http.ts"), "config/http.js")
	if !ok {
		t.Fatal("ParseSailsMiddlewareOrder: expected a parsed middleware order")
	}
	if len(order.Order) < 5 {
		t.Fatalf("Order=%v, want the full pipeline", order.Order)
	}
	want := []string{"cookieParser", "session", "bodyParser", "router"}
	for _, w := range want {
		found := false
		for _, got := range order.Order {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Order missing %q (got %v)", w, order.Order)
		}
	}
	if !sailsHTTPConfigFile("config/http.js") {
		t.Error("sailsHTTPConfigFile: want true for config/http.js")
	}
}
