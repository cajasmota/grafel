package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/golang"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Gin
// ---------------------------------------------------------------------------

func TestGinEngine(t *testing.T) {
	src := `r := gin.Default()`
	ents := extract(t, "custom_go_gin", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "r") {
		t.Error("expected r as gin engine service")
	}
}

func TestGinRoutes(t *testing.T) {
	src := `
r := gin.Default()
r.GET("/users", listUsers)
r.POST("/users", createUser)
r.DELETE("/users/:id", deleteUser)
`
	ents := extract(t, "custom_go_gin", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestGinGroupWithPrefix(t *testing.T) {
	src := `
api := r.Group("/api")
api.GET("/health", healthCheck)
`
	ents := extract(t, "custom_go_gin", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api") {
		t.Error("expected /api group component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/health") {
		t.Error("expected GET /api/health with group prefix resolved")
	}
}

func TestGinMiddleware(t *testing.T) {
	src := `r.Use(cors.Default(), gin.Logger())`
	ents := extract(t, "custom_go_gin", fi("main.go", "go", src))
	if len(ents) == 0 {
		t.Error("expected middleware patterns")
	}
}

func TestGinNoMatch(t *testing.T) {
	src := `package main\nfunc main() { fmt.Println("hello") }`
	ents := extract(t, "custom_go_gin", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Echo
// ---------------------------------------------------------------------------

func TestEchoEngine(t *testing.T) {
	src := `e := echo.New()`
	ents := extract(t, "custom_go_echo", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "e") {
		t.Error("expected e as echo engine service")
	}
}

func TestEchoRoutes(t *testing.T) {
	src := `
e := echo.New()
e.GET("/users", listUsers)
e.POST("/users", createUser)
`
	ents := extract(t, "custom_go_echo", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestEchoGroupPrefix(t *testing.T) {
	src := `
g := e.Group("/v1")
g.GET("/orders", listOrders)
`
	ents := extract(t, "custom_go_echo", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /v1/orders") {
		t.Error("expected GET /v1/orders with group prefix")
	}
}

func TestEchoNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_echo", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Fiber
// ---------------------------------------------------------------------------

func TestFiberEngine(t *testing.T) {
	src := `app := fiber.New()`
	ents := extract(t, "custom_go_fiber", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "app") {
		t.Error("expected app as fiber engine service")
	}
}

func TestFiberRoutes(t *testing.T) {
	src := `
app := fiber.New()
app.Get("/users", listUsers)
app.Post("/users", createUser)
`
	ents := extract(t, "custom_go_fiber", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestFiberGroupPrefix(t *testing.T) {
	src := `
api := app.Group("/api")
api.Get("/items", listItems)
`
	ents := extract(t, "custom_go_fiber", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/items") {
		t.Error("expected GET /api/items with group prefix")
	}
}

func TestFiberNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_fiber", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Chi
// ---------------------------------------------------------------------------

func TestChiRouter(t *testing.T) {
	src := `r := chi.NewRouter()`
	ents := extract(t, "custom_go_chi", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "r") {
		t.Error("expected r as chi router service")
	}
}

func TestChiRoutes(t *testing.T) {
	src := `
r := chi.NewRouter()
r.Get("/users", listUsers)
r.Post("/users", createUser)
r.Delete("/users/{id}", deleteUser)
`
	ents := extract(t, "custom_go_chi", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /users/{id}") {
		t.Error("expected DELETE /users/{id} route")
	}
}

func TestChiRouteGroup(t *testing.T) {
	src := `
r.Route("/api", func(r chi.Router) {
	r.Get("/health", healthCheck)
})
`
	ents := extract(t, "custom_go_chi", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api") {
		t.Error("expected /api route-group component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /health") {
		t.Error("expected GET /health route inside group")
	}
}

func TestChiHandleAndMount(t *testing.T) {
	src := `
r.HandleFunc("/legacy", legacyHandler)
r.Mount("/admin", adminRouter)
`
	ents := extract(t, "custom_go_chi", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /legacy") {
		t.Error("expected ANY /legacy from HandleFunc")
	}
	if !containsEntity(ents, "SCOPE.Component", "/admin") {
		t.Error("expected /admin mount component")
	}
}

func TestChiMiddleware(t *testing.T) {
	src := `r.Use(middleware.Logger)`
	ents := extract(t, "custom_go_chi", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Pattern", "middleware.Logger") {
		t.Error("expected middleware.Logger pattern")
	}
}

func TestChiNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_chi", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Gorilla Mux
// ---------------------------------------------------------------------------

func TestGorillaRouter(t *testing.T) {
	src := `r := mux.NewRouter()`
	ents := extract(t, "custom_go_gorilla_mux", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "r") {
		t.Error("expected r as gorilla router service")
	}
}

func TestGorillaRoutesWithMethods(t *testing.T) {
	src := `
r := mux.NewRouter()
r.HandleFunc("/users", listUsers).Methods("GET")
r.HandleFunc("/users", createUser).Methods("POST")
`
	ents := extract(t, "custom_go_gorilla_mux", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestGorillaRouteNoMethodsIsAny(t *testing.T) {
	src := `
r := mux.NewRouter()
r.HandleFunc("/health", healthCheck)
`
	ents := extract(t, "custom_go_gorilla_mux", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /health") {
		t.Error("expected ANY /health for un-method-constrained route")
	}
}

func TestGorillaSubrouterPrefix(t *testing.T) {
	src := `
api := r.PathPrefix("/api").Subrouter()
api.HandleFunc("/orders", listOrders).Methods("GET")
`
	ents := extract(t, "custom_go_gorilla_mux", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api") {
		t.Error("expected /api subrouter component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/orders") {
		t.Error("expected GET /api/orders with subrouter prefix")
	}
}

func TestGorillaNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_gorilla_mux", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// net/http (stdlib)
// ---------------------------------------------------------------------------

func TestNetHTTPServeMux(t *testing.T) {
	src := `mux := http.NewServeMux()`
	ents := extract(t, "custom_go_nethttp", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "mux") {
		t.Error("expected mux as net/http ServeMux service")
	}
}

func TestNetHTTPDefaultMuxRoutes(t *testing.T) {
	src := `
http.HandleFunc("/users", listUsers)
http.Handle("/static", fileServer)
`
	ents := extract(t, "custom_go_nethttp", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /users") {
		t.Error("expected ANY /users from http.HandleFunc")
	}
	if !containsEntity(ents, "SCOPE.Operation", "ANY /static") {
		t.Error("expected ANY /static from http.Handle")
	}
}

func TestNetHTTPMuxScopedRoutes(t *testing.T) {
	src := `
mux := http.NewServeMux()
mux.HandleFunc("/health", healthCheck)
mux.Handle("/metrics", promHandler)
`
	ents := extract(t, "custom_go_nethttp", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "mux") {
		t.Error("expected mux service")
	}
	if !containsEntity(ents, "SCOPE.Operation", "ANY /health") {
		t.Error("expected ANY /health from mux.HandleFunc")
	}
	if !containsEntity(ents, "SCOPE.Operation", "ANY /metrics") {
		t.Error("expected ANY /metrics from mux.Handle")
	}
}

func TestNetHTTPMethodPrefixedPatterns(t *testing.T) {
	// Go 1.22+ method-prefixed patterns.
	src := `
mux := http.NewServeMux()
mux.HandleFunc("GET /users/{id}", getUser)
mux.HandleFunc("POST /users", createUser)
mux.HandleFunc("DELETE /users/{id}", deleteUser)
`
	ents := extract(t, "custom_go_nethttp", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{id}") {
		t.Error("expected GET /users/{id} with method token split off")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users")
	}
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /users/{id}") {
		t.Error("expected DELETE /users/{id}")
	}
}

func TestNetHTTPNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_nethttp", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// GORM
// ---------------------------------------------------------------------------

func TestGORMOpen(t *testing.T) {
	src := `db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})`
	ents := extract(t, "custom_go_gorm", fi("db.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "db") {
		t.Error("expected db as GORM service")
	}
}

func TestGORMModel(t *testing.T) {
	src := `
type User struct {
    gorm.Model
    Name string
}
type Product struct {
    gorm.Model
    Price float64
}
`
	ents := extract(t, "custom_go_gorm", fi("models.go", "go", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User GORM model schema")
	}
	if !containsEntity(ents, "SCOPE.Schema", "Product") {
		t.Error("expected Product GORM model schema")
	}
}

func TestGORMAutoMigrate(t *testing.T) {
	src := `db.AutoMigrate(&User{}, &Order{}, &Product{})`
	ents := extract(t, "custom_go_gorm", fi("migrate.go", "go", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User from AutoMigrate")
	}
	if !containsEntity(ents, "SCOPE.Schema", "Order") {
		t.Error("expected Order from AutoMigrate")
	}
}

func TestGORMQuery(t *testing.T) {
	src := `
db.Find(&users)
db.Create(&user)
db.First(&user, id)
`
	ents := extract(t, "custom_go_gorm", fi("repo.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "find:users") {
		t.Error("expected find:users operation")
	}
	if !containsEntity(ents, "SCOPE.Operation", "create:user") {
		t.Error("expected create:user operation")
	}
}

func TestGORMNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_gorm", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
