package cpp_test

// auth_middleware_test.go — VALUE-ASSERTING fixture tests for auth_middleware.go.
//
// These prove the TS/JS bar: each test asserts the *specific* captured symbol
// name, auth method, middleware kind, and (where applicable) order — not
// len>0. Covers Drogon filters, oatpp Authorization handlers + interceptors,
// Crow middleware structs/templates, jwt-cpp call sites, and the generic
// bearer/api_key/session surface.

import "testing"

// authEntity returns the SCOPE.Pattern entity with the given exact Name, or nil.
func authEntity(ents []entitySummary, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Pattern" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// assertProp fails unless the entity named `name` exists and its `prop` equals
// `want`.
func assertProp(t *testing.T, ents []entitySummary, name, prop, want string) {
	t.Helper()
	e := authEntity(ents, name)
	if e == nil {
		t.Fatalf("expected entity %q, got %v", name, ents)
	}
	if got := e.Props[prop]; got != want {
		t.Errorf("entity %q: %s = %q, want %q", name, prop, got, want)
	}
}

// ---------------------------------------------------------------------------
// Drogon — auth filters
// ---------------------------------------------------------------------------

func TestCppAuthDrogonJwtFilter(t *testing.T) {
	src := `
#include <drogon/HttpFilter.h>
class JwtAuthFilter : public drogon::HttpFilter<JwtAuthFilter> {
public:
    void doFilter(const HttpRequestPtr& req, FilterCallback&& cb, FilterChainCallback&& ccb) override;
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("auth_filter.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:JwtAuthFilter", "auth_symbol", "JwtAuthFilter")
	assertProp(t, ents, "auth:drogon_filter:JwtAuthFilter", "auth_method", "jwt")
	assertProp(t, ents, "auth:drogon_filter:JwtAuthFilter", "framework", "drogon")
	// doFilter middleware also captured.
	assertProp(t, ents, "middleware:drogon:doFilter", "middleware_kind", "doFilter")
}

func TestCppAuthDrogonBasicFilter(t *testing.T) {
	src := `class BasicAuthFilter : public drogon::HttpFilter<BasicAuthFilter> {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("basic.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:BasicAuthFilter", "auth_method", "basic")
}

func TestCppMwDrogonRegisterFilter(t *testing.T) {
	src := `
#include <drogon/drogon.h>
int main() {
    app().registerFilter<JwtAuthFilter>();
    app().run();
}
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("main.cc", "cpp", src))
	assertProp(t, ents, "middleware:drogon:registerFilter:JwtAuthFilter", "middleware_symbol", "JwtAuthFilter")
	assertProp(t, ents, "middleware:drogon:registerFilter:JwtAuthFilter", "middleware_kind", "registerFilter")
	// register of an auth-named filter cross-emits an auth entity.
	assertProp(t, ents, "auth:drogon_filter:JwtAuthFilter", "auth_method", "jwt")
}

func TestCppMwDrogonFilterAdd(t *testing.T) {
	src := `
class UserController : public drogon::HttpController<UserController> {
public:
    METHOD_LIST_BEGIN
    FILTER_ADD(BearerAuthFilter);
    METHOD_LIST_END
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("ctrl.h", "cpp", src))
	assertProp(t, ents, "middleware:drogon:FILTER_ADD:BearerAuthFilter", "middleware_kind", "FILTER_ADD")
	assertProp(t, ents, "auth:drogon_filter:BearerAuthFilter", "auth_method", "bearer")
}

// ---------------------------------------------------------------------------
// oatpp — authorization handlers + interceptors
// ---------------------------------------------------------------------------

func TestCppAuthOatppBearerHandler(t *testing.T) {
	src := `
class MyBearerAuth : public oatpp::web::server::handler::BearerAuthorizationHandler {
public:
    MyBearerAuth() : BearerAuthorizationHandler("my-realm") {}
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("auth.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:MyBearerAuth", "auth_method", "bearer")
	assertProp(t, ents, "auth:oatpp_authorization_handler:MyBearerAuth", "auth_symbol", "MyBearerAuth")
	assertProp(t, ents, "auth:oatpp_authorization_handler:MyBearerAuth", "framework", "oatpp")
}

func TestCppAuthOatppBasicHandler(t *testing.T) {
	src := `class MyBasicAuth : public oatpp::web::server::handler::BasicAuthorizationHandler {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("basic.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:MyBasicAuth", "auth_method", "basic")
}

func TestCppMwOatppRequestInterceptor(t *testing.T) {
	src := `
#include <oatpp/web/server/interceptor/RequestInterceptor.hpp>
class AuthInterceptor : public oatpp::web::server::interceptor::RequestInterceptor {
public:
    std::shared_ptr<OutgoingResponse> intercept(const std::shared_ptr<IncomingRequest>& req) override;
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("interceptor.hpp", "cpp", src))
	assertProp(t, ents, "middleware:oatpp_interceptor:AuthInterceptor", "middleware_kind", "interceptor")
	assertProp(t, ents, "middleware:oatpp_interceptor:AuthInterceptor", "interceptor_phase", "request")
	// auth-named interceptor cross-emits auth.
	assertProp(t, ents, "auth:oatpp_interceptor:AuthInterceptor", "auth_method", "auth")
}

func TestCppMwOatppResponseInterceptor(t *testing.T) {
	src := `class HeaderInterceptor : public oatpp::web::server::interceptor::ResponseInterceptor {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("resp.hpp", "cpp", src))
	assertProp(t, ents, "middleware:oatpp_interceptor:HeaderInterceptor", "interceptor_phase", "response")
}

// ---------------------------------------------------------------------------
// Crow — middleware structs + ordered templates
// ---------------------------------------------------------------------------

func TestCppMwCrowAppTemplateOrder(t *testing.T) {
	src := `crow::App<LogMiddleware, AuthMiddleware> app;`
	ents := extract(t, "custom_cpp_auth_middleware", fi("server.cpp", "cpp", src))
	// Order is captured: Log first (0), Auth second (1).
	assertProp(t, ents, "middleware:crow_app:LogMiddleware", "middleware_order", "0")
	assertProp(t, ents, "middleware:crow_app:AuthMiddleware", "middleware_order", "1")
	assertProp(t, ents, "middleware:crow_app:AuthMiddleware", "middleware_kind", "crow_app_template")
	// AuthMiddleware cross-emits an auth entity.
	assertProp(t, ents, "auth:crow_middleware:AuthMiddleware", "auth_method", "auth")
}

func TestCppMwCrowMiddlewaresMacroOrder(t *testing.T) {
	src := `CROW_MIDDLEWARES(app, CORSMiddleware, JwtMiddleware);`
	ents := extract(t, "custom_cpp_auth_middleware", fi("main.cpp", "cpp", src))
	assertProp(t, ents, "middleware:crow_middlewares:CORSMiddleware", "middleware_order", "0")
	assertProp(t, ents, "middleware:crow_middlewares:JwtMiddleware", "middleware_order", "1")
	assertProp(t, ents, "auth:crow_middleware:JwtMiddleware", "auth_method", "jwt")
}

func TestCppMwCrowBeforeHandleStruct(t *testing.T) {
	src := `
struct LoggingMiddleware {
    struct context {};
    void before_handle(crow::request& req, crow::response& res, context& ctx) {}
    void after_handle(crow::request& req, crow::response& res, context& ctx) {}
};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("mw.cpp", "cpp", src))
	assertProp(t, ents, "middleware:crow_struct:LoggingMiddleware", "middleware_symbol", "LoggingMiddleware")
	assertProp(t, ents, "middleware:crow:before_handle", "middleware_kind", "before_handle")
	assertProp(t, ents, "middleware:crow:after_handle", "middleware_kind", "after_handle")
}

// ---------------------------------------------------------------------------
// JWT call sites + generic bearer/api_key/session
// ---------------------------------------------------------------------------

func TestCppAuthJwtCppVerify(t *testing.T) {
	src := `auto decoded = jwt::decode(raw_token); jwt::verify().verify(decoded);`
	ents := extract(t, "custom_cpp_auth_middleware", fi("jwt.cpp", "cpp", src))
	assertProp(t, ents, "auth:jwt:jwt::decode", "auth_method", "jwt")
	assertProp(t, ents, "auth:jwt:jwt::verify", "auth_method", "jwt")
}

func TestCppAuthBearerHeader(t *testing.T) {
	src := `auto auth = req.getHeader("Authorization"); if (auth.find("Bearer") == 0) {}`
	ents := extract(t, "custom_cpp_auth_middleware", fi("handler.cpp", "cpp", src))
	assertProp(t, ents, "auth:bearer:authorization_header", "auth_method", "bearer")
}

func TestCppAuthApiKey(t *testing.T) {
	src := `auto key = req.getHeader("X-Api-Key");`
	ents := extract(t, "custom_cpp_auth_middleware", fi("apikey.cpp", "cpp", src))
	assertProp(t, ents, "auth:api_key:header", "auth_method", "api_key")
}

func TestCppAuthSession(t *testing.T) {
	src := `auto sid = req.cookies().get("session_id"); SessionManager::validate(sid);`
	ents := extract(t, "custom_cpp_auth_middleware", fi("session.cpp", "cpp", src))
	if e := authEntity(ents, "auth:session:session_id"); e != nil {
		assertProp(t, ents, "auth:session:session_id", "auth_method", "session")
	} else {
		assertProp(t, ents, "auth:session:SessionManager", "auth_method", "session")
	}
}

// ---------------------------------------------------------------------------
// cppClassifyAuthMethod — arms no fixture selected (#7270)
//
// Each of these drives the classifier through a Drogon symbol name and asserts
// the auth_method that arm produces. Every name is chosen so that removing the
// arm under test changes what the fixture sees. That constrains the gate as
// well as the name: where the HttpFilter class-declaration path could not
// offer that, the fixture reaches the classifier through a different gate —
// see the fallback-arm test at the end of this block.
// ---------------------------------------------------------------------------

// api_key: the arm matches two spellings ("apikey" and "api_key"), so both are
// exercised — a fixture using only one would leave the other half ungraded.
func TestCppAuthDrogonApiKeyFilter(t *testing.T) {
	src := `
class ApiKeyFilter : public drogon::HttpFilter<ApiKeyFilter> {};
class Api_KeyHeaderFilter : public drogon::HttpFilter<Api_KeyHeaderFilter> {};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("apikey_filter.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:ApiKeyFilter", "auth_method", "api_key")
	assertProp(t, ents, "auth:drogon_filter:Api_KeyHeaderFilter", "auth_method", "api_key")
}

// session: likewise a two-spelling arm ("session" and "cookie").
func TestCppAuthDrogonSessionFilter(t *testing.T) {
	src := `
class SessionFilter : public drogon::HttpFilter<SessionFilter> {};
class CookieCheckFilter : public drogon::HttpFilter<CookieCheckFilter> {};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("session_filter.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:SessionFilter", "auth_method", "session")
	assertProp(t, ents, "auth:drogon_filter:CookieCheckFilter", "auth_method", "session")
}

// oauth: the name also contains "auth", so this additionally pins that the
// oauth arm is reached BEFORE the generic "auth" arm — without that ordering
// the value would be "auth".
func TestCppAuthDrogonOAuthFilter(t *testing.T) {
	src := `class OAuthCallbackFilter : public drogon::HttpFilter<OAuthCallbackFilter> {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("oauth_filter.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:OAuthCallbackFilter", "auth_method", "oauth")
}

// token: an arm whose return value is not its own label — a "token"-named
// symbol is classified as "bearer". The fixture name contains no "bearer", so
// nothing but this arm can stamp that value on it.
func TestCppAuthDrogonTokenFilter(t *testing.T) {
	src := `class TokenValidationFilter : public drogon::HttpFilter<TokenValidationFilter> {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("token_filter.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:TokenValidationFilter", "auth_method", "bearer")
}

// bearer: also exercised by TestCppMwDrogonFilterAdd, whose subject is the
// FILTER_ADD binding rather than the classifier. Anchored here directly so the
// arm does not rest on the class name an unrelated fixture happens to use.
func TestCppAuthDrogonBearerFilter(t *testing.T) {
	src := `class BearerCredentialFilter : public drogon::HttpFilter<BearerCredentialFilter> {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("bearer_filter.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:BearerCredentialFilter", "auth_method", "bearer")
}

// auth (the fallback arm), anchored at the registerFilter<X> gate rather than
// through an HttpFilter class declaration: that gate emits no auth entity at
// all when the name classifies to "", so removing the arm removes the entity
// this test demands — which is what makes this anchor non-vacuous.
// LoginAuthFilter's only auth signal is the substring "auth", so no earlier
// arm claims it.
func TestCppAuthDrogonRegisterFilterAuthFallback(t *testing.T) {
	src := `
#include <drogon/drogon.h>
int main() {
    app().registerFilter<LoginAuthFilter>();
    app().run();
}
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("register_auth.cc", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:LoginAuthFilter", "auth_method", "auth")
}

// emitAuth's unclassified default (#7295), on the HttpFilter class-declaration
// path — a separate mechanism from the classifier fallback arm above, which it
// happens to agree with on the value "auth". Because the two agree, a fixture
// whose name contains "auth" cannot say which of them produced the stamped
// value; this one carries a name the classifier does not recognise, so it
// reaches emitAuth with method == "". Kept as its own test on its own fixture
// for that reason: folding it into the test above would grade neither.
func TestCppAuthDrogonClassFilterUnclassifiedDefault(t *testing.T) {
	// Drift guard: "PlainFilter" must carry no signal the classifier knows.
	// The registerFilter<X> gate emits an auth entity only when
	// cppClassifyAuthMethod(X) != "", so the absence of that entity here is
	// an assertion about the classifier's return for this exact name. If a
	// future edit adds an arm this name matches, this half fails rather than
	// letting the half below silently stop exercising the substitution.
	guard := extract(t, "custom_cpp_auth_middleware", fi("register_plain.cc", "cpp", `
#include <drogon/drogon.h>
int main() {
    app().registerFilter<PlainFilter>();
    app().run();
}
`))
	// The absence below is only evidence if the registration was recognised at
	// all: assert the middleware entity that gate always emits first, so a dead
	// registerFilter recogniser fails here instead of passing the guard.
	if e := authEntity(guard, "middleware:drogon:registerFilter:PlainFilter"); e == nil {
		t.Fatalf("registerFilter<PlainFilter> was not recognised, so the guard below proves nothing; got %v", guard)
	}
	if e := authEntity(guard, "auth:drogon_filter:PlainFilter"); e != nil {
		t.Fatalf("PlainFilter is no longer unclassified: registerFilter emitted %+v", *e)
	}

	// The class-declaration path emits unconditionally, so this input reaches
	// emitAuth with method == "". The file carries no jwt call site, so
	// fileHasJWT is false and the substitution takes its "auth" arm — the
	// other arm is graded by the sibling test below.
	src := `class PlainFilter : public drogon::HttpFilter<PlainFilter> {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("plain_filter.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:PlainFilter", "auth_method", "auth")
	assertProp(t, ents, "auth:drogon_filter:PlainFilter", "auth_subtype", "auth")
}

// The jwt arm of the same substitution: an unclassified name in a file that
// DOES carry a jwt-cpp call site. Same input as the test above but for the
// added `jwt::verify`, so the two rows differ on fileHasJWT alone and each
// arm is graded by its own fixture.
//
// This fixture also carries the package's only auth_subtype assertion whose
// expected value differs from "auth", so stamping auth_subtype from anything
// other than the same `method` variable auth_method comes from is visible here.
func TestCppAuthDrogonClassFilterUnclassifiedJwtFile(t *testing.T) {
	src := `
#include <jwt-cpp/jwt.h>
class PlainFilter : public drogon::HttpFilter<PlainFilter> {};
void check(const std::string& tok) { jwt::verify(tok); }
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("plain_filter_jwt.h", "cpp", src))
	assertProp(t, ents, "auth:drogon_filter:PlainFilter", "auth_method", "jwt")
	assertProp(t, ents, "auth:drogon_filter:PlainFilter", "auth_subtype", "jwt")
}

// ---------------------------------------------------------------------------
// Negative cases
// ---------------------------------------------------------------------------

func TestCppAuthWrongLanguage(t *testing.T) {
	src := `class AuthFilter : public drogon::HttpFilter<AuthFilter> {};`
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
