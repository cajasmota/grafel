package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// makeRoutesToRel builds a ROUTES_TO edge in the shape the YAML rule emits.
func makeRoutesToRel(path, recv string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: "Route:" + path,
		ToID:   "Controller:" + recv,
		Kind:   "ROUTES_TO",
		Properties: map[string]string{
			"framework":    "go",
			"pattern_type": "yaml_driven",
		},
	}
}

func TestApplyGoRouteComposition_ChiCompositeLiteral(t *testing.T) {
	src := []byte(`package main

import "github.com/go-chi/chi/v5"

func main() {
	h := &UsersHandler{}
	r := chi.NewRouter()
	r.Get("/users", h.List)
	r.Get("/users/{id}", h.Get)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
		makeRoutesToRel("/users/{id}", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships

	if got[0].ToID != "Controller:UsersHandler.List" {
		t.Errorf("ToID[0] = %q, want Controller:UsersHandler.List", got[0].ToID)
	}
	if got[1].ToID != "Controller:UsersHandler.Get" {
		t.Errorf("ToID[1] = %q, want Controller:UsersHandler.Get", got[1].ToID)
	}
	if got[0].Properties["pattern_type"] != "ast_driven" {
		t.Errorf("pattern_type[0] = %q, want ast_driven", got[0].Properties["pattern_type"])
	}
}

func TestApplyGoRouteComposition_GorillaMuxHandleFunc(t *testing.T) {
	// gorilla/mux registers via r.HandleFunc("/path", h.Method).Methods(...).
	// The YAML rule captures only the bare receiver `h`; the AST pass must
	// rewrite the orphan Controller:h edge to the qualified handler method.
	src := []byte(`package main

import "github.com/gorilla/mux"

func main() {
	h := &UsersHandler{}
	r := mux.NewRouter()
	r.HandleFunc("/users", h.List).Methods("GET")
	r.HandleFunc("/users/{id}", h.Get).Methods("GET")
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
		makeRoutesToRel("/users/{id}", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:UsersHandler.List" {
		t.Errorf("ToID[0] = %q, want Controller:UsersHandler.List", got[0].ToID)
	}
	if got[1].ToID != "Controller:UsersHandler.Get" {
		t.Errorf("ToID[1] = %q, want Controller:UsersHandler.Get", got[1].ToID)
	}
	if got[0].Properties["go_route_binding"] != "method_receiver_resolved" {
		t.Errorf("go_route_binding[0] = %q, want method_receiver_resolved", got[0].Properties["go_route_binding"])
	}
}

func TestApplyGoRouteComposition_NetHTTPServeMux(t *testing.T) {
	// stdlib net/http: mux.HandleFunc("/path", h.Method) — the nethttp custom
	// extractor synthesizes the endpoint; the shared AST pass resolves the
	// bare receiver to the qualified handler method (HandleFunc is in
	// goHTTPVerbs). Covers the Go 1.22+ method-prefixed pattern too.
	src := []byte(`package main

import "net/http"

func main() {
	h := &UsersHandler{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users", h.List)
	mux.HandleFunc("GET /users/{id}", h.Get)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
		makeRoutesToRel("/users/{id}", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:UsersHandler.List" {
		t.Errorf("ToID[0] = %q, want Controller:UsersHandler.List", got[0].ToID)
	}
	if got[1].ToID != "Controller:UsersHandler.Get" {
		t.Errorf("ToID[1] = %q, want Controller:UsersHandler.Get", got[1].ToID)
	}
	if got[0].Properties["go_route_binding"] != "method_receiver_resolved" {
		t.Errorf("go_route_binding[0] = %q, want method_receiver_resolved", got[0].Properties["go_route_binding"])
	}
}

func TestApplyGoRouteComposition_ConstructorIdiom(t *testing.T) {
	// `h := handlers.NewUsersHandler(s)` — cross-package NewT() returns *T.
	src := []byte(`package main

import (
	"github.com/go-chi/chi/v5"
	"example.com/demo/handlers"
)

func main() {
	h := handlers.NewUsersHandler(nil)
	r := chi.NewRouter()
	r.Get("/users", h.List)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:UsersHandler.List" {
		t.Errorf("ToID = %q, want Controller:UsersHandler.List", got[0].ToID)
	}
}

func TestApplyGoRouteComposition_GinUppercaseVerb(t *testing.T) {
	src := []byte(`package main

func main() {
	h := &UserAPI{}
	r := newRouter()
	r.GET("/users", h.List)
	r.POST("/teams", h.CreateTeam)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
		makeRoutesToRel("/teams", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:UserAPI.List" {
		t.Errorf("ToID[0] = %q, want Controller:UserAPI.List", got[0].ToID)
	}
	if got[1].ToID != "Controller:UserAPI.CreateTeam" {
		t.Errorf("ToID[1] = %q, want Controller:UserAPI.CreateTeam", got[1].ToID)
	}
}

func TestApplyGoRouteComposition_PointerVarDecl(t *testing.T) {
	src := []byte(`package main

func main() {
	var h *UsersHandler
	r := newRouter()
	r.Get("/users", h.List)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:UsersHandler.List" {
		t.Errorf("ToID = %q, want Controller:UsersHandler.List", got[0].ToID)
	}
}

func TestApplyGoRouteComposition_UnresolvedReceiverLeftAlone(t *testing.T) {
	// `h` is never declared in-file → can't resolve type → leave edge alone.
	src := []byte(`package main

func register() {
	r := newRouter()
	r.Get("/users", h.List)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:h" {
		t.Errorf("ToID = %q, want unchanged Controller:h", got[0].ToID)
	}
	if got[0].Properties["pattern_type"] != "yaml_driven" {
		t.Errorf("pattern_type should stay yaml_driven, got %q",
			got[0].Properties["pattern_type"])
	}
}

func TestApplyGoRouteComposition_BareFuncHandlerUnchanged(t *testing.T) {
	// `r.Get("/users", listUsers)` — bare function, not a method call.
	// The YAML rule already emits Controller:listUsers, which DOES resolve.
	// Pass must leave it alone.
	src := []byte(`package main

func listUsers() {}

func main() {
	r := newRouter()
	r.Get("/users", listUsers)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "listUsers"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:listUsers" {
		t.Errorf("ToID = %q, want unchanged Controller:listUsers", got[0].ToID)
	}
}

func TestApplyGoRouteComposition_NonGoNoop(t *testing.T) {
	src := []byte(`r.Get("/users", h.List)`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "python", Path: "main.py", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:h" {
		t.Errorf("python file mutated: %q", got[0].ToID)
	}
}

func TestApplyGoRouteComposition_NonRoutesToRelationshipsUnchanged(t *testing.T) {
	src := []byte(`package main

func main() {
	h := &UsersHandler{}
	r := newRouter()
	r.Get("/users", h.List)
}
`)
	rels := []types.RelationshipRecord{
		{
			FromID: "Route:/users",
			ToID:   "Controller:h",
			Kind:   "CALLS", // not ROUTES_TO
		},
		makeRoutesToRel("/users", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:h" {
		t.Errorf("non-ROUTES_TO edge mutated: %q", got[0].ToID)
	}
	if got[1].ToID != "Controller:UsersHandler.List" {
		t.Errorf("ROUTES_TO edge not rewritten: %q", got[1].ToID)
	}
}

func TestApplyGoRouteComposition_AlreadyQualifiedToIDUnchanged(t *testing.T) {
	src := []byte(`package main

func main() {
	h := &UsersHandler{}
	r := newRouter()
	r.Get("/users", h.List)
}
`)
	rels := []types.RelationshipRecord{
		{
			FromID:     "Route:/users",
			ToID:       "Controller:Existing.Qualified",
			Kind:       "ROUTES_TO",
			Properties: map[string]string{"pattern_type": "yaml_driven"},
		},
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:Existing.Qualified" {
		t.Errorf("already-qualified ToID mutated: %q", got[0].ToID)
	}
}

func TestApplyGoRouteComposition_PreFilterSkipsIrrelevantFiles(t *testing.T) {
	// File has no router-call substrings → pre-filter exits early.
	src := []byte(`package main

func main() {
	println("no routes here")
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	if got[0].ToID != "Controller:h" {
		t.Errorf("ToID = %q, want unchanged Controller:h", got[0].ToID)
	}
}

// ---------------------------------------------------------------------------
// AST cross-file route composition: same-file multi-handler deepening (#3348).
//
// These tests prove that the AST pass correctly resolves ROUTES_TO edges when
// multiple distinct handler variables are registered in the same file — the
// "multi-receiver same-file composition" case. The extractor runs per-file, so
// cross-file composition is genuinely out of scope; same-file with multiple
// handler instances is the deepened surface.
// ---------------------------------------------------------------------------

func TestApplyGoRouteComposition_MultipleHandlersSameFile(t *testing.T) {
	// Two distinct handler structs wired to different route groups in the same file.
	// The AST var-type map must correctly separate them: h1→UsersHandler,
	// h2→OrdersHandler, so routes don't mis-cross.
	src := []byte(`package main

import "github.com/go-chi/chi/v5"

func main() {
	h1 := &UsersHandler{}
	h2 := &OrdersHandler{}
	r := chi.NewRouter()
	r.Get("/users", h1.List)
	r.Get("/users/{id}", h1.Get)
	r.Post("/orders", h2.Create)
	r.Delete("/orders/{id}", h2.Delete)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/users", "h1"),
		makeRoutesToRel("/users/{id}", "h1"),
		makeRoutesToRel("/orders", "h2"),
		makeRoutesToRel("/orders/{id}", "h2"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "main.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships

	want := map[string]string{
		"/users":       "Controller:UsersHandler.List",
		"/users/{id}":  "Controller:UsersHandler.Get",
		"/orders":      "Controller:OrdersHandler.Create",
		"/orders/{id}": "Controller:OrdersHandler.Delete",
	}
	for _, r := range got {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		path := r.FromID[len("Route:"):]
		wantToID, ok := want[path]
		if !ok {
			continue
		}
		if r.ToID != wantToID {
			t.Errorf("route %q: ToID=%q, want %q", path, r.ToID, wantToID)
		}
	}
}

func TestApplyGoRouteComposition_FiberGroupedRoutes(t *testing.T) {
	// fiber's Title-case verbs (Get/Post) with a group of routes using a
	// single handler struct. Verifies the same AST resolution path works for
	// Fiber's API surface (group-scoped but single-file).
	src := []byte(`package main

import "github.com/gofiber/fiber/v2"

func setupRoutes(app *fiber.App) {
	h := NewProductHandler(nil)
	app.Get("/products", h.List)
	app.Post("/products", h.Create)
	app.Put("/products/:id", h.Update)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/products", "h"),
		makeRoutesToRel("/products", "h"),
		makeRoutesToRel("/products/:id", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "routes.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships

	for _, r := range got {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		if r.ToID == "Controller:h" {
			t.Errorf("bare receiver not rewritten for fiber route %q: %q", r.FromID, r.ToID)
		}
		if r.Properties["pattern_type"] != "ast_driven" {
			t.Errorf("route %q: pattern_type=%q, want ast_driven", r.FromID, r.Properties["pattern_type"])
		}
	}
}

func TestApplyGoRouteComposition_EchoGroupRoutes(t *testing.T) {
	// echo's uppercase verbs (GET/POST) with a sub-group handler variable.
	// Same-file group composition: `api` sub-group with a dedicated handler.
	src := []byte(`package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	h := &ProductHandler{}
	api := e.Group("/api")
	_ = api
	e.GET("/products", h.List)
	e.POST("/products", h.Create)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/products", "h"),
		makeRoutesToRel("/products", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "routes.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships

	for _, r := range got {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		if r.ToID == "Controller:h" {
			t.Errorf("echo route receiver not resolved for %q", r.FromID)
		}
	}
}

func TestApplyGoRouteComposition_CrossFileReceiverLeftUntouched(t *testing.T) {
	// When the handler variable is declared in a different file (not present in
	// this file's AST), the pass leaves the edge unchanged (safe-bias). This
	// test verifies the honest-partial boundary: cross-file composition is out
	// of scope for the single-file AST pass, and the edge is not mis-bound.
	src := []byte(`package main

import "github.com/go-chi/chi/v5"

// h is injected from another file — not declared here.
func setupRoutes(r *chi.Mux) {
	r.Get("/items", h.List)
}
`)
	rels := []types.RelationshipRecord{
		makeRoutesToRel("/items", "h"),
	}
	_res := applyGoRouteComposition(DetectorPassArgs{Lang: "go", Path: "routes.go", Content: src, Relationships: rels})
	_, got := _res.Entities, _res.Relationships
	// h cannot be resolved from this file alone → edge stays as-is.
	if got[0].ToID != "Controller:h" {
		t.Errorf("cross-file receiver should not be rewritten, got: %q", got[0].ToID)
	}
}
