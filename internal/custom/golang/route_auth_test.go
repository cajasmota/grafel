package golang

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// findEndpoint returns the first endpoint op whose name matches "<VERB> <path>".
func findEndpoint(t *testing.T, ents []types.EntityRecord, name string) types.EntityRecord {
	t.Helper()
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "endpoint" && e.Name == name {
			return e
		}
	}
	t.Fatalf("endpoint %q not found among %d entities", name, len(ents))
	return types.EntityRecord{}
}

func runGin(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ents, err := (&ginExtractor{}).Extract(context.Background(), extractor.FileInput{
		Path: "main.go", Language: "go", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("gin extract: %v", err)
	}
	return ents
}

func runEcho(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ents, err := (&echoExtractor{}).Extract(context.Background(), extractor.FileInput{
		Path: "main.go", Language: "go", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("echo extract: %v", err)
	}
	return ents
}

// TestGinGroupAuth — the canonical acceptance case from #3734:
// `authorized := r.Group("/", AuthRequired())` + `authorized.GET("/me")`
// → GET / /me is auth_required, guard recorded; an unprotected route is NOT.
func TestGinGroupAuth(t *testing.T) {
	src := `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.Default()
	r.GET("/health", healthCheck)
	authorized := r.Group("/", AuthRequired())
	authorized.GET("/me", getMe)
	authorized.POST("/orders", createOrder)
}
`
	ents := runGin(t, src)

	me := findEndpoint(t, ents, "GET //me")
	if me.Properties["auth_required"] != "true" {
		t.Errorf("GET /me: auth_required=%q, want true (props: %v)", me.Properties["auth_required"], me.Properties)
	}
	if me.Properties["auth_guard"] != "AuthRequired" {
		t.Errorf("GET /me: auth_guard=%q, want AuthRequired", me.Properties["auth_guard"])
	}
	if me.Properties["auth_method"] != "middleware" {
		t.Errorf("GET /me: auth_method=%q, want middleware", me.Properties["auth_method"])
	}
	if me.Properties["auth_confidence"] != "high" {
		t.Errorf("GET /me: auth_confidence=%q, want high", me.Properties["auth_confidence"])
	}

	orders := findEndpoint(t, ents, "POST //orders")
	if orders.Properties["auth_required"] != "true" {
		t.Errorf("POST /orders: auth_required=%q, want true", orders.Properties["auth_required"])
	}

	// Negative: the unprotected /health route (registered on the raw engine,
	// no auth .Use, no group) must NOT be marked protected.
	health := findEndpoint(t, ents, "GET /health")
	if health.Properties["auth_required"] == "true" {
		t.Errorf("GET /health: auth_required=true, want unprotected (props: %v)", health.Properties)
	}
}

// TestGinInlineRouteAuth — inline route middleware binds to that exact route.
func TestGinInlineRouteAuth(t *testing.T) {
	src := `package main
func main() {
	r := gin.Default()
	r.GET("/admin", JWTAuth(), adminHandler)
	r.GET("/public", publicHandler)
}
`
	ents := runGin(t, src)

	admin := findEndpoint(t, ents, "GET /admin")
	if admin.Properties["auth_required"] != "true" {
		t.Errorf("GET /admin: auth_required=%q, want true (props: %v)", admin.Properties["auth_required"], admin.Properties)
	}
	if admin.Properties["auth_kind"] != "jwt" {
		t.Errorf("GET /admin: auth_kind=%q, want jwt", admin.Properties["auth_kind"])
	}
	if admin.Properties["auth_guard"] != "JWTAuth" {
		t.Errorf("GET /admin: auth_guard=%q, want JWTAuth", admin.Properties["auth_guard"])
	}

	pub := findEndpoint(t, ents, "GET /public")
	if pub.Properties["auth_required"] == "true" {
		t.Errorf("GET /public: auth_required=true, want unprotected")
	}
}

// TestGinEngineWideAuth — an engine-level `.Use(authMw)` gives MEDIUM coverage
// to every route that has no stronger signal.
func TestGinEngineWideAuth(t *testing.T) {
	src := `package main
func main() {
	r := gin.Default()
	r.Use(jwt.New(cfg))
	r.GET("/me", getMe)
}
`
	ents := runGin(t, src)
	me := findEndpoint(t, ents, "GET /me")
	if me.Properties["auth_required"] != "true" {
		t.Errorf("GET /me: auth_required=%q, want true via engine-wide .Use (props: %v)", me.Properties["auth_required"], me.Properties)
	}
	if me.Properties["auth_confidence"] != "medium" {
		t.Errorf("GET /me: auth_confidence=%q, want medium (engine-wide)", me.Properties["auth_confidence"])
	}
	if me.Properties["auth_kind"] != "jwt" {
		t.Errorf("GET /me: auth_kind=%q, want jwt", me.Properties["auth_kind"])
	}
}

// TestEchoTrailingMiddlewareAuth — echo passes middleware AFTER the handler:
// `e.GET("/admin", h, jwtMiddleware)` → /admin auth_required, kind=jwt.
func TestEchoTrailingMiddlewareAuth(t *testing.T) {
	src := `package main
func main() {
	e := echo.New()
	e.GET("/admin", adminHandler, jwtMiddleware)
	e.GET("/public", publicHandler)
}
`
	ents := runEcho(t, src)

	admin := findEndpoint(t, ents, "GET /admin")
	if admin.Properties["auth_required"] != "true" {
		t.Errorf("GET /admin: auth_required=%q, want true (props: %v)", admin.Properties["auth_required"], admin.Properties)
	}
	if admin.Properties["auth_kind"] != "jwt" {
		t.Errorf("GET /admin: auth_kind=%q, want jwt", admin.Properties["auth_kind"])
	}
	if admin.Properties["auth_confidence"] != "high" {
		t.Errorf("GET /admin: auth_confidence=%q, want high", admin.Properties["auth_confidence"])
	}

	pub := findEndpoint(t, ents, "GET /public")
	if pub.Properties["auth_required"] == "true" {
		t.Errorf("GET /public: auth_required=true, want unprotected")
	}
}

// TestEchoGroupAuth — `g := e.Group("/admin", jwtMiddleware)` protects routes
// registered on g; the full prefixed path is composed but inline keying uses
// the route's own path.
func TestEchoGroupAuth(t *testing.T) {
	src := `package main
func main() {
	e := echo.New()
	g := e.Group("/admin", jwtMiddleware)
	g.GET("/users", listUsers)
	e.GET("/login", login)
}
`
	ents := runEcho(t, src)

	users := findEndpoint(t, ents, "GET /admin/users")
	if users.Properties["auth_required"] != "true" {
		t.Errorf("GET /admin/users: auth_required=%q, want true via group (props: %v)", users.Properties["auth_required"], users.Properties)
	}
	if users.Properties["auth_kind"] != "jwt" {
		t.Errorf("GET /admin/users: auth_kind=%q, want jwt", users.Properties["auth_kind"])
	}

	login := findEndpoint(t, ents, "GET /login")
	if login.Properties["auth_required"] == "true" {
		t.Errorf("GET /login: auth_required=true, want unprotected")
	}
}
