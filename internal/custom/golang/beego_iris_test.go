package golang_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Beego
// ---------------------------------------------------------------------------

func TestBeegoRunService(t *testing.T) {
	src := `func main() { web.Run() }`
	ents := extract(t, "custom_go_beego", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "beego_app") {
		t.Error("expected beego_app service from web.Run()")
	}
}

func TestBeegoMethodRouterMapping(t *testing.T) {
	src := `web.Router("/users", &UserController{}, "get:GetAll;post:Post")`
	ents := extract(t, "custom_go_beego", fi("router.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users from beego method-mapping")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users from beego method-mapping")
	}
}

func TestBeegoRouterNoMappingIsAny(t *testing.T) {
	src := `beego.Router("/health", &HealthController{})`
	ents := extract(t, "custom_go_beego", fi("router.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /health") {
		t.Error("expected ANY /health for RESTful-default beego route")
	}
}

func TestBeegoNamespaceComponent(t *testing.T) {
	src := `ns := web.NewNamespace("/api/v1")`
	ents := extract(t, "custom_go_beego", fi("router.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api/v1") {
		t.Error("expected /api/v1 namespace component")
	}
}

func TestBeegoAnnotationRoute(t *testing.T) {
	src := `
// @router /users [get,post]
func (c *UserController) GetAll() {}
`
	ents := extract(t, "custom_go_beego", fi("controller.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users from @router annotation")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users from @router annotation")
	}
}

func TestBeegoAutoRouter(t *testing.T) {
	src := `web.AutoRouter(&AdminController{})`
	ents := extract(t, "custom_go_beego", fi("router.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /Admin") {
		t.Error("expected ANY /Admin from AutoRouter (Controller suffix stripped)")
	}
}

func TestBeegoFixture(t *testing.T) {
	f := fixtureInput(t, "beego_routes.go", "go")
	ents := extract(t, "custom_go_beego", f)
	want := []string{"GET /users", "POST /users", "ANY /health"}
	for _, w := range want {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("fixture: expected operation %q", w)
		}
	}
	if !containsEntity(ents, "SCOPE.Component", "/api/v1") {
		t.Error("fixture: expected /api/v1 namespace component")
	}
	if !containsEntity(ents, "SCOPE.Service", "beego_app") {
		t.Error("fixture: expected beego_app service")
	}
}

func TestBeegoNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_beego", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Iris
// ---------------------------------------------------------------------------

func TestIrisApp(t *testing.T) {
	src := `app := iris.New()`
	ents := extract(t, "custom_go_iris", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "app") {
		t.Error("expected app as iris application service")
	}
}

func TestIrisRoutes(t *testing.T) {
	src := `
app := iris.New()
app.Get("/users", listUsers)
app.Post("/users", createUser)
`
	ents := extract(t, "custom_go_iris", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestIrisPartyPrefix(t *testing.T) {
	src := `
v1 := app.Party("/api/v1")
v1.Get("/orders", listOrders)
`
	ents := extract(t, "custom_go_iris", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api/v1") {
		t.Error("expected /api/v1 party component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/v1/orders") {
		t.Error("expected GET /api/v1/orders with party prefix resolved")
	}
}

func TestIrisNestedParty(t *testing.T) {
	src := `
v1 := app.Party("/api/v1")
admin := v1.Party("/admin")
admin.Delete("/users/{id}", deleteUser)
`
	ents := extract(t, "custom_go_iris", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /api/v1/admin/users/{id}") {
		t.Error("expected nested-party prefix resolved")
	}
}

func TestIrisHandleExplicitMethod(t *testing.T) {
	src := `app.Handle("PUT", "/profile", updateProfile)`
	ents := extract(t, "custom_go_iris", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "PUT /profile") {
		t.Error("expected PUT /profile from app.Handle")
	}
}

func TestIrisMiddleware(t *testing.T) {
	src := `app.Use(recoverMiddleware)`
	ents := extract(t, "custom_go_iris", fi("main.go", "go", src))
	if !containsEntity(ents, "SCOPE.Pattern", "recoverMiddleware") {
		t.Error("expected recoverMiddleware pattern")
	}
}

func TestIrisFixture(t *testing.T) {
	f := fixtureInput(t, "iris_routes.go", "go")
	ents := extract(t, "custom_go_iris", f)
	want := []string{
		"GET /health", "POST /login",
		"GET /api/v1/users", "POST /api/v1/users",
		"DELETE /api/v1/admin/users/{id}",
		"PUT /profile",
	}
	for _, w := range want {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("fixture: expected operation %q", w)
		}
	}
	if !containsEntity(ents, "SCOPE.Service", "app") {
		t.Error("fixture: expected app service")
	}
}

func TestIrisNoMatch(t *testing.T) {
	src := `package main`
	ents := extract(t, "custom_go_iris", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
