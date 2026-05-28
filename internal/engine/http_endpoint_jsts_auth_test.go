package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// authProps runs detection on a JS/TS fixture and returns the synthetic
// http_endpoint_definition entities keyed by "<VERB> <path>".
func authProps(t *testing.T, language, path, content string) map[string]types.EntityRecord {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(content),
		Language: language,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	out := map[string]types.EntityRecord{}
	for _, e := range res.Entities {
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		key := e.Properties["verb"] + " " + e.Properties["path"]
		out[key] = e
	}
	return out
}

// requireProtected asserts the endpoint at key is present and carries an
// auth_required=true posture with the expected method and (optionally) confidence.
func requireProtected(t *testing.T, eps map[string]types.EntityRecord, key, wantMethod string) {
	t.Helper()
	e, ok := eps[key]
	if !ok {
		t.Fatalf("endpoint %q not synthesised (got: %v)", key, keysOf(eps))
	}
	if e.Properties["auth_required"] != "true" {
		t.Errorf("%s: auth_required=%q, want true (props: %v)", key, e.Properties["auth_required"], e.Properties)
	}
	if wantMethod != "" && e.Properties["auth_method"] != wantMethod {
		t.Errorf("%s: auth_method=%q, want %q", key, e.Properties["auth_method"], wantMethod)
	}
	// The MCP signal-1 key must be present so archigraph_auth_coverage fires.
	if e.Properties["auth_middleware"] == "" && e.Properties["auth_guard"] == "" {
		t.Errorf("%s: neither auth_middleware nor auth_guard stamped (props: %v)", key, e.Properties)
	}
}

// requirePublic asserts the endpoint is present and is NOT marked auth_required.
func requirePublic(t *testing.T, eps map[string]types.EntityRecord, key string) {
	t.Helper()
	e, ok := eps[key]
	if !ok {
		t.Fatalf("endpoint %q not synthesised (got: %v)", key, keysOf(eps))
	}
	if e.Properties["auth_required"] == "true" {
		t.Errorf("%s: auth_required=true, want public/unknown (props: %v)", key, e.Properties)
	}
}

func keysOf(m map[string]types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestAuth_Express — app-level passport + route-level requireAuth.
func TestAuth_Express(t *testing.T) {
	eps := authProps(t, "typescript", "app.ts", readBackendFixture(t, "express_auth.ts"))
	requireProtected(t, eps, "GET /me", "middleware")
	requireProtected(t, eps, "POST /orders", "middleware")
	// /health inherits the app-level passport gate (medium confidence).
	e := eps["GET /health"]
	if e.Properties["auth_required"] != "true" || e.Properties["auth_confidence"] != "medium" {
		t.Errorf("GET /health: want app-level medium coverage, got %v", e.Properties)
	}
	if eps["GET /me"].Properties["auth_confidence"] != "high" {
		t.Errorf("GET /me: want high confidence, got %q", eps["GET /me"].Properties["auth_confidence"])
	}
}

// TestAuth_Koa — koa-router route-level guard + a public route.
func TestAuth_Koa(t *testing.T) {
	eps := authProps(t, "typescript", "routes.ts", readBackendFixture(t, "koa_auth.ts"))
	requireProtected(t, eps, "GET /profile", "middleware")
	requireProtected(t, eps, "PUT /profile", "middleware")
	requirePublic(t, eps, "GET /ping")
}

// TestAuth_Hono — app-level jwtAuth + route-level verifyToken.
func TestAuth_Hono(t *testing.T) {
	eps := authProps(t, "typescript", "app.ts", readBackendFixture(t, "hono_auth.ts"))
	requireProtected(t, eps, "GET /secure", "middleware")
	// /items inherits the app-level jwtAuth gate.
	requireProtected(t, eps, "GET /items", "middleware")
}

// TestAuth_Fastify — route-level requireAuth + a public route.
func TestAuth_Fastify(t *testing.T) {
	eps := authProps(t, "typescript", "server.ts", readBackendFixture(t, "fastify_auth.ts"))
	requireProtected(t, eps, "GET /account", "middleware")
	requireProtected(t, eps, "POST /account", "middleware")
	requirePublic(t, eps, "GET /status")
}

// TestAuth_Nest — class-level @UseGuards (medium) + method-level guard+roles (high).
func TestAuth_Nest(t *testing.T) {
	eps := authProps(t, "typescript", "users.controller.ts", readBackendFixture(t, "nestjs_auth.ts"))
	// Class-level guard → every method protected.
	requireProtected(t, eps, "GET /users", "guard")
	requireProtected(t, eps, "GET /users/{id}", "guard")
	// Method-level @UseGuards(RolesGuard) @Roles('admin') → high + roles.
	create := eps["POST /users"]
	if create.Properties["auth_required"] != "true" || create.Properties["auth_confidence"] != "high" {
		t.Errorf("POST /users: want method-level high guard, got %v", create.Properties)
	}
	if create.Properties["auth_roles"] != "admin" {
		t.Errorf("POST /users: auth_roles=%q, want admin", create.Properties["auth_roles"])
	}
	if create.Properties["auth_guard"] == "" {
		t.Errorf("POST /users: auth_guard not stamped (props: %v)", create.Properties)
	}
}

// TestAuth_Adonis — route-chain .middleware('auth') + a public route.
func TestAuth_Adonis(t *testing.T) {
	eps := authProps(t, "typescript", "start/routes.ts", readBackendFixture(t, "adonisjs_auth.ts"))
	requireProtected(t, eps, "GET /dashboard", "middleware")
	requireProtected(t, eps, "POST /posts", "middleware")
	requirePublic(t, eps, "GET /about")
}

// TestAuth_Hapi — per-route options.auth (protected) + auth:false (public).
func TestAuth_Hapi(t *testing.T) {
	eps := authProps(t, "typescript", "server.ts", readBackendFixture(t, "hapi_auth.ts"))
	requireProtected(t, eps, "GET /private", "config")
	// auth: false → explicitly public.
	e, ok := eps["POST /login"]
	if !ok {
		t.Fatalf("POST /login not synthesised (got: %v)", keysOf(eps))
	}
	if e.Properties["auth_required"] != "false" {
		t.Errorf("POST /login: auth_required=%q, want false (auth:false)", e.Properties["auth_required"])
	}
}

// TestAuth_Feathers — app-level authenticate() gates mounted services.
func TestAuth_Feathers(t *testing.T) {
	eps := authProps(t, "typescript", "app.ts", readBackendFixture(t, "feathers_auth.ts"))
	// Service verbs inherit the app-level authenticate() gate.
	requireProtected(t, eps, "GET /messages", "middleware")
	requireProtected(t, eps, "POST /messages", "middleware")
	requireProtected(t, eps, "GET /users", "middleware")
}

// TestAuth_Marble — authorize$ effect in the pipe (protected) + a public effect.
func TestAuth_Marble(t *testing.T) {
	eps := authProps(t, "typescript", "user.effects.ts", readBackendFixture(t, "marblejs_auth.ts"))
	requireProtected(t, eps, "GET /me", "middleware")
	requirePublic(t, eps, "GET /status")
}

// TestAuth_Polka — route-level requireAuth + a public route.
func TestAuth_Polka(t *testing.T) {
	eps := authProps(t, "typescript", "server.ts", readBackendFixture(t, "polka_auth.ts"))
	requireProtected(t, eps, "GET /private", "middleware")
	requirePublic(t, eps, "GET /public")
}

// TestAuth_Restify — server.use passport gate + route-level requireAuth.
func TestAuth_Restify(t *testing.T) {
	eps := authProps(t, "typescript", "server.ts", readBackendFixture(t, "restify_auth.ts"))
	requireProtected(t, eps, "GET /secrets", "middleware")
	requireProtected(t, eps, "GET /info", "middleware") // inherits server.use gate
}

// TestAuth_SailsPolicies — config/policies.js global default-protected posture
// (framework_specific idiom). Proves the policy-map recogniser.
func TestAuth_SailsPolicies(t *testing.T) {
	pm, ok := ParseSailsPolicies(readBackendFixture(t, "sails_policies.ts"), "config/policies.js")
	if !ok {
		t.Fatal("ParseSailsPolicies: expected a parsed policy map")
	}
	if !pm.DefaultProtected {
		t.Errorf("DefaultProtected=false, want true for '*': 'isLoggedIn'")
	}
	if pm.DefaultPolicy != "'isLoggedIn'" {
		t.Errorf("DefaultPolicy=%q, want 'isLoggedIn'", pm.DefaultPolicy)
	}
	if !pm.HasDefault {
		t.Error("HasDefault=false, want true")
	}
	// AuthController object block: per-action overrides.
	ac, ok := pm.Controllers["AuthController"]
	if !ok {
		t.Fatalf("AuthController block not parsed (controllers: %v)", pm.Controllers)
	}
	if ac.Actions["login"] != "true" {
		t.Errorf("AuthController.login=%q, want true", ac.Actions["login"])
	}
	if ac.Actions["logout"] != "'isLoggedIn'" {
		t.Errorf("AuthController.logout=%q, want 'isLoggedIn'", ac.Actions["logout"])
	}
	// DashboardController bare value: controller-level catch-all.
	dc, ok := pm.Controllers["DashboardController"]
	if !ok || !dc.HasControllerPolicy {
		t.Fatalf("DashboardController controller-level policy not parsed (controllers: %v)", pm.Controllers)
	}
	if dc.ControllerPolicy != "'isLoggedIn'" {
		t.Errorf("DashboardController catch-all=%q, want 'isLoggedIn'", dc.ControllerPolicy)
	}
}

// TestAuth_SailsCrossFileAttribution — the #2897 cross-file join. Synthesises
// the Sails endpoints from config/routes.js, then runs the corpus-wide
// ApplySailsAuthPolicy pass with a reader serving config/policies.js, and
// asserts the resolved posture per precedence level (action > controller >
// global '*').
func TestAuth_SailsCrossFileAttribution(t *testing.T) {
	routesSrc := readBackendFixture(t, "sails_routes.ts")
	policiesSrc := readBackendFixture(t, "sails_policies.ts")

	// 1. Synthesise the Sails endpoints from config/routes.js (per-file pass).
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "config/routes.js",
		Content:  []byte(routesSrc),
		Language: "javascript",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// 2. Run the corpus-wide policy attribution pass with both files available.
	reader := func(p string) []byte {
		switch p {
		case "config/policies.js":
			return []byte(policiesSrc)
		case "config/routes.js":
			return []byte(routesSrc)
		}
		return nil
	}
	paths := []string{"config/routes.js", "config/policies.js"}
	stats := ApplySailsAuthPolicy(res.Entities, paths, reader)
	if stats.PolicyFiles != 1 {
		t.Fatalf("PolicyFiles=%d, want 1", stats.PolicyFiles)
	}
	if stats.Attributed == 0 {
		t.Fatal("Attributed=0, want >0 (cross-file join produced no postures)")
	}

	eps := map[string]types.EntityRecord{}
	for _, e := range res.Entities {
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		eps[e.Properties["verb"]+" "+e.Properties["path"]] = e
	}

	// Global '*' default protects UsersController.* (no override).
	requireProtected(t, eps, "GET /users", "config")
	if eps["GET /users"].Properties["auth_confidence"] != "medium" {
		t.Errorf("GET /users: confidence=%q, want medium", eps["GET /users"].Properties["auth_confidence"])
	}
	// Controller-level catch-all gates DashboardController.index.
	requireProtected(t, eps, "GET /dashboard", "config")
	// Action-level override: AuthController.login is explicitly public (true).
	requirePublic(t, eps, "POST /login")
	if eps["POST /login"].Properties["auth_method"] != "config" {
		t.Errorf("POST /login: method=%q, want config", eps["POST /login"].Properties["auth_method"])
	}
	// Action-level override: AuthController.logout is gated.
	requireProtected(t, eps, "POST /logout", "config")
}
