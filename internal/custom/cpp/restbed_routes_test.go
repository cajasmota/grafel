package cpp_test

// restbed_routes_test.go — fixture tests for restbed_routes.go

import "testing"

func TestRestbedSetMethodHandler(t *testing.T) {
	src := `
auto resource = make_shared<Resource>();
resource->set_path("/api/users");
resource->set_method_handler("GET", getUsersHandler);
`
	ents := extract(t, "custom_cpp_restbed", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users") {
		t.Errorf("expected GET /api/users endpoint, got %v", ents)
	}
}

func TestRestbedSetMethodHandlerPost(t *testing.T) {
	src := `
resource->set_path("/api/orders");
resource->set_method_handler("POST", createOrderHandler);
`
	ents := extract(t, "custom_cpp_restbed", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /api/orders") {
		t.Errorf("expected POST /api/orders endpoint, got %v", ents)
	}
}

func TestRestbedDotStyleAccess(t *testing.T) {
	src := `
resource.set_path("/health");
resource.set_method_handler("GET", healthHandler);
`
	ents := extract(t, "custom_cpp_restbed", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /health") {
		t.Errorf("expected GET /health endpoint from dot-style access, got %v", ents)
	}
}

func TestRestbedFallbackPath(t *testing.T) {
	// No set_path in file — falls back to <varname>
	src := `resource->set_method_handler("DELETE", deleteHandler);`
	ents := extract(t, "custom_cpp_restbed", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "DELETE <resource>") {
		t.Errorf("expected DELETE <resource> fallback, got %v", ents)
	}
}

func TestRestbedNoMatch(t *testing.T) {
	src := `#include <corvusoft/restbed/service.hpp>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_restbed", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestRestbedWrongLanguage(t *testing.T) {
	src := `
resource->set_path("/foo");
resource->set_method_handler("GET", fooHandler);
`
	ents := extract(t, "custom_cpp_restbed", fi("srv.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
