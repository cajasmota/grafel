package cpp_test

// rate_limit_test.go — value-asserting fixtures for the C++ (Drogon)
// rate_limit_stamping pass (#4115). Proves the flat contract
// (rate_limited / rate_limit / rate_limit_scope / rate_limit_source /
// rate_limit_name / limit / period) fires for the genuine Drogon idioms and
// proves the negatives (non-throttle filter, external/nginx rate limiting,
// non-Drogon C++ frameworks) do NOT stamp.

import "testing"

// findRateLimit returns the first SCOPE.Pattern/rate_limit entity whose
// rate_limit_name (or, when absent, Name) matches, or nil.
func findRateLimit(ents []entitySummary, pred func(entitySummary) bool) *entitySummary {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Pattern" && ents[i].Subtype == "rate_limit" && pred(ents[i]) {
			return &ents[i]
		}
	}
	return nil
}

func anyRateLimit(ents []entitySummary) bool {
	return findRateLimit(ents, func(entitySummary) bool { return true }) != nil
}

// --- 1. drogon::RateLimiter factory with std::chrono::seconds window -------

func TestCppRateLimit_DrogonRateLimiterChronoSeconds(t *testing.T) {
	src := `
#include <drogon/drogon.h>
#include <drogon/utils/RateLimiter.h>
using namespace drogon;
void setup() {
    auto limiter = drogon::RateLimiter::newRateLimiter(100, std::chrono::seconds(60));
}`
	ents := extract(t, "custom_cpp_rate_limit", fi("svc.cc", "cpp", src))
	e := findRateLimit(ents, func(s entitySummary) bool {
		return s.Props["rate_limit_source"] == "drogon_ratelimiter"
	})
	if e == nil {
		t.Fatalf("expected drogon_ratelimiter rate-limit entity, got %+v", ents)
	}
	if e.Props["rate_limited"] != "true" {
		t.Errorf("rate_limited = %q, want true", e.Props["rate_limited"])
	}
	if e.Props["limit"] != "100" {
		t.Errorf("limit = %q, want 100", e.Props["limit"])
	}
	if e.Props["period"] != "60" {
		t.Errorf("period = %q, want 60", e.Props["period"])
	}
	if e.Props["rate_limit"] != "100/60s" {
		t.Errorf("rate_limit = %q, want 100/60s", e.Props["rate_limit"])
	}
	if e.Props["rate_limit_scope"] != "engine" {
		t.Errorf("rate_limit_scope = %q, want engine", e.Props["rate_limit_scope"])
	}
}

// --- 2. drogon::RateLimiter factory with chrono user-defined literal (2min) -

func TestCppRateLimit_DrogonRateLimiterChronoLiteral(t *testing.T) {
	src := `
#include <drogon/drogon.h>
using namespace std::chrono_literals;
auto rl = drogon::RateLimiter::newRateLimiter(30, 2min);`
	ents := extract(t, "custom_cpp_rate_limit", fi("svc.cc", "cpp", src))
	e := findRateLimit(ents, func(s entitySummary) bool {
		return s.Props["rate_limit_source"] == "drogon_ratelimiter"
	})
	if e == nil {
		t.Fatalf("expected drogon_ratelimiter entity, got %+v", ents)
	}
	if e.Props["limit"] != "30" {
		t.Errorf("limit = %q, want 30", e.Props["limit"])
	}
	if e.Props["period"] != "120" { // 2min -> 120s
		t.Errorf("period = %q, want 120", e.Props["period"])
	}
	if e.Props["rate_limit"] != "30/120s" {
		t.Errorf("rate_limit = %q, want 30/120s", e.Props["rate_limit"])
	}
}

// --- 3. RateLimiter with a non-literal window → honest-partial (rate omitted) -

func TestCppRateLimit_DrogonRateLimiterPartialWindow(t *testing.T) {
	src := `
#include <drogon/drogon.h>
auto rl = drogon::RateLimiter::newRateLimiter(200, windowFromConfig());`
	ents := extract(t, "custom_cpp_rate_limit", fi("svc.cc", "cpp", src))
	e := findRateLimit(ents, func(s entitySummary) bool {
		return s.Props["rate_limit_source"] == "drogon_ratelimiter"
	})
	if e == nil {
		t.Fatalf("expected drogon_ratelimiter entity, got %+v", ents)
	}
	if e.Props["rate_limited"] != "true" {
		t.Errorf("rate_limited = %q, want true", e.Props["rate_limited"])
	}
	if e.Props["limit"] != "200" {
		t.Errorf("limit = %q, want 200", e.Props["limit"])
	}
	if e.Props["rate_limit"] != "" {
		t.Errorf("rate_limit = %q, want omitted (non-literal window)", e.Props["rate_limit"])
	}
	if e.Props["period"] != "" {
		t.Errorf("period = %q, want omitted (non-literal window)", e.Props["period"])
	}
}

// --- 4. Rate-limit HttpFilter class + FILTER_ADD route binding -------------

func TestCppRateLimit_DrogonFilterAndBinding(t *testing.T) {
	src := `
#include <drogon/HttpFilter.h>
using namespace drogon;
class RateLimitFilter : public HttpFilter<RateLimitFilter> {
  public:
    void doFilter(const HttpRequestPtr &req, FilterCallback &&fcb, FilterChainCallback &&ccb) override;
};
class ApiController : public drogon::HttpController<ApiController> {
  public:
    METHOD_LIST_BEGIN
    ADD_METHOD_TO(ApiController::list, "/api/items", Get);
    METHOD_LIST_END
    void list(...) {
        FILTER_ADD("RateLimitFilter");
    }
};`
	ents := extract(t, "custom_cpp_rate_limit", fi("ctrl.cc", "cpp", src))

	cls := findRateLimit(ents, func(s entitySummary) bool {
		return s.Props["rate_limit_name"] == "RateLimitFilter" && s.Props["rate_limit_scope"] == "engine"
	})
	if cls == nil {
		t.Fatalf("expected engine-scope rate-limit filter class entity, got %+v", ents)
	}
	if cls.Props["rate_limit_source"] != "drogon_filter" {
		t.Errorf("class source = %q, want drogon_filter", cls.Props["rate_limit_source"])
	}

	bind := findRateLimit(ents, func(s entitySummary) bool {
		return s.Props["rate_limit_name"] == "RateLimitFilter" && s.Props["rate_limit_scope"] == "route"
	})
	if bind == nil {
		t.Fatalf("expected route-scope FILTER_ADD binding entity, got %+v", ents)
	}
	if bind.Props["rate_limit_binding"] != "FILTER_ADD" {
		t.Errorf("binding = %q, want FILTER_ADD", bind.Props["rate_limit_binding"])
	}
	if bind.Props["rate_limited"] != "true" {
		t.Errorf("rate_limited = %q, want true", bind.Props["rate_limited"])
	}
}

// --- 5. registerFilter<X>() global registration ----------------------------

func TestCppRateLimit_DrogonRegisterFilter(t *testing.T) {
	src := `
#include <drogon/drogon.h>
int main() {
    drogon::app().registerFilter<ThrottleFilter>();
    drogon::app().run();
}`
	ents := extract(t, "custom_cpp_rate_limit", fi("main.cc", "cpp", src))
	e := findRateLimit(ents, func(s entitySummary) bool {
		return s.Props["rate_limit_binding"] == "registerFilter"
	})
	if e == nil {
		t.Fatalf("expected registerFilter throttle entity, got %+v", ents)
	}
	if e.Props["rate_limit_name"] != "ThrottleFilter" {
		t.Errorf("rate_limit_name = %q, want ThrottleFilter", e.Props["rate_limit_name"])
	}
	if e.Props["rate_limit_scope"] != "engine" {
		t.Errorf("scope = %q, want engine", e.Props["rate_limit_scope"])
	}
}

// --- NEGATIVE 6: a plain auth filter is NOT a throttle ---------------------

func TestCppRateLimit_NegativeAuthFilterNotThrottle(t *testing.T) {
	src := `
#include <drogon/HttpFilter.h>
class JwtAuthFilter : public drogon::HttpFilter<JwtAuthFilter> {
    void doFilter(...) override;
};
void setup() {
    FILTER_ADD("JwtAuthFilter");
    drogon::app().registerFilter<LoginRequiredFilter>();
}`
	ents := extract(t, "custom_cpp_rate_limit", fi("auth.cc", "cpp", src))
	if anyRateLimit(ents) {
		t.Fatalf("auth/login filters must NOT stamp rate_limit; got %+v", ents)
	}
}

// --- NEGATIVE 7: external (nginx/envoy) rate limiting is not in-code -------

func TestCppRateLimit_NegativeExternalRateLimit(t *testing.T) {
	// A Drogon service whose rate limiting is delegated to nginx — only a
	// comment mentions it; nothing statically detectable in app code.
	src := `
#include <drogon/drogon.h>
// Rate limiting is enforced by nginx (limit_req_zone) in front of this service.
int main() {
    drogon::app().registerHandler("/ping", [](const drogon::HttpRequestPtr &,
        std::function<void(const drogon::HttpResponsePtr &)> &&cb) { /* ... */ });
    drogon::app().run();
}`
	ents := extract(t, "custom_cpp_rate_limit", fi("main.cc", "cpp", src))
	if anyRateLimit(ents) {
		t.Fatalf("external/nginx rate limiting must NOT stamp; got %+v", ents)
	}
}

// --- NEGATIVE 8: non-Drogon C++ frameworks have no detectable idiom --------

func TestCppRateLimit_NegativeNonDrogonFrameworks(t *testing.T) {
	// oatpp-style interceptor + a hand-rolled counter: no framework-native
	// rate-limit primitive, so nothing is stamped (honest-missing).
	src := `
#include <oatpp/web/server/interceptor/RequestInterceptor.hpp>
class MyInterceptor : public oatpp::web::server::interceptor::RequestInterceptor {
    int requestCount = 0; // hand-rolled, not a framework primitive
};`
	ents := extract(t, "custom_cpp_rate_limit", fi("interceptor.cpp", "cpp", src))
	if anyRateLimit(ents) {
		t.Fatalf("non-Drogon C++ frameworks must NOT stamp rate_limit; got %+v", ents)
	}
}

// --- NEGATIVE 9: wrong language gate (a .rs file mislabeled) ---------------

func TestCppRateLimit_NegativeWrongLanguage(t *testing.T) {
	src := `drogon::RateLimiter::newRateLimiter(100, std::chrono::seconds(60));`
	ents := extract(t, "custom_cpp_rate_limit", fi("svc.rs", "rust", src))
	if len(ents) != 0 {
		t.Fatalf("non-cpp language must yield no entities; got %+v", ents)
	}
}
