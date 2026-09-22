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

// ---------------------------------------------------------------------------
// oatpp AuthorizationHandler — the two producers of `method` at that call site
// (#7303). The handler loop does not reach emitAuth the way the drogon class
// path does: it first converts the macro's Bearer/Basic flavour capture with
// strings.ToLower, and only falls back to cppClassifyAuthMethod(name) when
// that capture is empty. Both producers can yield the same string, so the two
// tests below are a PAIR, each built so that exactly one producer can have
// stamped its expected value:
//
//   - the flavourless test asserts "session", which strings.ToLower of an
//     empty flavour cannot produce and emitAuth's own unclassified default
//     (graded on the drogon path by #7295) cannot produce either;
//   - the Bearer-flavoured test asserts "bearer" for a name the classifier
//     returns "" for, so only the flavour conversion can have produced it.
//
// Folding them into one fixture would grade neither: the existing
// TestCppAuthOatppBearerHandler above names its class MyBearerAuth, which both
// producers map to "bearer", so it cannot attribute the value to either.
// ---------------------------------------------------------------------------

// Flavourless AuthorizationHandler whose name the classifier DOES recognise.
// This is the only shape that reaches the `if method == ""` fallback at this
// call site with the classifier able to return a distinguishing value:
// deleting that fallback leaves method == "" and emitAuth substitutes "auth",
// so "session" here is an assertion about this call site's own fallback and
// not about the shared substitution.
//
// emitAuth stamps auth_subtype and auth_method from the same variable, so the
// two assertions below are one observation of the stamped method, not two.
func TestCppAuthOatppFlavourlessHandlerClassifiedByName(t *testing.T) {
	src := `class SessionGuard : public oatpp::web::server::handler::AuthorizationHandler {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("flavourless.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:SessionGuard", "auth_method", "session")
	assertProp(t, ents, "auth:oatpp_authorization_handler:SessionGuard", "auth_subtype", "session")
}

// Bearer-flavoured handler whose NAME carries no auth signal, so "bearer" can
// only have come from strings.ToLower of the flavour capture. The guard below
// pins that premise: the interceptor path emits an auth entity only when
// cppClassifyAuthMethod(name) != "", so the absence of one for this exact name
// is an assertion that the classifier returns "" for it. Without the guard a
// future classifier arm matching "gatekeeper" would silently turn this test
// back into the un-attributable shape TestCppAuthOatppBearerHandler already has.
func TestCppAuthOatppBearerFlavourUnclassifiedName(t *testing.T) {
	guard := extract(t, "custom_cpp_auth_middleware", fi("gatekeeper_interceptor.hpp", "cpp", `
#include <oatpp/web/server/interceptor/RequestInterceptor.hpp>
class GateKeeper : public oatpp::web::server::interceptor::RequestInterceptor {};
`))
	// The absence below is only evidence if the interceptor was recognised at
	// all: assert the middleware entity that path always emits first, so a dead
	// recogniser fails here instead of passing the guard vacuously.
	if e := authEntity(guard, "middleware:oatpp_interceptor:GateKeeper"); e == nil {
		t.Fatalf("GateKeeper interceptor was not recognised, so the guard below proves nothing; got %v", guard)
	}
	if e := authEntity(guard, "auth:oatpp_interceptor:GateKeeper"); e != nil {
		t.Fatalf("GateKeeper is no longer unclassified: the interceptor path emitted %+v", *e)
	}

	src := `class GateKeeper : public oatpp::web::server::handler::BearerAuthorizationHandler {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("bearer_unclassified.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:GateKeeper", "auth_method", "bearer")
	assertProp(t, ents, "auth:oatpp_authorization_handler:GateKeeper", "auth_symbol", "GateKeeper")
}

// The Basic end of the same pattern. The flavour capture has two non-empty
// values and scoring one says nothing about the other: a change that consulted
// the capture only for "Bearer" would still pass the test above AND
// TestCppAuthOatppBasicHandler, whose MyBasicAuth name the classifier maps to
// "basic" on its own. As with GateKeeper, the guard pins that the classifier
// returns "" for this name, so "basic" can only have come from the capture.
func TestCppAuthOatppBasicFlavourUnclassifiedName(t *testing.T) {
	guard := extract(t, "custom_cpp_auth_middleware", fi("doorman_interceptor.hpp", "cpp", `
#include <oatpp/web/server/interceptor/RequestInterceptor.hpp>
class DoorMan : public oatpp::web::server::interceptor::RequestInterceptor {};
`))
	if e := authEntity(guard, "middleware:oatpp_interceptor:DoorMan"); e == nil {
		t.Fatalf("DoorMan interceptor was not recognised, so the guard below proves nothing; got %v", guard)
	}
	if e := authEntity(guard, "auth:oatpp_interceptor:DoorMan"); e != nil {
		t.Fatalf("DoorMan is no longer unclassified: the interceptor path emitted %+v", *e)
	}

	src := `class DoorMan : public oatpp::web::server::handler::BasicAuthorizationHandler {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("basic_unclassified.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:DoorMan", "auth_method", "basic")
	assertProp(t, ents, "auth:oatpp_authorization_handler:DoorMan", "auth_symbol", "DoorMan")
}

// The PRECEDENCE between the two producers, which the three fixtures above
// cannot see. Each of them is built so that exactly one producer can produce
// the asserted value — that is what makes them attribute it — and that same
// property makes their ORDER invisible: a handler only one producer speaks for
// stamps the same string whichever is consulted first. This fixture is the one
// shape where both speak and disagree, so only its assertion depends on the
// order. Inverting the two (classifier first, flavour as the fallback) leaves
// both producers present and consulted, so no producer-deletion mutant reaches
// it either.
//
// Under inversion this input stamps "jwt" instead of "bearer" — a wrong value,
// not a missing entity. The shape is idiomatic oatpp: a Bearer handler named
// for the token format it carries. The file has no jwt-cpp or libjwt CALL, so
// fileHasJWT is false here and "jwt" could only come from the classifier
// reading the name.
//
// The guard pins the premise in the positive direction: if the classifier ever
// stopped returning "jwt" for this name, only one producer would speak for the
// input and the assertion below would keep passing while grading nothing.
func TestCppAuthOatppFlavourWinsOverClassifiedName(t *testing.T) {
	guard := extract(t, "custom_cpp_auth_middleware", fi("jwtguard_interceptor.hpp", "cpp", `
#include <oatpp/web/server/interceptor/RequestInterceptor.hpp>
class JwtGuard : public oatpp::web::server::interceptor::RequestInterceptor {};
`))
	assertProp(t, guard, "auth:oatpp_interceptor:JwtGuard", "auth_method", "jwt")

	src := `class JwtGuard : public oatpp::web::server::handler::BearerAuthorizationHandler {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("jwt_named_bearer.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:JwtGuard", "auth_method", "bearer")
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
// oatpp base-class NAMESPACE QUALIFIER (#7321)
//
// Both oatpp recognisers admit an optional qualifier before the base class —
// `(?:[\w:]*::)?` — and every fixture above spells the base class with the
// full `oatpp::web::server::…::` path, so the group is held constant across
// all of them. Two of its values were graded by nothing:
//
//   - ABSENT entirely, legal after `using namespace oatpp::web::server::handler;`
//     or a `using`-declaration of the class itself;
//   - a SINGLE segment, legal after `using namespace oatpp::web::server;`.
//
// The four fixtures below vary only that group and hold everything else
// constant against the fixtures above: same flavour capture, same phase, same
// three assertions each. Narrowing the qualifier is a NO-EDGE failure — the
// class is simply not recognised and no entity is emitted — so what each of
// these really grades is the entity's EXISTENCE. The two handler names are
// deliberately value-COINCIDENT (flavour capture and name classifier both
// yield "basic"): this axis is recognition, not value attribution, and the
// coincidence keeps these fixtures out of the kill sets of #7303's producer
// mutants, which grade attribution. The price of that choice, stated plainly:
// the `auth_method` assertion on the handler pair is graded by nothing — it
// survives both #7303 mutants precisely because the two producers agree. It
// is kept so a future mis-stamp fails here loudly, not because it is pinned.
//
// The `using` lines are DECORATION. The extractor never reads them and
// performs no name resolution whatsoever; each recogniser is a single regex
// over the file text. They are here to document why the bare spelling is
// legal C++, and they grade nothing about how the bare spelling is matched.
//
// Deliberately NOT covered, each verified by probe rather than assumed:
//
//   - A leading global-scope qualifier (`: public ::BearerAuthorizationHandler`)
//     MATCHES today, because `[\w:]*` may match empty. Legal C++, but not a
//     spelling observed in oatpp code, and no corpus was checked.
//
//   - BASE-CLAUSE ORDERING, a different axis and likelier in real code than
//     anything else on this list. The oatpp base must be the FIRST
//     base-specifier: `[\w:]*` cannot cross a comma, so
//     `class G : public oatpp::base::Countable, public handler::BearerAuthorizationHandler {};`
//     is NO-MATCH at both recognisers, while the same two bases in the other
//     order MATCH. `oatpp::base::Countable` is a real oatpp base that
//     application types routinely inherit. Being filed as a follow-up.
//
//   - ACCESS SPECIFIER — a neighbouring axis, now PARTLY graded. The rule is
//     not "only `public` inheritance is recognised": it is that a WRITTEN
//     specifier must be exactly `public`, with nothing between it and the
//     qualifier. Probed end to end at both recognisers: no specifier at all
//     MATCHES (and that is *private* inheritance in C++); `private` and
//     `protected` spelled out are NO-MATCH; `public virtual` and
//     `virtual public` are both NO-MATCH.
//
//     The omitted-specifier half is graded by the two rows further down, added
//     once a mandatory-`public` mutant was found ALIVE. The four NO-MATCH
//     spellings are deliberately NOT fixtured: a row asserting their absence
//     is a forbidden row, and no mutant can detect a vacuous one — it would
//     need a planted violation proving the row fires. They stay on this list
//     with the measurement instead.
//
//     The same optional-`public` group is on FOUR recognisers in this file:
//     these two, plus drogon `HttpFilter` and pistache `Handler`. Those two
//     are unscored and untouched here; the axis exists at four sites and is
//     graded at two.
// ---------------------------------------------------------------------------

// Handler base class with NO qualifier at all.
func TestCppAuthOatppUnqualifiedHandlerBaseClass(t *testing.T) {
	src := `
using namespace oatpp::web::server::handler;
class PlainBasicGuard : public BasicAuthorizationHandler {};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("unqualified_handler.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:PlainBasicGuard", "auth_symbol", "PlainBasicGuard")
	assertProp(t, ents, "auth:oatpp_authorization_handler:PlainBasicGuard", "auth_method", "basic")
	assertProp(t, ents, "auth:oatpp_authorization_handler:PlainBasicGuard", "framework", "oatpp")
}

// Handler base class with a SINGLE-segment qualifier. Not reached by the
// fixture above — an unqualified base class satisfies any predicate that only
// constrains what a PRESENT qualifier may contain.
func TestCppAuthOatppSingleSegmentQualifiedHandlerBaseClass(t *testing.T) {
	src := `
using namespace oatpp::web::server;
class BasicScopedGuard : public handler::BasicAuthorizationHandler {};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("scoped_handler.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:BasicScopedGuard", "auth_symbol", "BasicScopedGuard")
	assertProp(t, ents, "auth:oatpp_authorization_handler:BasicScopedGuard", "auth_method", "basic")
	assertProp(t, ents, "auth:oatpp_authorization_handler:BasicScopedGuard", "framework", "oatpp")
}

// Interceptor base class with NO qualifier at all. Scored separately from the
// handler twin: the two recognisers carry the same optional group but are
// distinct regexes, and a verdict on one says nothing about the other.
func TestCppMwOatppUnqualifiedInterceptorBaseClass(t *testing.T) {
	src := `
using oatpp::web::server::interceptor::RequestInterceptor;
class PlainGate : public RequestInterceptor {};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("unqualified_interceptor.hpp", "cpp", src))
	assertProp(t, ents, "middleware:oatpp_interceptor:PlainGate", "middleware_symbol", "PlainGate")
	assertProp(t, ents, "middleware:oatpp_interceptor:PlainGate", "middleware_kind", "interceptor")
	assertProp(t, ents, "middleware:oatpp_interceptor:PlainGate", "interceptor_phase", "request")
}

// Interceptor base class with a SINGLE-segment qualifier.
func TestCppMwOatppSingleSegmentQualifiedInterceptorBaseClass(t *testing.T) {
	src := `
using namespace oatpp::web::server;
class ScopedGate : public interceptor::RequestInterceptor {};
`
	ents := extract(t, "custom_cpp_auth_middleware", fi("scoped_interceptor.hpp", "cpp", src))
	assertProp(t, ents, "middleware:oatpp_interceptor:ScopedGate", "middleware_symbol", "ScopedGate")
	assertProp(t, ents, "middleware:oatpp_interceptor:ScopedGate", "middleware_kind", "interceptor")
	assertProp(t, ents, "middleware:oatpp_interceptor:ScopedGate", "interceptor_phase", "request")
}

// ---------------------------------------------------------------------------
// oatpp base-class ACCESS SPECIFIER (#7321, second round)
//
// One regex group to the LEFT of the qualifier, and the same shape of hole:
// both recognisers spell the specifier `(?:public\s+)?`, optional, and every
// fixture in the package spelled `public`. Making it MANDATORY at both sites
// was ALIVE.
//
// Each of the two rows below has a pre-existing twin: TestCppAuthOatppBasicHandler
// for the handler, TestCppMwOatppRequestInterceptor for the interceptor. Stated
// precisely, because "identical but for one token" would be false: across every
// group either regex actually grades — access specifier, namespace qualifier,
// and the `(Bearer|Basic|)` / `(Request|Response)` capture — the ONLY difference
// is the `public ` keyword. Each twin pair holds the full oatpp qualifier and
// the same capture value (Basic / Request) constant.
//
// The remaining differences are outside those groups and are named here rather
// than glossed: the class NAME differs (so the interceptor twin also emits an
// auth entity from its auth-ish name, while ImplicitGate does not), the
// interceptor twin has a body and an include while ImplicitGate is a one-liner,
// and the assertion sets differ in one slot. None of those is a group either
// recogniser discriminates on, which is why the pairing is still informative —
// but it is a pair of near-twins, not a one-token differential.
//
// Both rows deliberately carry the FULL oatpp qualifier, not the single-segment
// one, so they grade the specifier axis alone and stay out of the qualifier
// mutants' kill sets.
//
// C++ note, because it is counter-intuitive: omitting the specifier on a
// `class` is PRIVATE inheritance, so these two inputs are semantically the
// private-inheritance case that the spelled-out `private` form — NO-MATCH
// today — would express. The recognisers discriminate on the keyword being
// written, not on the inheritance the C++ actually has.
// ---------------------------------------------------------------------------

// Handler base class with NO access specifier written.
func TestCppAuthOatppImplicitAccessSpecifierHandler(t *testing.T) {
	src := `class ImplicitBasicGuard : oatpp::web::server::handler::BasicAuthorizationHandler {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("implicit_access_handler.hpp", "cpp", src))
	assertProp(t, ents, "auth:oatpp_authorization_handler:ImplicitBasicGuard", "auth_symbol", "ImplicitBasicGuard")
	assertProp(t, ents, "auth:oatpp_authorization_handler:ImplicitBasicGuard", "auth_method", "basic")
	assertProp(t, ents, "auth:oatpp_authorization_handler:ImplicitBasicGuard", "framework", "oatpp")
}

// Interceptor base class with NO access specifier written. Scored separately
// from the handler twin: the group is textually identical at both sites but
// they are distinct regexes, and a verdict on one says nothing about the other.
func TestCppMwOatppImplicitAccessSpecifierInterceptor(t *testing.T) {
	src := `class ImplicitGate : oatpp::web::server::interceptor::RequestInterceptor {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("implicit_access_interceptor.hpp", "cpp", src))
	assertProp(t, ents, "middleware:oatpp_interceptor:ImplicitGate", "middleware_symbol", "ImplicitGate")
	assertProp(t, ents, "middleware:oatpp_interceptor:ImplicitGate", "middleware_kind", "interceptor")
	assertProp(t, ents, "middleware:oatpp_interceptor:ImplicitGate", "interceptor_phase", "request")
}

// CHARACTERISATION ROW — asserts what the extractor does TODAY, not what it
// ought to do. The fixtures above widen the qualifier coverage of a surface
// that has no framework gate at all, and this row keeps that fact visible
// rather than letting the widening bury it.
//
// Neither oatpp recogniser consults the detected framework: detectCPPFramework
// is computed, but both emit sites pass the literal "oatpp". So a base class
// merely NAMED *AuthorizationHandler (or *Interceptor), under any namespace
// whatsoever, in a file with no oatpp reference anywhere, is stamped
// framework=oatpp. Note the detector would not save it either — both
// `AuthorizationHandler` and `RequestInterceptor`/`ResponseInterceptor` are
// themselves oatpp markers in cppFrameworkMarkers, so substituting the
// detected framework for the literal changes nothing at either site. A real
// gate would have to require an oatpp token in the source.
//
// That is a claim about BOTH recognisers, so BOTH are observed: the handler
// row below and its interceptor twin. Scoring one end of a two-ended pattern
// and asserting both is the defect this branch exists to remove.
//
// This behaviour is PRE-EXISTING and is not introduced by the fixtures above.
// If this row ever fails, that is very likely the good news: if it is a
// deliberate framework-gating fix, FLIP this row to assert the new behaviour
// and say so in the commit — do not delete it.
func TestCppAuthOatppFrameworkStampIsUngated(t *testing.T) {
	src := `class MyLibGuard : public mylib::BasicAuthorizationHandler {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("no_oatpp_anywhere.hpp", "cpp", src))
	e := authEntity(ents, "auth:oatpp_authorization_handler:MyLibGuard")
	if e == nil {
		t.Fatalf("mylib::BasicAuthorizationHandler is no longer recognised at all; this row characterises the FRAMEWORK stamp and cannot do so if nothing is emitted. Got %v", ents)
	}
	if got := e.Props["framework"]; got != "oatpp" {
		t.Fatalf("framework = %q, want %q. This row CHARACTERISES today's ungated stamp: a non-oatpp namespace in a file with no oatpp token is still stamped oatpp. If this is a deliberate framework-gating fix, flip this row and say so; do not delete it.", got, "oatpp")
	}
}

// The INTERCEPTOR end of the same characterisation. Identical hole, identical
// shape: the interceptor emit hardcodes "oatpp" exactly as the handler one
// does, and `RequestInterceptor` is itself an oatpp marker, so the detected
// framework would not discriminate either. Gating the handler emit on an
// oatpp token is DEAD against the row above; gating the INTERCEPTOR emit was
// ALIVE with an empty kill set until this row existed.
func TestCppMwOatppFrameworkStampIsUngatedInterceptor(t *testing.T) {
	src := `class MyLibGate : public mylib::RequestInterceptor {};`
	ents := extract(t, "custom_cpp_auth_middleware", fi("no_oatpp_anywhere_interceptor.hpp", "cpp", src))
	e := authEntity(ents, "middleware:oatpp_interceptor:MyLibGate")
	if e == nil {
		t.Fatalf("mylib::RequestInterceptor is no longer recognised at all; this row characterises the FRAMEWORK stamp and cannot do so if nothing is emitted. Got %v", ents)
	}
	if got := e.Props["framework"]; got != "oatpp" {
		t.Fatalf("framework = %q, want %q. This row CHARACTERISES today's ungated stamp: a non-oatpp namespace in a file with no oatpp token is still stamped oatpp. If this is a deliberate framework-gating fix, flip this row and say so; do not delete it.", got, "oatpp")
	}
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
// This fixture's auth_subtype assertion expects "jwt" rather than "auth", so
// stamping auth_subtype from anything other than the same `method` variable
// auth_method comes from is visible here. It is no longer the package's only
// such assertion: the oatpp SessionGuard fixture above expects "session".
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
