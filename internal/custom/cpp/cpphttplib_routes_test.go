package cpp_test

// cpphttplib_routes_test.go — golden fixture tests for cpphttplib_routes.go.

import "testing"

func TestCppHttplibBasicVerbs(t *testing.T) {
	src := `
#include <httplib.h>
int main() {
    httplib::Server svr;
    svr.Get("/hi", [](const httplib::Request&, httplib::Response& res) {
        res.set_content("Hello", "text/plain");
    });
    svr.Post("/users", createUser);
    svr.Put("/users/:id", updateUser);
    svr.Delete("/users/:id", deleteUser);
    svr.listen("0.0.0.0", 8080);
}
`
	ents := extract(t, "custom_cpp_cpphttplib", fi("server.cc", "cpp", src))
	assertEndpoint(t, ents, "GET", "/hi", "<lambda>")
	assertEndpoint(t, ents, "POST", "/users", "createUser")
	assertEndpoint(t, ents, "PUT", "/users/{id}", "updateUser")
	assertEndpoint(t, ents, "DELETE", "/users/{id}", "deleteUser")
}

func TestCppHttplibHandlerAttribution(t *testing.T) {
	src := `
#include <httplib.h>
httplib::Server svr;
void wire() { svr.Get("/health", healthHandler); }
`
	ents := extract(t, "custom_cpp_cpphttplib", fi("wire.cc", "cpp", src))
	assertEndpoint(t, ents, "GET", "/health", "healthHandler")
}

func TestCppHttplibLambdaHandler(t *testing.T) {
	src := `
#include <httplib.h>
httplib::Server svr;
void w() { svr.Get("/x", [&](const httplib::Request&, httplib::Response& r){ r.status = 200; }); }
`
	ents := extract(t, "custom_cpp_cpphttplib", fi("x.cc", "cpp", src))
	assertEndpoint(t, ents, "GET", "/x", "<lambda>")
}

func TestCppHttplibSSLServer(t *testing.T) {
	src := `
#include <httplib.h>
httplib::SSLServer svr("./cert.pem", "./key.pem");
void w() { svr.Patch("/cfg", patchCfg); svr.Options("/cfg", optsCfg); }
`
	ents := extract(t, "custom_cpp_cpphttplib", fi("ssl.cc", "cpp", src))
	assertEndpoint(t, ents, "PATCH", "/cfg", "patchCfg")
	assertEndpoint(t, ents, "OPTIONS", "/cfg", "optsCfg")
}

func TestCppHttplibNoMarkerNoMatch(t *testing.T) {
	// Without an httplib marker, obj.Get("...") must NOT be misattributed.
	src := `
struct Cache { std::string Get(const std::string& k); };
void f(Cache& c) { c.Get("/not-a-route"); }
`
	ents := extract(t, "custom_cpp_cpphttplib", fi("cache.cc", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities without httplib marker, got %d: %v", len(ents), ents)
	}
}

func TestCppHttplibWrongLanguage(t *testing.T) {
	src := `httplib::Server svr; void w() { svr.Get("/x", h); }`
	ents := extract(t, "custom_cpp_cpphttplib", fi("x.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
