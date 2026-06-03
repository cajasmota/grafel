package cpp_test

// endpoint_deprecation_test.go — value-asserting fixtures for the C/C++
// endpoint_deprecation_versioning pass (#4147, child of #3628). Proves the flat
// contract (deprecated / deprecated_since / deprecated_replacement /
// deprecation_source / api_version) fires for the genuine C/C++ deprecation
// idioms across the route-DSL frameworks, and proves the negatives
// (non-deprecated route, versionless route, deprecation away from any route,
// non-C++ language) do NOT stamp.

import "testing"

// findDep returns the first SCOPE.Pattern/deprecation entity matching pred, or nil.
func findDep(ents []entitySummary, pred func(entitySummary) bool) *entitySummary {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Pattern" && ents[i].Subtype == "deprecation" && pred(ents[i]) {
			return &ents[i]
		}
	}
	return nil
}

func anyDep(ents []entitySummary) bool {
	return findDep(ents, func(entitySummary) bool { return true }) != nil
}

// --- 1. Drogon [[deprecated("use /api/v2/users")]] on a versioned route ----

func TestCppDep_DrogonAttributeReplacementAndVersion(t *testing.T) {
	src := `
#include <drogon/drogon.h>
using namespace drogon;
class UserController : public HttpController<UserController> {
public:
    METHOD_LIST_BEGIN
    ADD_METHOD_TO(UserController::listV1, "/api/v1/users", Get);
    METHOD_LIST_END

    // legacy v1 listing
    [[deprecated("use /api/v2/users")]]
    void listV1(const HttpRequestPtr &req, std::function<void(const HttpResponsePtr &)> &&cb);
};`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("user.cc", "cpp", src))
	e := findDep(ents, func(s entitySummary) bool { return s.Props["deprecation_source"] == "[[deprecated]]" })
	if e == nil {
		t.Fatalf("expected [[deprecated]] deprecation entity, got %+v", ents)
	}
	if e.Props["deprecated"] != "true" {
		t.Errorf("deprecated = %q, want true", e.Props["deprecated"])
	}
	if e.Props["deprecated_replacement"] != "/api/v2/users" {
		t.Errorf("deprecated_replacement = %q, want /api/v2/users", e.Props["deprecated_replacement"])
	}
	if e.Props["api_version"] != "1" {
		t.Errorf("api_version = %q, want 1 (the deprecated route's own version)", e.Props["api_version"])
	}
	if e.Props["framework"] != "drogon" {
		t.Errorf("framework = %q, want drogon", e.Props["framework"])
	}
}

// --- 2. Drogon attribute with explicit "since 2.0" message -----------------

func TestCppDep_DrogonAttributeSince(t *testing.T) {
	src := `
#include <drogon/drogon.h>
ADD_METHOD_TO(C::old, "/api/v1/old", Get);
[[deprecated("deprecated since 2.0, prefer /api/v2/new")]]
void old();`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("c.cc", "cpp", src))
	e := findDep(ents, func(s entitySummary) bool { return s.Props["deprecation_source"] == "[[deprecated]]" })
	if e == nil {
		t.Fatalf("expected deprecation entity, got %+v", ents)
	}
	if e.Props["deprecated_since"] != "2.0" {
		t.Errorf("deprecated_since = %q, want 2.0", e.Props["deprecated_since"])
	}
	if e.Props["deprecated_replacement"] != "/api/v2/new" {
		t.Errorf("deprecated_replacement = %q, want /api/v2/new", e.Props["deprecated_replacement"])
	}
}

// --- 3. Crow // DEPRECATED banner comment at a CROW_ROUTE ------------------

func TestCppDep_CrowBannerComment(t *testing.T) {
	src := `
#include <crow.h>
int main() {
    crow::SimpleApp app;
    // DEPRECATED since 3.1 use /api/v2/items
    CROW_ROUTE(app, "/api/v1/items")([]{ return "items"; });
}`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("app.cc", "cpp", src))
	e := findDep(ents, func(s entitySummary) bool { return s.Props["deprecation_source"] == "comment // DEPRECATED" })
	if e == nil {
		t.Fatalf("expected banner-comment deprecation entity, got %+v", ents)
	}
	if e.Props["deprecated"] != "true" {
		t.Errorf("deprecated = %q, want true", e.Props["deprecated"])
	}
	if e.Props["deprecated_since"] != "3.1" {
		t.Errorf("deprecated_since = %q, want 3.1", e.Props["deprecated_since"])
	}
	if e.Props["deprecated_replacement"] != "/api/v2/items" {
		t.Errorf("deprecated_replacement = %q, want /api/v2/items", e.Props["deprecated_replacement"])
	}
	if e.Props["api_version"] != "1" {
		t.Errorf("api_version = %q, want 1", e.Props["api_version"])
	}
	if e.Props["framework"] != "crow" {
		t.Errorf("framework = %q, want crow", e.Props["framework"])
	}
}

// --- 4. oatpp Sunset response header at an ENDPOINT -------------------------

func TestCppDep_OatppSunsetHeader(t *testing.T) {
	src := `
#include <oatpp/web/server/api/ApiController.hpp>
ENDPOINT("GET", "/api/v1/legacy", legacy) {
    auto resp = createResponse(Status::CODE_200, "ok");
    resp->putHeader("Sunset", "Sat, 31 Dec 2025 23:59:59 GMT");
    return resp;
}`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("ctrl.cpp", "cpp", src))
	e := findDep(ents, func(s entitySummary) bool { return s.Props["deprecation_source"] == "Sunset response header" })
	if e == nil {
		t.Fatalf("expected Sunset-header deprecation entity, got %+v", ents)
	}
	if e.Props["deprecated"] != "true" {
		t.Errorf("deprecated = %q, want true", e.Props["deprecated"])
	}
	if e.Props["api_version"] != "1" {
		t.Errorf("api_version = %q, want 1", e.Props["api_version"])
	}
	// header-only signal carries no since/replacement (honest-partial).
	if e.Props["deprecated_since"] != "" {
		t.Errorf("deprecated_since = %q, want empty for a header-only signal", e.Props["deprecated_since"])
	}
}

// --- 5. Pistache Deprecation header at a Routes::Get route ------------------

func TestCppDep_PistacheDeprecationHeader(t *testing.T) {
	src := `
#include <pistache/router.h>
using namespace Pistache::Rest;
void setup(Router &router) {
    Routes::Get(router, "/api/v3/ping", Routes::bind(&handlePing));
}
void handlePing(const Rest::Request &req, Http::ResponseWriter resp) {
    resp.headers().add<Http::Header::Raw>("Deprecation", "true");
}`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("ping.cpp", "cpp", src))
	e := findDep(ents, func(s entitySummary) bool { return s.Props["deprecation_source"] == "Deprecation response header" })
	if e == nil {
		t.Fatalf("expected Deprecation-header deprecation entity, got %+v", ents)
	}
	if e.Props["api_version"] != "3" {
		t.Errorf("api_version = %q, want 3", e.Props["api_version"])
	}
}

// --- 6. NEGATIVE: a non-deprecated versioned route emits nothing -----------

func TestCppDep_NonDeprecatedNone(t *testing.T) {
	src := `
#include <drogon/drogon.h>
ADD_METHOD_TO(C::list, "/api/v1/users", Get);
void list();`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("c.cc", "cpp", src))
	if anyDep(ents) {
		t.Fatalf("expected NO deprecation entity for a non-deprecated route, got %+v", ents)
	}
}

// --- 7. NEGATIVE: a deprecated route with no version segment → no api_version

func TestCppDep_VersionlessNoApiVersion(t *testing.T) {
	src := `
#include <drogon/drogon.h>
ADD_METHOD_TO(C::old, "/users", Get);
[[deprecated("use the new users endpoint")]]
void old();`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("c.cc", "cpp", src))
	e := findDep(ents, func(s entitySummary) bool { return s.Props["deprecation_source"] == "[[deprecated]]" })
	if e == nil {
		t.Fatalf("expected deprecation entity, got %+v", ents)
	}
	if _, ok := e.Props["api_version"]; ok {
		t.Errorf("api_version = %q, want UNSET for a versionless route", e.Props["api_version"])
	}
}

// --- 8. NEGATIVE: [[deprecated]] far from any route does NOT leak -----------

func TestCppDep_DeprecatedHelperNoRouteNone(t *testing.T) {
	src := `
#include <string>
// a plain library helper, no HTTP route anywhere in this file.
[[deprecated("use computeV2 instead")]]
int computeV1(int x) { return x; }`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("util.cc", "cpp", src))
	if anyDep(ents) {
		t.Fatalf("expected NO deprecation entity for a non-route helper, got %+v", ents)
	}
}

// --- 9. NEGATIVE: non-C++ language is ignored ------------------------------

func TestCppDep_NonCppLanguageIgnored(t *testing.T) {
	src := `[[deprecated("use /api/v2")]]
ADD_METHOD_TO(C::x, "/api/v1/x", Get);`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("x.rs", "rust", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-cpp language, got %+v", ents)
	}
}

// --- 10. Bare [[deprecated]] (no message) at a route still credits true ----

func TestCppDep_BareAttribute(t *testing.T) {
	src := `
#include <crow.h>
crow::SimpleApp app;
[[deprecated]]
CROW_ROUTE(app, "/api/v1/old")([]{ return "old"; });`
	ents := extract(t, "custom_cpp_endpoint_deprecation", fi("app.cc", "cpp", src))
	e := findDep(ents, func(s entitySummary) bool { return s.Props["deprecation_source"] == "[[deprecated]]" })
	if e == nil {
		t.Fatalf("expected deprecation entity for bare [[deprecated]], got %+v", ents)
	}
	if e.Props["deprecated"] != "true" {
		t.Errorf("deprecated = %q, want true", e.Props["deprecated"])
	}
	if e.Props["deprecated_replacement"] != "" {
		t.Errorf("deprecated_replacement = %q, want empty for a bare attribute", e.Props["deprecated_replacement"])
	}
	if e.Props["api_version"] != "1" {
		t.Errorf("api_version = %q, want 1", e.Props["api_version"])
	}
}
