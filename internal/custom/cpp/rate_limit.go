package cpp

// rate_limit.go — endpoint rate-limit / throttle stamping for C/C++ HTTP
// frameworks (#4115, child of #3628 Middleware/rate_limit_stamping).
//
// C/C++ greenfield: prior to this pass EVERY C/C++ HTTP-framework cell for
// rate_limit_stamping was `missing`. This pass is deliberately NARROW because
// the honest, verify-first finding is that C++ web frameworks predominantly
// delegate rate limiting to EXTERNAL infrastructure (nginx / envoy / an API
// gateway) — there is no in-code primitive to detect for most of them:
//
//	oatpp / Crow / Pistache / POCO / Restbed / RESTinio / cpprestsdk
//	  — no framework-native rate-limit type. Rate limiting, when present, is
//	    either external middleware or a bespoke token-bucket the project rolled
//	    itself. Those remain honestly `missing` (see registry notes); inventing
//	    a "rate_limited" stamp for a hand-rolled counter would be fabrication.
//
// The ONE C++ framework with a genuine, statically-detectable rate-limit idiom
// is Drogon, which ships a first-class rate limiter:
//
//	1. drogon::RateLimiter factory — the token-bucket primitive:
//	     auto limiter = drogon::RateLimiter::newRateLimiter(
//	         100, std::chrono::seconds(60));
//	   Group: capacity (the request cap) + a std::chrono window. We resolve the
//	   cap + window/seconds → human rate "100/60s" when both are literal. The
//	   RateLimiterPtr type alias usage is the same signal.
//
//	2. A Drogon HttpFilter whose name marks it a rate limiter, bound to routes:
//	     class RateLimitFilter : public HttpFilter<RateLimitFilter> { ... };
//	     FILTER_ADD("RateLimitFilter");   // per-controller route binding
//	     app().registerFilter<RateLimitFilter>();
//	   A filter is Drogon's per-route middleware hook, so a rate-limit filter
//	   bound via FILTER_ADD is route-scoped throttling. The filter CLASS itself
//	   (scope=engine until bound) and each FILTER_ADD binding (scope=route) are
//	   stamped, naming the filter as evidence. Only filters whose name carries a
//	   rate-limit signal (RateLimit / Throttle / RateLimiter) are stamped — a
//	   plain auth/logging filter is NOT a throttle (that stays auth_middleware's
//	   job and is the negative here).
//
// Shared flat contract (same keys every other language stamps so the graph
// answers "which surfaces are throttled and at what rate?"):
//
//	rate_limited      — "true" when a throttle applies.
//	rate_limit        — human rate "100/60s" when statically resolvable from a
//	                    literal capacity + literal std::chrono window; OMITTED
//	                    (honest-partial) otherwise.
//	rate_limit_scope  — "route"  (a RateLimitFilter bound via FILTER_ADD)
//	                    | "engine" (a RateLimiter factory / a rate-limit filter
//	                      class definition not yet bound to a route in-file).
//	rate_limit_source — the recognised idiom: "drogon_ratelimiter" (the factory)
//	                    | "drogon_filter" (a rate-limit HttpFilter / binding).
//	rate_limit_name   — the filter class name (evidence) when from a filter.
//	limit / period    — the resolved numeric cap + window-seconds (when literal).
//
// Every surface adds a SCOPE.Pattern/rate_limit marker (the multi-line Drogon
// idioms do not reduce to a single route op the way a decorator does), mirroring
// the csharp/elixir marker model.
//
// Honesty: partial — heuristic regex on source text. We capture the concrete
// limiter capacity/window and the concrete filter symbol within a file, but do
// NOT resolve a filter class defined in another translation unit, nor bind a
// RateLimiter instance to a specific route. Negatives proven: a non-throttle
// filter and an external/nginx comment do not stamp.
//
// Refs #4115.

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_rate_limit", &cppRateLimitExtractor{})
}

type cppRateLimitExtractor struct{}

func (e *cppRateLimitExtractor) Language() string { return "custom_cpp_rate_limit" }

var (
	// drogon::RateLimiter::newRateLimiter(100, std::chrono::seconds(60))
	// drogon::RateLimiter::newRateLimiter(50, 30s)                       (chrono literal)
	// Group 1 = capacity literal. The window is parsed separately from the
	// remainder of the call args by reChronoWindow so both the std::chrono::X(n)
	// spelling and the `30s`/`2min` chrono-literal suffix spelling are handled.
	reDrogonRateLimiterNew = regexp.MustCompile(
		`\bRateLimiter::newRateLimiter\s*\(\s*(\d+)\s*,\s*([^)]*\)?[^,)]*)\)`)

	// std::chrono::seconds(60) / chrono::minutes(2) / std::chrono::hours(1)
	// — the explicit-duration window spelling. Group 1 = unit, group 2 = count.
	reChronoDuration = regexp.MustCompile(
		`chrono::(seconds|minutes|hours|milliseconds)\s*\(\s*(\d+)\s*\)`)

	// `60s` / `2min` / `1h` / `500ms` — the C++14 chrono user-defined-literal
	// window spelling. Group 1 = count, group 2 = unit suffix.
	reChronoLiteral = regexp.MustCompile(`\b(\d+)\s*(ms|s|min|h)\b`)

	// The Drogon HttpFilter-class / FILTER_ADD / registerFilter<X> regexes are
	// already declared (identically) in auth_middleware.go in this same package
	// — reDrogonFilterClass, reDrogonFilterAdd, reDrogonRegisterFilter — and are
	// reused here. We additionally gate each match through cppNameIsRateLimit so
	// only a throttle-named filter is stamped as a rate limit (a plain auth /
	// logging filter stays auth_middleware's concern and is the negative here).
)

// cppNameIsRateLimit reports whether a filter/class symbol name reads as a
// rate-limit / throttle primitive (so a plain auth/logging filter is NOT
// mis-stamped as a throttle). Conservative on purpose: only the unambiguous
// rate-limit vocabulary qualifies.
func cppNameIsRateLimit(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "ratelimit") ||
		strings.Contains(l, "rate_limit") ||
		strings.Contains(l, "throttle") ||
		strings.Contains(l, "ratelimiter")
}

// cppChronoSeconds converts a std::chrono duration unit + count to whole
// seconds. A sub-second (milliseconds) window returns 0 so the caller reports
// the rate in ms rather than rounding a sub-second window to 0s.
func cppChronoSeconds(unit string, n int) (secs int, subSecondMs int) {
	switch unit {
	case "seconds", "s":
		return n, 0
	case "minutes", "min":
		return n * 60, 0
	case "hours", "h":
		return n * 3600, 0
	case "milliseconds", "ms":
		return 0, n
	}
	return 0, 0
}

// cppResolveWindow extracts the window (in seconds, or sub-second ms) from the
// trailing-args text of a newRateLimiter(...) call. Returns (secs, ms, ok).
// Prefers the explicit std::chrono::unit(n) spelling; falls back to the chrono
// user-defined-literal (`60s`) spelling.
func cppResolveWindow(argsText string) (secs int, ms int, ok bool) {
	if m := reChronoDuration.FindStringSubmatch(argsText); m != nil {
		n, _ := strconv.Atoi(m[2])
		s, sub := cppChronoSeconds(m[1], n)
		return s, sub, true
	}
	if m := reChronoLiteral.FindStringSubmatch(argsText); m != nil {
		n, _ := strconv.Atoi(m[1])
		s, sub := cppChronoSeconds(m[2], n)
		return s, sub, true
	}
	return 0, 0, false
}

// cppHumanRate builds the shared "<limit>/<window>" rate from a cap + a window
// expressed in seconds (or, for a sub-second window, milliseconds). Returns ""
// when the window is unresolved.
func cppHumanRate(limit, secs, ms int) string {
	switch {
	case secs > 0:
		return strconv.Itoa(limit) + "/" + strconv.Itoa(secs) + "s"
	case ms > 0:
		return strconv.Itoa(limit) + "/" + strconv.Itoa(ms) + "ms"
	}
	return ""
}

func (e *cppRateLimitExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_rate_limit.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "cpp" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: must mention one of the recognised Drogon rate-limit idioms.
	if !strings.Contains(src, "RateLimiter") &&
		!strings.Contains(src, "HttpFilter") &&
		!strings.Contains(src, "FILTER_ADD") &&
		!strings.Contains(src, "registerFilter") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// ---------------------------------------------------------------------
	// 1. drogon::RateLimiter::newRateLimiter(capacity, chrono-window)
	//    The token-bucket factory. Engine-scope (a limiter instance is not
	//    bound to a named route in-file). Resolves the human rate when the
	//    capacity + window are both literal.
	// ---------------------------------------------------------------------
	for _, m := range reDrogonRateLimiterNew.FindAllStringSubmatchIndex(src, -1) {
		cap, _ := strconv.Atoi(src[m[2]:m[3]])
		argsText := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "drogon_ratelimiter:" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "drogon",
			"provenance", "INFERRED_FROM_DROGON_RATELIMITER",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "engine",
			"rate_limit_source", "drogon_ratelimiter")
		if cap > 0 {
			ent.Properties["limit"] = strconv.Itoa(cap)
		}
		if secs, ms, ok := cppResolveWindow(argsText); ok {
			if secs > 0 {
				ent.Properties["period"] = strconv.Itoa(secs)
			}
			if cap > 0 {
				if rate := cppHumanRate(cap, secs, ms); rate != "" {
					ent.Properties["rate_limit"] = rate
				}
			}
		}
		add(ent)
	}

	// ---------------------------------------------------------------------
	// 2. Rate-limit HttpFilter subclasses + their route bindings.
	//    A filter whose name reads as a throttle is a Drogon rate-limit
	//    middleware. The class definition is engine-scope (not yet bound); a
	//    FILTER_ADD("X") / registerFilter<X>() binding naming a rate-limit
	//    filter is route-scope throttling.
	// ---------------------------------------------------------------------

	// 2a. The filter class definition (engine scope).
	for _, m := range reDrogonFilterClass.FindAllStringSubmatchIndex(src, -1) {
		fname := src[m[2]:m[3]]
		if !cppNameIsRateLimit(fname) {
			continue // a plain auth/logging filter — not a throttle (negative).
		}
		line := lineOf(src, m[0])
		ent := makeEntity("drogon_ratelimit_filter:"+fname, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "drogon",
			"provenance", "INFERRED_FROM_DROGON_RATELIMIT_FILTER",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "engine",
			"rate_limit_source", "drogon_filter",
			"rate_limit_name", fname)
		add(ent)
	}

	// 2b. FILTER_ADD("X") — per-route binding (route scope) of a rate-limit filter.
	for _, m := range reDrogonFilterAdd.FindAllStringSubmatchIndex(src, -1) {
		fname := src[m[2]:m[3]]
		if !cppNameIsRateLimit(fname) {
			continue
		}
		line := lineOf(src, m[0])
		name := "drogon_ratelimit_bind:" + fname + ":" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "drogon",
			"provenance", "INFERRED_FROM_DROGON_RATELIMIT_FILTER",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "route",
			"rate_limit_source", "drogon_filter",
			"rate_limit_name", fname,
			"rate_limit_binding", "FILTER_ADD")
		add(ent)
	}

	// 2c. registerFilter<X>() — global registration (engine scope) of a rate-limit filter.
	for _, m := range reDrogonRegisterFilter.FindAllStringSubmatchIndex(src, -1) {
		fname := src[m[2]:m[3]]
		if !cppNameIsRateLimit(fname) {
			continue
		}
		line := lineOf(src, m[0])
		name := "drogon_ratelimit_register:" + fname + ":" + file.Path + ":" + strconv.Itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "rate_limit", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "drogon",
			"provenance", "INFERRED_FROM_DROGON_RATELIMIT_FILTER",
			"kind", "rate_limit",
			"rate_limited", "true",
			"rate_limit_scope", "engine",
			"rate_limit_source", "drogon_filter",
			"rate_limit_name", fname,
			"rate_limit_binding", "registerFilter")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
