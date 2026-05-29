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
