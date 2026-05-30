package cpp_test

// poco_routes_test.go — fixture tests for poco_routes.go

import "testing"

func TestPocoAddHandlerTemplate(t *testing.T) {
	src := `srv.addHandler<UserRequestHandler>("/api/users");`
	ents := extract(t, "custom_cpp_poco", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /api/users") {
		t.Errorf("expected ANY /api/users endpoint from addHandler<T>, got %v", ents)
	}
}

func TestPocoRouterAddGet(t *testing.T) {
	src := `router.add(HTTPRequest::HTTP_GET, "/api/items", new ItemFactory());`
	ents := extract(t, "custom_cpp_poco", fi("router.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/items") {
		t.Errorf("expected GET /api/items endpoint from router.add, got %v", ents)
	}
}

func TestPocoRouterAddPost(t *testing.T) {
	src := `router.add(HTTPRequest::HTTP_POST, "/api/items", new ItemPostFactory());`
	ents := extract(t, "custom_cpp_poco", fi("router.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /api/items") {
		t.Errorf("expected POST /api/items endpoint from router.add, got %v", ents)
	}
}

func TestPocoServerAddHandler(t *testing.T) {
	src := `server.addHandler("/health", healthHandler);`
	ents := extract(t, "custom_cpp_poco", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /health") {
		t.Errorf("expected ANY /health endpoint from server.addHandler, got %v", ents)
	}
}

func TestPocoNoMatch(t *testing.T) {
	src := `#include <Poco/Net/HTTPServer.h>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_poco", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestPocoWrongLanguage(t *testing.T) {
	src := `server.addHandler("/foo", fooHandler);`
	ents := extract(t, "custom_cpp_poco", fi("srv.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
