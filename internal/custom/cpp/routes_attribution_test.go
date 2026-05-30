package cpp_test

// routes_attribution_test.go — value-asserting routing tests for the eight
// C/C++ HTTP frameworks. Unlike the containsEntity smoke tests, every case here
// proves the exact (http_method, route_path, handler_name) triple AND that
// framework-specific path-param syntaxes normalise to the canonical `{id}`
// form via cppNormalizeRoutePath. This is the evidence backing the
// route_extraction / handler_attribution / endpoint_synthesis = full flips.

import "testing"

// ---------------------------------------------------------------------------
// Path-param normalisation (cppNormalizeRoutePath) — per framework
// ---------------------------------------------------------------------------

func TestCrowAngleParamNormalised(t *testing.T) {
	// Crow typed-unnamed param <int> normalises to {int}.
	src := `CROW_ROUTE(app, "/users/<int>")(getUser);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/users/{int}", "getUser")
}

func TestCrowAngleParamNamedNormalised(t *testing.T) {
	// Crow typed-named param <int:id> keeps the name → {id}.
	src := `CROW_ROUTE(app, "/users/<int:id>").methods("POST"_method)(setUser);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	assertEndpoint(t, ents, "POST", "/users/{id}", "setUser")
}

func TestCrowStringParamNormalised(t *testing.T) {
	src := `CROW_ROUTE(app, "/files/<string>/<path>")(getFile);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/files/{string}/{path}", "getFile")
}

func TestPistacheColonParamNormalised(t *testing.T) {
	src := `Routes::Get(router, "/api/users/:id", Routes::bind(&UserHandler::getUser));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/api/users/{id}", "UserHandler::getUser")
}

func TestPistacheInstanceColonParamNormalised(t *testing.T) {
	src := `router.put("/orders/:orderId/items/:itemId", Routes::bind(&OrderHandler::patch));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "PUT", "/orders/{orderId}/items/{itemId}", "OrderHandler::patch")
}

func TestRestinioColonParamNormalised(t *testing.T) {
	src := `router->http_get("/api/users/:id", getUserHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/api/users/{id}", "getUserHandler")
}

func TestRestinioBraceParamPreserved(t *testing.T) {
	src := `router->http_delete("/api/items/{id}", deleteItemHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "DELETE", "/api/items/{id}", "deleteItemHandler")
}

func TestDrogonBraceParamPreserved(t *testing.T) {
	src := `ADD_METHOD_TO(UserCtrl::getUser, "/api/users/{id}", Get);`
	ents := extract(t, "custom_cpp_drogon", fi("ctrl.cc", "cpp", src))
	assertEndpoint(t, ents, "GET", "/api/users/{id}", "UserCtrl::getUser")
}

func TestOatppBraceParamPreserved(t *testing.T) {
	src := `ENDPOINT_ASYNC("PUT", "/api/items/{id}", UpdateItemHandler)`
	ents := extract(t, "custom_cpp_oatpp", fi("controller.cpp", "cpp", src))
	assertEndpoint(t, ents, "PUT", "/api/items/{id}", "UpdateItemHandler")
}

func TestPocoParamNormalised(t *testing.T) {
	// POCO router.add with a colon param normalises in the route_path prop.
	src := `router.add(HTTPRequest::HTTP_GET, "/api/items/:id", new ItemFactory());`
	ents := extract(t, "custom_cpp_poco", fi("router.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/api/items/{id}", "<factory>")
}

func TestRestbedParamNormalised(t *testing.T) {
	src := `
resource->set_path("/api/users/:id");
resource->set_method_handler("GET", getUserHandler);
`
	ents := extract(t, "custom_cpp_restbed", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/api/users/{id}", "getUserHandler")
}

// ---------------------------------------------------------------------------
// Handler attribution — exact handler_name proof (no path params)
// ---------------------------------------------------------------------------

func TestCrowHandlerAttribution(t *testing.T) {
	src := `CROW_ROUTE(app, "/login").methods("POST"_method)(loginHandler);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	assertEndpoint(t, ents, "POST", "/login", "loginHandler")
}

func TestDrogonRegisterHandlerAttribution(t *testing.T) {
	src := `app().registerHandler("/health", healthHandler, {Get});`
	ents := extract(t, "custom_cpp_drogon", fi("app.cc", "cpp", src))
	assertEndpoint(t, ents, "GET", "/health", "healthHandler")
}

func TestPistacheBindHandlerAttribution(t *testing.T) {
	// Routes::bind(&Handler::method) → handler_name "UserHandler::getUsers".
	src := `Routes::Get(router, "/api/users", Routes::bind(&UserHandler::getUsers));`
	ents := extract(t, "custom_cpp_pistache", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/api/users", "UserHandler::getUsers")
}

func TestOatppHandlerAttribution(t *testing.T) {
	src := `ENDPOINT("GET", "/api/users", getUsers)`
	ents := extract(t, "custom_cpp_oatpp", fi("controller.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "/api/users", "getUsers")
}

func TestRestinioHandlerAttribution(t *testing.T) {
	src := `router->http_post("/api/users", createUserHandler);`
	ents := extract(t, "custom_cpp_restinio", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "POST", "/api/users", "createUserHandler")
}

func TestRestbedHandlerAttribution(t *testing.T) {
	src := `
resource->set_path("/api/orders");
resource->set_method_handler("POST", createOrderHandler);
`
	ents := extract(t, "custom_cpp_restbed", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "POST", "/api/orders", "createOrderHandler")
}

func TestPocoHandlerAttribution(t *testing.T) {
	src := `srv.addHandler<UserRequestHandler>("/api/users");`
	ents := extract(t, "custom_cpp_poco", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "ANY", "/api/users", "UserRequestHandler")
}

func TestCppRestSDKHandlerAttribution(t *testing.T) {
	src := `
http_listener listener("http://localhost:8080/api");
listener.support(methods::GET, handleGet);
`
	ents := extract(t, "custom_cpp_cpprestsdk", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "http://localhost:8080/api", "handleGet")
}

// ---------------------------------------------------------------------------
// Multi-verb endpoint synthesis — exact composite name + per-prop check
// ---------------------------------------------------------------------------

func TestDrogonMultiVerbSynthesis(t *testing.T) {
	src := `ADD_METHOD_TO(MyCtrl::handler, "/api/items/{id}", Get, Put, Delete);`
	ents := extract(t, "custom_cpp_drogon", fi("ctrl.cc", "cpp", src))
	ep := findEndpoint(ents, "GET,PUT,DELETE /api/items/{id}")
	if ep == nil {
		t.Fatalf("expected GET,PUT,DELETE /api/items/{id}, got %v", ents)
	}
	if ep.Props["route_path"] != "/api/items/{id}" {
		t.Errorf("route_path = %q, want /api/items/{id}", ep.Props["route_path"])
	}
	if ep.Props["handler_name"] != "MyCtrl::handler" {
		t.Errorf("handler_name = %q, want MyCtrl::handler", ep.Props["handler_name"])
	}
}

func TestCrowMultiVerbSynthesis(t *testing.T) {
	src := `CROW_ROUTE(app, "/users").methods("GET"_method, "POST"_method)(usersHandler);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	ep := findEndpoint(ents, "GET,POST /users")
	if ep == nil {
		t.Fatalf("expected GET,POST /users, got %v", ents)
	}
	if ep.Props["handler_name"] != "usersHandler" {
		t.Errorf("handler_name = %q, want usersHandler", ep.Props["handler_name"])
	}
}

// ---------------------------------------------------------------------------
// Normalisation idempotency + sentinel pass-through
// ---------------------------------------------------------------------------

func TestCppRestSDKSentinelPathPreserved(t *testing.T) {
	// No listener init → "<listener>" sentinel must NOT be mangled by the
	// angle-param normaliser (it names a variable, not a route param).
	src := `listener.support(methods::GET, getHandler);`
	ents := extract(t, "custom_cpp_cpprestsdk", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "GET", "<listener>", "getHandler")
}

func TestRestbedSentinelPathPreserved(t *testing.T) {
	src := `resource->set_method_handler("DELETE", deleteHandler);`
	ents := extract(t, "custom_cpp_restbed", fi("server.cpp", "cpp", src))
	assertEndpoint(t, ents, "DELETE", "<resource>", "deleteHandler")
}

func TestCrowCatchAllGlobPreserved(t *testing.T) {
	// The "*" catch-all glob must survive normalisation unchanged.
	src := `CROW_CATCHALL_ROUTE(app)(notFoundHandler);`
	ents := extract(t, "custom_cpp_crow", fi("main.cpp", "cpp", src))
	assertEndpoint(t, ents, "ANY", "*", "notFoundHandler")
}
