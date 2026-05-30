package cpp_test

// oatpp_routes_test.go — fixture tests for oatpp_routes.go

import "testing"

func TestOatppEndpointBasic(t *testing.T) {
	src := `ENDPOINT("GET", "/api/users", getUsers)`
	ents := extract(t, "custom_cpp_oatpp", fi("controller.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users") {
		t.Errorf("expected GET /api/users endpoint, got %v", ents)
	}
}

func TestOatppEndpointPost(t *testing.T) {
	src := `ENDPOINT("POST", "/api/users", createUser)`
	ents := extract(t, "custom_cpp_oatpp", fi("controller.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /api/users") {
		t.Errorf("expected POST /api/users endpoint, got %v", ents)
	}
}

func TestOatppEndpointAsync(t *testing.T) {
	src := `ENDPOINT_ASYNC("PUT", "/api/items/{id}", UpdateItemHandler)`
	ents := extract(t, "custom_cpp_oatpp", fi("controller.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "PUT /api/items/{id}") {
		t.Errorf("expected PUT /api/items/{id} async endpoint, got %v", ents)
	}
}

func TestOatppEndpointMultiple(t *testing.T) {
	src := `
ENDPOINT("GET", "/health", healthCheck)
ENDPOINT("POST", "/login", doLogin)
ENDPOINT_ASYNC("DELETE", "/api/users/{id}", deleteUser)
`
	ents := extract(t, "custom_cpp_oatpp", fi("controller.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /health") {
		t.Error("expected GET /health")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /login") {
		t.Error("expected POST /login")
	}
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /api/users/{id}") {
		t.Error("expected DELETE /api/users/{id}")
	}
}

func TestOatppNoMatch(t *testing.T) {
	src := `#include <oatpp/web/server/HttpConnectionHandler.hpp>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_oatpp", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestOatppWrongLanguage(t *testing.T) {
	src := `ENDPOINT("GET", "/api/items", getItems)`
	ents := extract(t, "custom_cpp_oatpp", fi("ctrl.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
