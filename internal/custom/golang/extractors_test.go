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
