package golang_test

import (
	"testing"
)

// Tests for the framework-agnostic middleware + auth extender
// (custom_go_middleware_auth, issue #3213): buffalo / iris / hertz /
// gorilla-mux (.Use chains), beego (InsertFilter), revel (interceptors).
//
// Reuses fullEntity / findMW / extractFull / fixtureFile / fi from
// middleware_auth_test.go (same package).

const mwExtendKey = "custom_go_middleware_auth"

// findAuth finds the dedicated auth SCOPE.Pattern by its auth:<name> name.
func findAuth(ents []fullEntity, name string) *fullEntity {
	return findMW(ents, name)
}

// ---------------------------------------------------------------------------
// .Use-chain frameworks
// ---------------------------------------------------------------------------

func TestBuffaloMiddlewareAuthExtend(t *testing.T) {
	src := `app.Use(csrf.New)
app.Use(RequestLogger)
app.Middleware.Use(JWTAuth)
var _ = buffalo.New`
	ents := extractFull(t, mwExtendKey, fi("app.go", "go", src))
	for _, n := range []string{"csrf.New", "RequestLogger", "JWTAuth"} {
		if findMW(ents, n) == nil {
			t.Fatalf("missing middleware %q", n)
		}
	}
	jwt := findMW(ents, "JWTAuth")
	if jwt.Props["framework"] != "buffalo" {
		t.Errorf("framework=%q want buffalo", jwt.Props["framework"])
	}
	if jwt.Props["auth_kind"] != "jwt" {
		t.Errorf("auth_kind=%q want jwt", jwt.Props["auth_kind"])
	}
	if findAuth(ents, "auth:JWTAuth") == nil {
		t.Error("missing dedicated auth:JWTAuth pattern")
	}
}

func TestIrisMiddlewareAuthExtend(t *testing.T) {
	ents := extractFull(t, mwExtendKey, fixtureFile(t, "iris_middleware_auth.go"))
	log := findMW(ents, "logger.New()")
	if log == nil || log.Props["framework"] != "iris" {
		t.Fatalf("missing iris logger middleware, got %+v", log)
	}
	if log.Props["is_auth"] == "true" {
		t.Error("logger wrongly flagged auth")
	}
	jwt := findMW(ents, "jwt.New(jwtConfig).VerifyMiddleware()")
	if jwt == nil || jwt.Props["auth_kind"] != "jwt" {
		t.Errorf("expected iris jwt auth, got %+v", jwt)
	}
	if findMW(ents, "BasicAuth()") == nil {
		t.Error("missing UseGlobal BasicAuth() middleware")
	}
}

func TestHertzMiddlewareAuthExtend(t *testing.T) {
	ents := extractFull(t, mwExtendKey, fixtureFile(t, "hertz_middleware_auth.go"))
	// Ordered chain: RecoveryMiddleware() order 0, AccessLog() order 1.
	rec := findMW(ents, "RecoveryMiddleware()")
	if rec == nil || rec.Props["mw_order"] != "0" {
		t.Errorf("RecoveryMiddleware order, got %+v", rec)
	}
	acc := findMW(ents, "AccessLog()")
	if acc == nil || acc.Props["mw_order"] != "1" {
		t.Errorf("AccessLog order, got %+v", acc)
	}
	auth := findMW(ents, "authMw.MiddlewareFunc()")
	if auth == nil || auth.Props["auth_kind"] != "auth" {
		t.Errorf("expected hertz auth middleware, got %+v", auth)
	}
	if auth.Props["framework"] != "hertz" {
		t.Errorf("framework=%q want hertz", auth.Props["framework"])
	}
}

func TestGorillaMuxMiddlewareAuthExtend(t *testing.T) {
	ents := extractFull(t, mwExtendKey, fixtureFile(t, "gorilla_mux_middleware_auth.go"))
	logmw := findMW(ents, "LoggingMiddleware")
	if logmw == nil || logmw.Props["framework"] != "gorilla-mux" {
		t.Fatalf("missing gorilla LoggingMiddleware, got %+v", logmw)
	}
	if logmw.Props["mw_order"] != "0" {
		t.Errorf("LoggingMiddleware order=%q want 0", logmw.Props["mw_order"])
	}
	auth := findMW(ents, "JWTAuthMiddleware")
	if auth == nil || auth.Props["auth_kind"] != "jwt" {
		t.Errorf("expected gorilla jwt auth, got %+v", auth)
	}
	if findAuth(ents, "auth:JWTAuthMiddleware") == nil {
		t.Error("missing auth:JWTAuthMiddleware pattern")
	}
}

// ---------------------------------------------------------------------------
// beego InsertFilter API
// ---------------------------------------------------------------------------

func TestBeegoInsertFilterMiddlewareAuth(t *testing.T) {
	ents := extractFull(t, mwExtendKey, fixtureFile(t, "beego_middleware_auth.go"))
	logf := findMW(ents, "LoggingFilter")
	if logf == nil || logf.Props["framework"] != "beego" {
		t.Fatalf("missing beego LoggingFilter, got %+v", logf)
	}
	if logf.Props["pattern_kind"] != "middleware" {
		t.Errorf("pattern_kind=%q want middleware", logf.Props["pattern_kind"])
	}
	if logf.Props["filter_position"] != "web.BeforeRouter" {
		t.Errorf("filter_position=%q", logf.Props["filter_position"])
	}
	if logf.Props["filter_path"] != "/api/*" {
		t.Errorf("filter_path=%q want /api/*", logf.Props["filter_path"])
	}
	auth := findMW(ents, "JWTAuthFilter")
	if auth == nil || auth.Props["auth_kind"] != "jwt" {
		t.Errorf("expected beego jwt auth filter, got %+v", auth)
	}
	if findAuth(ents, "auth:JWTAuthFilter") == nil {
		t.Error("missing auth:JWTAuthFilter pattern")
	}
}

// ---------------------------------------------------------------------------
// revel interceptors
// ---------------------------------------------------------------------------

func TestRevelInterceptMiddlewareAuth(t *testing.T) {
	ents := extractFull(t, mwExtendKey, fixtureFile(t, "revel_middleware_auth.go"))
	logmw := findMW(ents, "RequestLogger")
	if logmw == nil || logmw.Props["framework"] != "revel" {
		t.Fatalf("missing revel RequestLogger interceptor, got %+v", logmw)
	}
	if logmw.Props["middleware_form"] != "interceptor" {
		t.Errorf("middleware_form=%q want interceptor", logmw.Props["middleware_form"])
	}
	auth := findMW(ents, "App.checkAuthUser")
	if auth == nil || auth.Props["auth_kind"] != "auth" {
		t.Errorf("expected revel auth interceptor, got %+v", auth)
	}
	if findAuth(ents, "auth:App.checkAuthUser") == nil {
		t.Error("missing auth:App.checkAuthUser pattern")
	}
}

// ---------------------------------------------------------------------------
// NA frameworks: net/http + fasthttp have no middleware-registration surface;
// the extender must emit nothing for them (no .Use / filter / interceptor).
// ---------------------------------------------------------------------------

func TestNetHTTPNoMiddlewareEmitted(t *testing.T) {
	src := `mux := http.NewServeMux()
mux.HandleFunc("GET /users", listUsers)`
	ents := extractFull(t, mwExtendKey, fi("main.go", "go", src))
	for _, e := range ents {
		if e.Props["pattern_kind"] == "middleware" || e.Props["pattern_kind"] == "auth" {
			t.Errorf("net/http should emit no middleware/auth, got %q", e.Name)
		}
	}
}

func TestFasthttpNoMiddlewareEmitted(t *testing.T) {
	src := `r := router.New()
r.GET("/", Index)
fasthttp.ListenAndServe(":8080", r.Handler)`
	ents := extractFull(t, mwExtendKey, fi("main.go", "go", src))
	for _, e := range ents {
		if e.Props["pattern_kind"] == "middleware" || e.Props["pattern_kind"] == "auth" {
			t.Errorf("fasthttp should emit no middleware/auth, got %q", e.Name)
		}
	}
}

// Files with no recognised framework marker emit nothing.
func TestMiddlewareAuthExtendNoFrameworkNoEmit(t *testing.T) {
	src := `func main() { fmt.Println("hi") }`
	ents := extractFull(t, mwExtendKey, fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for unattributed file, got %d", len(ents))
	}
}
