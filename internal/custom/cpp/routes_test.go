package cpp_test

// routes_test.go — per-framework golden fixture tests for
// drogon_routes.go, crow_routes.go, pistache_routes.go, cpprestsdk_routes.go.

import "testing"

// ---------------------------------------------------------------------------
// Drogon
// ---------------------------------------------------------------------------

func TestDrogonAddMethodTo(t *testing.T) {
	src := `
ADD_METHOD_TO(UserCtrl::getUser, "/api/users/{id}", Get);
ADD_METHOD_TO(UserCtrl::createUser, "/api/users", Post);
`
	ents := extract(t, "custom_cpp_drogon", fi("routes.cc", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users/{id}") {
		t.Error("expected GET /api/users/{id} endpoint from ADD_METHOD_TO")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /api/users") {
		t.Error("expected POST /api/users endpoint from ADD_METHOD_TO")
	}
}

func TestDrogonAddMethodToMultiVerb(t *testing.T) {
	src := `ADD_METHOD_TO(MyCtrl::handler, "/api/items", Get, Post);`
	ents := extract(t, "custom_cpp_drogon", fi("routes.cc", "cpp", src))
	// Multi-verb: GET,POST /api/items
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Name == "GET,POST /api/items" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected GET,POST /api/items for multi-verb ADD_METHOD_TO, got %v", ents)
	}
}

func TestDrogonMethodAdd(t *testing.T) {
	src := `METHOD_ADD(ItemCtrl, "/items/{id}", Get);`
	ents := extract(t, "custom_cpp_drogon", fi("ctrl.cc", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /items/{id}") {
		t.Error("expected GET /items/{id} endpoint from METHOD_ADD")
	}
}

func TestDrogonRegisterHandler(t *testing.T) {
	src := `app().registerHandler("/health", healthHandler, {Get});`
	ents := extract(t, "custom_cpp_drogon", fi("app.cc", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /health") {
		t.Error("expected GET /health endpoint from registerHandler")
	}
}

func TestDrogonRegisterHandlerMultiVerb(t *testing.T) {
	src := `app().registerHandler("/upload", uploadHandler, {Post, Put});`
	ents := extract(t, "custom_cpp_drogon", fi("app.cc", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Name == "POST,PUT /upload" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected POST,PUT /upload endpoint, got %v", ents)
	}
}

func TestDrogonNoMatch(t *testing.T) {
	src := `#include <drogon/drogon.h>
int main() { return 0; }
`
	ents := extract(t, "custom_cpp_drogon", fi("main.cc", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for plain main, got %d", len(ents))
	}
}

func TestDrogonWrongLanguage(t *testing.T) {
	src := `ADD_METHOD_TO(Ctrl::fn, "/path", Get);`
	ents := extract(t, "custom_cpp_drogon", fi("routes.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Crow
// ---------------------------------------------------------------------------

func TestCrowRouteBasic(t *testing.T) {
	src := `CROW_ROUTE(app, "/hello")(helloHandler);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /hello") {
		t.Errorf("expected GET /hello endpoint from basic CROW_ROUTE, got %v", ents)
	}
}

func TestCrowRouteWithMethods(t *testing.T) {
	src := `CROW_ROUTE(app, "/users").methods("GET"_method, "POST"_method)(usersHandler);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Name == "GET,POST /users" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected GET,POST /users endpoint, got %v", ents)
	}
}

func TestCrowRoutePostOnly(t *testing.T) {
	src := `CROW_ROUTE(app, "/login").methods("POST"_method)(loginHandler);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /login") {
		t.Errorf("expected POST /login endpoint, got %v", ents)
	}
}

func TestCrowBPRoute(t *testing.T) {
	src := `CROW_BP_ROUTE(bp, "/api/status")(statusHandler);`
	ents := extract(t, "custom_cpp_crow", fi("bp.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/status") {
		t.Errorf("expected GET /api/status endpoint from CROW_BP_ROUTE, got %v", ents)
	}
}

func TestCrowCatchAll(t *testing.T) {
	src := `CROW_CATCHALL_ROUTE(app)(notFoundHandler);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY *") {
		t.Errorf("expected ANY * endpoint from CROW_CATCHALL_ROUTE, got %v", ents)
	}
}

func TestCrowNoMatch(t *testing.T) {
	src := `#include <crow.h>
int main() { app.port(8080).run(); }`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestCrowWrongLanguage(t *testing.T) {
	src := `CROW_ROUTE(app, "/foo")(handler);`
	ents := extract(t, "custom_cpp_crow", fi("main.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Pistache
// ---------------------------------------------------------------------------

func TestPistacheStaticGet(t *testing.T) {
	src := `Routes::Get(router, "/api/users", Routes::bind(&UserHandler::getUsers));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users") {
		t.Errorf("expected GET /api/users endpoint, got %v", ents)
	}
}

func TestPistacheStaticPost(t *testing.T) {
	src := `Routes::Post(router, "/api/users", Routes::bind(&UserHandler::createUser));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "POST /api/users") {
		t.Errorf("expected POST /api/users endpoint, got %v", ents)
	}
}

func TestPistacheStaticDelete(t *testing.T) {
	src := `Routes::Delete(router, "/api/users/:id", Routes::bind(&UserHandler::deleteUser));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	// :id is normalised to the canonical {id} form.
	assertEndpoint(t, ents, "DELETE", "/api/users/{id}", "UserHandler::deleteUser")
}

func TestPistacheStaticAny(t *testing.T) {
	src := `Routes::Any(router, "/health", Routes::bind(&HealthHandler::check));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY /health") {
		t.Errorf("expected ANY /health endpoint, got %v", ents)
	}
}

func TestPistacheInstanceMethod(t *testing.T) {
	src := `router.get("/status", Routes::bind(&StatusHandler::get));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /status") {
		t.Errorf("expected GET /status from instance .get(), got %v", ents)
	}
}

func TestPistacheNoMatch(t *testing.T) {
	src := `#include <pistache/endpoint.h>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_pistache", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestPistacheWrongLanguage(t *testing.T) {
	src := `Routes::Get(router, "/foo", Routes::bind(&H::fn));`
	ents := extract(t, "custom_cpp_pistache", fi("srv.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// cpprestsdk
// ---------------------------------------------------------------------------

func TestCppRestSDKSupportVerb(t *testing.T) {
	src := `
http_listener listener("http://localhost:8080/api");
listener.support(methods::GET, handleGet);
listener.support(methods::POST, handlePost);
`
	ents := extract(t, "custom_cpp_cpprestsdk", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET http://localhost:8080/api") {
		t.Errorf("expected GET endpoint, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST http://localhost:8080/api") {
		t.Errorf("expected POST endpoint, got %v", ents)
	}
}

func TestCppRestSDKSupportAny(t *testing.T) {
	src := `
http_listener listener("http://localhost:9090/");
listener.support(handleAll);
`
	ents := extract(t, "custom_cpp_cpprestsdk", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "ANY http://localhost:9090/") {
		t.Errorf("expected ANY endpoint, got %v", ents)
	}
}

func TestCppRestSDKSupportUnknownPath(t *testing.T) {
	// No listener init in file — path falls back to <varname>
	src := `listener.support(methods::GET, getHandler);`
	ents := extract(t, "custom_cpp_cpprestsdk", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET <listener>") {
		t.Errorf("expected GET <listener> fallback path, got %v", ents)
	}
}

func TestCppRestSDKNoMatch(t *testing.T) {
	src := `#include <cpprest/http_listener.h>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_cpprestsdk", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestCppRestSDKWrongLanguage(t *testing.T) {
	src := `listener.support(methods::GET, handler);`
	ents := extract(t, "custom_cpp_cpprestsdk", fi("srv.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
