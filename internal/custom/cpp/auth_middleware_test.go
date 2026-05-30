package cpp_test

// auth_middleware_test.go — fixture tests for auth_middleware.go.
// Exercises Drogon HttpFilter, Crow CROW_MIDDLEWARES, Pistache Handler chains,
// cpprestsdk auth header extraction, JWT patterns, and the middleware catalog.

import "testing"

// ---------------------------------------------------------------------------
// Auth coverage
// ---------------------------------------------------------------------------

func TestCppAuthJwtCppInclude(t *testing.T) {
	src := `#include <jwt-cpp/jwt.h>
void verify(const std::string& token) {
    auto decoded = jwt::decode(token);
}`
	ents := extract(t, "custom_cpp_auth_middleware", fi("auth.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected auth SCOPE.Pattern for jwt-cpp include, got %v", ents)
	}
}

func TestCppAuthJwtDecode(t *testing.T) {
	src := `auto decoded = jwt::decode(raw_token);`
	ents := extract(t, "custom_cpp_auth_middleware", fi("jwt_auth.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected auth SCOPE.Pattern for jwt::decode, got %v", ents)
	}
}

func TestCppAuthDrogonHttpFilter(t *testing.T) {
	src := `
#include <drogon/HttpFilter.h>
class AuthFilter : public drogon::HttpFilter<AuthFilter> {
public:
    void doFilter(const HttpRequestPtr& req, FilterCallback&& cb, FilterChainCallback&& ccb) override;
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("auth_filter.h", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for Drogon HttpFilter class, got %v", ents)
	}
}

func TestCppAuthCrowMiddlewares(t *testing.T) {
	src := `
#include <crow.h>
struct AuthMiddleware {
    struct context {};
    void before_handle(crow::request& req, crow::response& res, context& ctx) {}
};
int main() {
    crow::App<AuthMiddleware> app;
    CROW_MIDDLEWARES(app, AuthMiddleware);
}
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("server.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for CROW_MIDDLEWARES, got %v", ents)
	}
}

func TestCppAuthBearerHeader(t *testing.T) {
	src := `
auto auth = request.headers().find("Authorization");
if (auth != request.headers().end()) {
    // check Bearer token
    if (auth->second.find("Bearer") != std::string::npos) {
        validate_token(auth->second);
    }
}
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("handler.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for Authorization Bearer header, got %v", ents)
	}
}

func TestCppAuthWrongLanguage(t *testing.T) {
	src := `#include <jwt-cpp/jwt.h>
void verify(const char* token) { jwt_decode(NULL, token, NULL, 0); }`
	ents := extract(t, "custom_cpp_auth_middleware", fi("auth.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

func TestCppAuthNoMatch(t *testing.T) {
	src := `
#include <iostream>
int main() {
    std::cout << "Hello World" << std::endl;
    return 0;
}
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for plain main, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Middleware coverage
// ---------------------------------------------------------------------------

func TestCppMwDrogonDoFilter(t *testing.T) {
	src := `
#include <drogon/HttpFilter.h>
class RateLimitFilter : public drogon::HttpFilter<RateLimitFilter> {
public:
    void doFilter(const HttpRequestPtr& req, FilterCallback&& cb, FilterChainCallback&& ccb) override {
        // rate limiting logic
        ccb();
    }
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("rate_filter.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for Drogon doFilter middleware, got %v", ents)
	}
}

func TestCppMwDrogonRegisterFilter(t *testing.T) {
	src := `
#include <drogon/drogon.h>
int main() {
    app().registerFilter<AuthFilter>();
    app().run();
}
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("main.cc", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for app().registerFilter, got %v", ents)
	}
}

func TestCppMwCrowBeforeHandle(t *testing.T) {
	src := `
struct LoggingMiddleware {
    struct context {};
    void before_handle(crow::request& req, crow::response& res, context& ctx) {
        CROW_LOG_INFO << "Request: " << req.url;
    }
    void after_handle(crow::request& req, crow::response& res, context& ctx) {}
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("mw.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for Crow before_handle, got %v", ents)
	}
}

func TestCppMwCrowAppTemplate(t *testing.T) {
	src := `crow::App<LogMiddleware, AuthMiddleware> app;`
	ents := extract(t, "custom_cpp_auth_middleware", fi("server.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for crow::App<Mw...>, got %v", ents)
	}
}

func TestCppMwPistacheHandler(t *testing.T) {
	src := `
#include <pistache/endpoint.h>
class MyHandler : public Pistache::Http::Handler {
public:
    HTTP_PROTOTYPE(MyHandler)
    void onRequest(const Pistache::Http::Request& req, Pistache::Http::ResponseWriter writer) override;
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("handler.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for Pistache Handler, got %v", ents)
	}
}
