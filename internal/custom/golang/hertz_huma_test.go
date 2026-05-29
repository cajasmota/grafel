package golang_test

import (
	"testing"
)

// handlerFor returns the "handler" property of the first entity matching
// kind+name, or "" if absent. Reuses fullEntity from middleware_auth_test.go.
func handlerFor(ents []fullEntity, kind, name string) string {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return e.Props["handler"]
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Hertz (CloudWeGo)
// ---------------------------------------------------------------------------

func TestHertzEngine(t *testing.T) {
	ents := extract(t, "custom_go_hertz", fi("main.go", "go", `h := server.Default()`))
	if !containsEntity(ents, "SCOPE.Service", "h") {
		t.Error("expected h as hertz engine service")
	}
}

func TestHertzEngineNew(t *testing.T) {
	ents := extract(t, "custom_go_hertz", fi("main.go", "go", `h := server.New(server.WithHostPorts(":8080"))`))
	if !containsEntity(ents, "SCOPE.Service", "h") {
		t.Error("expected h from server.New")
	}
}

func TestHertzVerbRoutes(t *testing.T) {
	src := `
h.GET("/users", listUsers)
h.POST("/users", createUser)
h.DELETE("/users/:id", deleteUser)
`
	ents := extract(t, "custom_go_hertz", fi("routes.go", "go", src))
	for _, w := range []string{"GET /users", "POST /users", "DELETE /users/:id"} {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("expected route %q", w)
		}
	}
}

func TestHertzHandlerAttribution(t *testing.T) {
	src := `h.GET("/users", listUsers)`
	ents := extractFull(t, "custom_go_hertz", fi("routes.go", "go", src))
	if got := handlerFor(ents, "SCOPE.Operation", "GET /users"); got != "listUsers" {
		t.Errorf("expected handler listUsers, got %q", got)
	}
}

func TestHertzHandlerAttributionWithMiddleware(t *testing.T) {
	// Verb call with inline middleware before the final handler argument.
	src := `h.GET("/users", JWTAuth(), listUsers)`
	ents := extractFull(t, "custom_go_hertz", fi("routes.go", "go", src))
	if got := handlerFor(ents, "SCOPE.Operation", "GET /users"); got != "listUsers" {
		t.Errorf("expected trailing handler listUsers, got %q", got)
	}
}

func TestHertzGroupPrefix(t *testing.T) {
	src := `
api := h.Group("/api/v1")
api.GET("/health", healthCheck)
`
	ents := extract(t, "custom_go_hertz", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api/v1") {
		t.Error("expected /api/v1 group component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/v1/health") {
		t.Error("expected GET /api/v1/health with group prefix resolved")
	}
}

func TestHertzNestedGroupPrefix(t *testing.T) {
	src := `
api := h.Group("/api/v1")
admin := api.Group("/admin")
admin.GET("/stats", adminStats)
`
	ents := extract(t, "custom_go_hertz", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/v1/admin/stats") {
		t.Error("expected nested group prefix resolution")
	}
}

func TestHertzStatic(t *testing.T) {
	ents := extract(t, "custom_go_hertz", fi("main.go", "go", `h.Static("/assets", "./static")`))
	if !containsEntity(ents, "SCOPE.Operation", "GET /assets") {
		t.Error("expected GET /assets static mount")
	}
}

func TestHertzMiddleware(t *testing.T) {
	ents := extract(t, "custom_go_hertz", fi("main.go", "go", `h.Use(RecoveryMiddleware)`))
	if !containsEntity(ents, "SCOPE.Pattern", "RecoveryMiddleware") {
		t.Error("expected RecoveryMiddleware pattern")
	}
}

func TestHertzNoRoute(t *testing.T) {
	ents := extract(t, "custom_go_hertz", fi("main.go", "go", `h.NoRoute(notFound)`))
	if !containsEntity(ents, "SCOPE.Pattern", "NoRoute") {
		t.Error("expected NoRoute error-handler pattern")
	}
}

func TestHertzFixture(t *testing.T) {
	f := fixtureInput(t, "hertz_routes.go", "go")
	ents := extractFull(t, "custom_go_hertz", f)
	wantHandlers := map[string]string{
		"GET /":                    "indexHandler",
		"POST /login":              "loginHandler",
		"GET /api/v1/users":        "listUsers",
		"POST /api/v1/users":       "createUser",
		"DELETE /api/v1/users/:id": "deleteUser",
		"GET /api/v1/admin/stats":  "adminStats",
	}
	for op, h := range wantHandlers {
		if got := handlerFor(ents, "SCOPE.Operation", op); got != h {
			t.Errorf("fixture: %q expected handler %q, got %q", op, h, got)
		}
	}
	summaries := summaries(ents)
	if !containsEntity(summaries, "SCOPE.Service", "h") {
		t.Error("fixture: expected h engine service")
	}
	if !containsEntity(summaries, "SCOPE.Operation", "GET /assets") {
		t.Error("fixture: expected static mount")
	}
}

func TestHertzNoMatch(t *testing.T) {
	ents := extract(t, "custom_go_hertz", fi("main.go", "go", `package main`))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Huma (OpenAPI-first)
// ---------------------------------------------------------------------------

func TestHumaStringVerb(t *testing.T) {
	src := `
import "github.com/danielgtaylor/huma/v2"
huma.Register(api, huma.Operation{Method: "POST", Path: "/users"}, createUser)
`
	ents := extract(t, "custom_go_huma", fi("router.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users from string-literal verb")
	}
}

func TestHumaConstantVerb(t *testing.T) {
	src := `
import "github.com/danielgtaylor/huma/v2"
huma.Register(api, huma.Operation{Method: http.MethodGet, Path: "/users/{id}"}, getUser)
`
	ents := extract(t, "custom_go_huma", fi("router.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{id}") {
		t.Error("expected GET /users/{id} from http.MethodGet constant")
	}
}

func TestHumaHandlerAttribution(t *testing.T) {
	src := `
import "github.com/danielgtaylor/huma/v2"
huma.Register(api, huma.Operation{Method: "GET", Path: "/users/{id}"}, getUser)
`
	ents := extractFull(t, "custom_go_huma", fi("router.go", "go", src))
	if got := handlerFor(ents, "SCOPE.Operation", "GET /users/{id}"); got != "getUser" {
		t.Errorf("expected handler getUser, got %q", got)
	}
}

func TestHumaIncompleteOperationSkipped(t *testing.T) {
	// Missing Path -> would fail at huma runtime too; emit nothing.
	src := `
import "github.com/danielgtaylor/huma/v2"
huma.Register(api, huma.Operation{Method: "GET"}, getUser)
`
	ents := extract(t, "custom_go_huma", fi("router.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for incomplete Operation, got %d", len(ents))
	}
}

func TestHumaFixture(t *testing.T) {
	f := fixtureInput(t, "huma_routes.go", "go")
	ents := extractFull(t, "custom_go_huma", f)
	wantHandlers := map[string]string{
		"GET /users/{id}":    "getUser",
		"POST /users":        "createUser",
		"DELETE /users/{id}": "deleteUser",
	}
	for op, h := range wantHandlers {
		if got := handlerFor(ents, "SCOPE.Operation", op); got != h {
			t.Errorf("fixture: %q expected handler %q, got %q", op, h, got)
		}
	}
}

func TestHumaNoMatch(t *testing.T) {
	ents := extract(t, "custom_go_huma", fi("main.go", "go", `package main`))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// summaries projects full entities down to the kind/name shape consumed by the
// shared containsEntity helper.
func summaries(ents []fullEntity) []entitySummary {
	out := make([]entitySummary, 0, len(ents))
	for _, e := range ents {
		out = append(out, entitySummary{Kind: e.Kind, Name: e.Name})
	}
	return out
}
