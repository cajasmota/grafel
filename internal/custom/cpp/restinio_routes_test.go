package cpp_test

// restinio_routes_test.go — fixture tests for restinio_routes.go

import "testing"

func TestRestinioHTTPGet(t *testing.T) {
	src := `router->http_get("/api/users", getUsersHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users") {
		t.Errorf("expected GET /api/users endpoint, got %v", ents)
	}
}

func TestRestinioHTTPPost(t *testing.T) {
	src := `router->http_post("/api/users", createUserHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /api/users") {
		t.Errorf("expected POST /api/users endpoint, got %v", ents)
	}
}

func TestRestinioHTTPPut(t *testing.T) {
	src := `router->http_put("/api/items/{id}", updateItemHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "PUT /api/items/{id}") {
		t.Errorf("expected PUT /api/items/{id} endpoint, got %v", ents)
	}
}

func TestRestinioHTTPDelete(t *testing.T) {
	src := `router->http_delete("/api/items/{id}", deleteItemHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /api/items/{id}") {
		t.Errorf("expected DELETE /api/items/{id} endpoint, got %v", ents)
	}
}

func TestRestinioDotAccess(t *testing.T) {
	src := `router.http_get("/health", healthHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /health") {
		t.Errorf("expected GET /health from dot-style access, got %v", ents)
	}
}

func TestRestinioAddHandler(t *testing.T) {
	src := `router->add_handler(restinio::http_connection_header_t::GET, "/api/status", statusHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/status") {
		t.Errorf("expected GET /api/status from add_handler, got %v", ents)
	}
}

func TestRestinioNoMatch(t *testing.T) {
	src := `#include <restinio/all.hpp>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_restinio", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestRestinioWrongLanguage(t *testing.T) {
	src := `router->http_get("/foo", fooHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("srv.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
