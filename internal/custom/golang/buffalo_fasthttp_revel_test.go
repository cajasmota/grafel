package golang_test

import (
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// ---------------------------------------------------------------------------
// Buffalo
// ---------------------------------------------------------------------------

func TestBuffaloApp(t *testing.T) {
	src := `app := buffalo.New(buffalo.Options{})`
	ents := extract(t, "custom_go_buffalo", fi("app.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "app") {
		t.Error("expected app as buffalo application service")
	}
}

func TestBuffaloVerbRoutes(t *testing.T) {
	src := `
app.GET("/", HomeHandler)
app.POST("/login", LoginHandler)
`
	ents := extract(t, "custom_go_buffalo", fi("app.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /") {
		t.Error("expected GET / route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /login") {
		t.Error("expected POST /login route")
	}
}

func TestBuffaloGroupPrefix(t *testing.T) {
	src := `
api := app.Group("/api/v1")
api.GET("/health", HealthHandler)
`
	ents := extract(t, "custom_go_buffalo", fi("app.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api/v1") {
		t.Error("expected /api/v1 group component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/v1/health") {
		t.Error("expected GET /api/v1/health with group prefix resolved")
	}
}

func TestBuffaloResource(t *testing.T) {
	src := `app.Resource("/users", UsersResource{})`
	ents := extract(t, "custom_go_buffalo", fi("app.go", "go", src))
	want := []string{
		"GET /users", "GET /users/new", "POST /users",
		"GET /users/{id}", "GET /users/{id}/edit",
		"PUT /users/{id}", "DELETE /users/{id}",
	}
	for _, w := range want {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("expected resource route %q", w)
		}
	}
}

func TestBuffaloMount(t *testing.T) {
	src := `app.Mount("/admin", admin.App())`
	ents := extract(t, "custom_go_buffalo", fi("app.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/admin") {
		t.Error("expected /admin mount component")
	}
}

func TestBuffaloMiddleware(t *testing.T) {
	src := `app.Use(Authorize)`
	ents := extract(t, "custom_go_buffalo", fi("app.go", "go", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Authorize") {
		t.Error("expected Authorize middleware pattern")
	}
}

func TestBuffaloFixture(t *testing.T) {
	f := fixtureInput(t, "buffalo_routes.go", "go")
	ents := extract(t, "custom_go_buffalo", f)
	want := []string{
		"GET /", "POST /login", "GET /api/v1/health",
		"GET /users", "POST /users", "DELETE /users/{id}",
	}
	for _, w := range want {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("fixture: expected operation %q", w)
		}
	}
	if !containsEntity(ents, "SCOPE.Service", "app") {
		t.Error("fixture: expected app service")
	}
	if !containsEntity(ents, "SCOPE.Component", "/admin") {
		t.Error("fixture: expected /admin mount component")
	}
}

func TestBuffaloNoMatch(t *testing.T) {
	ents := extract(t, "custom_go_buffalo", fi("main.go", "go", `package main`))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// fasthttp
// ---------------------------------------------------------------------------

func TestFasthttpRouter(t *testing.T) {
	src := `r := router.New()`
	ents := extract(t, "custom_go_fasthttp", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "r") {
		t.Error("expected r as fasthttp router service")
	}
}

func TestFasthttpRoutes(t *testing.T) {
	src := `
r := router.New()
r.GET("/users", listUsers)
r.POST("/users", createUser)
`
	ents := extract(t, "custom_go_fasthttp", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestFasthttpHandleExplicitMethod(t *testing.T) {
	src := `r.Handle("PUT", "/users/{id}", updateUser)`
	ents := extract(t, "custom_go_fasthttp", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "PUT /users/{id}") {
		t.Error("expected PUT /users/{id} from r.Handle")
	}
}

func TestFasthttpGroupPrefix(t *testing.T) {
	src := `
api := r.Group("/api/v1")
api.GET("/health", healthCheck)
`
	ents := extract(t, "custom_go_fasthttp", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/v1/health") {
		t.Error("expected GET /api/v1/health with group prefix resolved")
	}
}

func TestFasthttpRawHandler(t *testing.T) {
	src := `func listUsers(ctx *fasthttp.RequestCtx) { ctx.WriteString("ok") }`
	ents := extract(t, "custom_go_fasthttp", fi("handlers.go", "go", src))
	if !containsEntity(ents, "SCOPE.Pattern", "handler:listUsers") {
		t.Error("expected raw RequestHandler pattern for listUsers")
	}
}

func TestFasthttpListen(t *testing.T) {
	src := `fasthttp.ListenAndServe(":8080", r.Handler)`
	ents := extract(t, "custom_go_fasthttp", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "fasthttp_server:r.Handler") {
		t.Error("expected fasthttp_server service from ListenAndServe")
	}
}

func TestFasthttpFixture(t *testing.T) {
	f := fixtureInput(t, "fasthttp_routes.go", "go")
	ents := extract(t, "custom_go_fasthttp", f)
	want := []string{"GET /users", "POST /users", "PUT /users/{id}", "GET /api/v1/health"}
	for _, w := range want {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("fixture: expected operation %q", w)
		}
	}
	if !containsEntity(ents, "SCOPE.Service", "r") {
		t.Error("fixture: expected r router service")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "handler:listUsers") {
		t.Error("fixture: expected listUsers raw handler pattern")
	}
}

func TestFasthttpNoMatch(t *testing.T) {
	ents := extract(t, "custom_go_fasthttp", fi("main.go", "go", `package main`))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Revel
// ---------------------------------------------------------------------------

// revelRoutesInput loads the conf/routes fixture under a path containing
// "conf/routes" so the routes-file detection path is exercised.
func revelRoutesInput(t *testing.T) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "conf", "routes")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read revel routes fixture: %v", err)
	}
	return extreg.FileInput{Path: p, Language: "text", Content: content}
}

func TestRevelRoutesFile(t *testing.T) {
	ents := extract(t, "custom_go_revel", revelRoutesInput(t))
	want := []string{
		"GET /", "GET /login", "POST /users",
		"GET /users/{id}", "DELETE /users/{id}",
	}
	for _, w := range want {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("expected route %q from conf/routes", w)
		}
	}
}

func TestRevelModuleMount(t *testing.T) {
	ents := extract(t, "custom_go_revel", revelRoutesInput(t))
	if !containsEntity(ents, "SCOPE.Component", "module:testrunner") {
		t.Error("expected module:testrunner mount component")
	}
}

func TestRevelAutoRouteWildcard(t *testing.T) {
	ents := extract(t, "custom_go_revel", revelRoutesInput(t))
	// The wildcard auto-route resolves dynamically; it should still synthesize
	// an ANY endpoint but not attribute a static handler.
	if !containsEntity(ents, "SCOPE.Operation", "ANY /{controller}/{action}") {
		t.Error("expected ANY /{controller}/{action} auto-route")
	}
}

func TestRevelControllerActions(t *testing.T) {
	f := fixtureInput(t, "revel_controller.go", "go")
	ents := extract(t, "custom_go_revel", f)
	if !containsEntity(ents, "SCOPE.Pattern", "handler:App.Index") {
		t.Error("expected App.Index controller-action handler pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "handler:App.Login") {
		t.Error("expected App.Login controller-action handler pattern")
	}
}

func TestRevelInterceptor(t *testing.T) {
	f := fixtureInput(t, "revel_controller.go", "go")
	ents := extract(t, "custom_go_revel", f)
	if !containsEntity(ents, "SCOPE.Pattern", "interceptor:checkUser") {
		t.Error("expected checkUser interceptor middleware pattern")
	}
}

func TestRevelNoMatch(t *testing.T) {
	ents := extract(t, "custom_go_revel", fi("main.go", "go", `package main`))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
