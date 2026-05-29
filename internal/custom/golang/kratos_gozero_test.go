package golang_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Kratos (go-kratos) — proto/codegen-driven *_http.pb.go transport.
// ---------------------------------------------------------------------------

func TestKratosRegisterService(t *testing.T) {
	src := `
func RegisterGreeterHTTPServer(s *http.Server, srv GreeterHTTPServer) {
	r := s.Route("/")
	r.GET("/hello", _Greeter_SayHello0_HTTP_Handler(srv))
}
`
	ents := extract(t, "custom_go_kratos", fi("greeter_http.pb.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "Greeter") {
		t.Error("expected Greeter service from RegisterGreeterHTTPServer")
	}
}

func TestKratosVerbRoutes(t *testing.T) {
	src := `
func RegisterGreeterHTTPServer(s *http.Server, srv GreeterHTTPServer) {
	r := s.Route("/")
	r.GET("/hello/{name}", _Greeter_SayHello0_HTTP_Handler(srv))
	r.POST("/greetings", _Greeter_CreateGreeting0_HTTP_Handler(srv))
}
`
	ents := extract(t, "custom_go_kratos", fi("greeter_http.pb.go", "go", src))
	for _, w := range []string{"GET /hello/{name}", "POST /greetings"} {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("expected route %q", w)
		}
	}
}

func TestKratosHandlerAttribution(t *testing.T) {
	src := `
func RegisterGreeterHTTPServer(s *http.Server, srv GreeterHTTPServer) {
	r := s.Route("/")
	r.GET("/hello", _Greeter_SayHello0_HTTP_Handler(srv))
}
`
	ents := extractFull(t, "custom_go_kratos", fi("greeter_http.pb.go", "go", src))
	if got := handlerFor(ents, "SCOPE.Operation", "GET /hello"); got != "_Greeter_SayHello0_HTTP_Handler" {
		t.Errorf("expected generated handler wrapper, got %q", got)
	}
	// service_method recovered from the wrapper name.
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Name == "GET /hello" {
			if e.Props["service_method"] != "SayHello" {
				t.Errorf("expected service_method SayHello, got %q", e.Props["service_method"])
			}
		}
	}
}

func TestKratosRoutePrefix(t *testing.T) {
	src := `
func RegisterGreeterHTTPServer(s *http.Server, srv GreeterHTTPServer) {
	r := s.Route("/v1")
	r.GET("/hello", _Greeter_SayHello0_HTTP_Handler(srv))
}
`
	ents := extract(t, "custom_go_kratos", fi("greeter_http.pb.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/v1") {
		t.Error("expected /v1 route-prefix component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /v1/hello") {
		t.Error("expected GET /v1/hello with prefix resolved")
	}
}

func TestKratosFixture(t *testing.T) {
	f := fixtureInput(t, "kratos_http.pb.go", "go")
	ents := extractFull(t, "custom_go_kratos", f)
	wantHandlers := map[string]string{
		"GET /helloworld/{name}": "_Greeter_SayHello0_HTTP_Handler",
		"GET /greetings":         "_Greeter_ListGreetings0_HTTP_Handler",
		"POST /greetings":        "_Greeter_CreateGreeting0_HTTP_Handler",
		"DELETE /greetings/{id}": "_Greeter_DeleteGreeting0_HTTP_Handler",
	}
	for op, h := range wantHandlers {
		if got := handlerFor(ents, "SCOPE.Operation", op); got != h {
			t.Errorf("fixture: %q expected handler %q, got %q", op, h, got)
		}
	}
	if !containsEntity(summaries(ents), "SCOPE.Service", "Greeter") {
		t.Error("fixture: expected Greeter service")
	}
}

func TestKratosNoMatch(t *testing.T) {
	// Hand-written kratos service code carries no generated registration site.
	ents := extract(t, "custom_go_kratos", fi("service.go", "go", `package service
func (s *GreeterService) SayHello(ctx context.Context) error { return nil }`))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// go-zero (goctl-generated handler/routes.go).
// ---------------------------------------------------------------------------

func TestGoZeroConstantVerbRoute(t *testing.T) {
	src := `
server.AddRoutes(
	[]rest.Route{
		{Method: http.MethodGet, Path: "/users/:id", Handler: user.GetUserHandler(serverCtx)},
	},
)
`
	ents := extract(t, "custom_go_go_zero", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/:id") {
		t.Error("expected GET /users/:id from http.MethodGet constant")
	}
}

func TestGoZeroStringVerbRoute(t *testing.T) {
	src := `
server.AddRoutes([]rest.Route{
	{Method: "POST", Path: "/users", Handler: CreateUserHandler(serverCtx)},
})
`
	ents := extract(t, "custom_go_go_zero", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users from string-literal verb")
	}
}

func TestGoZeroHandlerAttribution(t *testing.T) {
	src := `
server.AddRoutes([]rest.Route{
	{Method: http.MethodGet, Path: "/users/:id", Handler: user.GetUserHandler(serverCtx)},
})
`
	ents := extractFull(t, "custom_go_go_zero", fi("routes.go", "go", src))
	if got := handlerFor(ents, "SCOPE.Operation", "GET /users/:id"); got != "user.GetUserHandler" {
		t.Errorf("expected handler user.GetUserHandler, got %q", got)
	}
}

func TestGoZeroWithPrefix(t *testing.T) {
	src := `
server.AddRoutes(
	[]rest.Route{
		{Method: http.MethodGet, Path: "/users", Handler: ListUsersHandler(serverCtx)},
	},
	rest.WithPrefix("/api/v1"),
)
`
	ents := extract(t, "custom_go_go_zero", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Component", "/api/v1") {
		t.Error("expected /api/v1 prefix component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/v1/users") {
		t.Error("expected GET /api/v1/users with prefix resolved")
	}
}

func TestGoZeroServerScope(t *testing.T) {
	src := `
server.AddRoutes([]rest.Route{
	{Method: "GET", Path: "/health", Handler: HealthHandler(ctx)},
})
`
	ents := extract(t, "custom_go_go_zero", fi("routes.go", "go", src))
	if !containsEntity(ents, "SCOPE.Service", "server") {
		t.Error("expected server SCOPE.Service")
	}
}

func TestGoZeroFixture(t *testing.T) {
	f := fixtureInput(t, "go_zero_routes.go", "go")
	ents := extractFull(t, "custom_go_go_zero", f)
	wantHandlers := map[string]string{
		"GET /api/v1/users/:id":    "user.GetUserHandler",
		"POST /api/v1/users":       "user.CreateUserHandler",
		"DELETE /api/v1/users/:id": "user.DeleteUserHandler",
		"GET /health":              "HealthHandler",
	}
	for op, h := range wantHandlers {
		if got := handlerFor(ents, "SCOPE.Operation", op); got != h {
			t.Errorf("fixture: %q expected handler %q, got %q", op, h, got)
		}
	}
	if !containsEntity(summaries(ents), "SCOPE.Component", "/api/v1") {
		t.Error("fixture: expected /api/v1 prefix component")
	}
}

func TestGoZeroNoMatch(t *testing.T) {
	ents := extract(t, "custom_go_go_zero", fi("main.go", "go", `package main`))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
