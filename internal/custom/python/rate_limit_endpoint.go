// rate_limit_endpoint.go — endpoint rate-limit / throttle stamping for Python
// web frameworks (child of #3628, "[api] endpoint rate-limit / throttle
// stamping").
//
// A `rate_limit` SCOPE.Pattern node already records "this file uses a rate
// limiter" (internal/patterns/rate_limit_extractor.go). This file attributes
// the limiter to the SPECIFIC route endpoint and resolves the numeric rate,
// stamping the flat property contract the auth resolver (auth_endpoint.go) uses
// so the graph answers "which endpoints are throttled and at what rate?":
//
//	rate_limited      — "true" when a throttle applies to the endpoint.
//	rate_limit        — human rate "100/min" / "5/minute" when statically
//	                    resolvable; OMITTED (honest-partial) when the rate is
//	                    config-/settings-driven (e.g. a DRF throttle class whose
//	                    rate lives in REST_FRAMEWORK['DEFAULT_THROTTLE_RATES']).
//	rate_limit_scope  — "user" | "anon" | "ip" | "endpoint" — the throttle key.
//	rate_limit_source — the recognised throttle symbol / decorator (evidence).
//
// Supported surfaces:
//
//	slowapi (FastAPI/Starlette) — `@limiter.limit("5/minute")` on the route.
//	django-ratelimit            — `@ratelimit(key='ip', rate='5/m')`.
//	DRF                         — `@throttle_classes([UserRateThrottle])` /
//	                              `throttle_classes = [UserRateThrottle]`; the
//	                              rate is resolved when a co-located throttle
//	                              subclass declares `rate = '1000/day'`,
//	                              otherwise honest-partial (rate omitted).
//	flask-limiter               — `@limiter.limit("100/hour")` on the view.
package python

import (
	"regexp"
	"strings"
)

// pyRateLimit is the resolved throttle posture for one route endpoint.
type pyRateLimit struct {
	Rate   string // "5/minute", "1000/day", … or "" (honest-partial)
	Scope  string // "user" | "anon" | "ip" | "endpoint" | ""
	Source string // evidence symbol / decorator
	found  bool
}

// stamp writes the resolved posture onto an endpoint Properties map. No-op when
// no throttle signal was recognised (never fabricates rate_limited=false).
func (r pyRateLimit) stamp(props map[string]string) {
	if props == nil || !r.found {
		return
	}
	props["rate_limited"] = "true"
	if r.Rate != "" {
		props["rate_limit"] = r.Rate
	}
	if r.Scope != "" {
		props["rate_limit_scope"] = r.Scope
	}
	if r.Source != "" {
		props["rate_limit_source"] = r.Source
	}
}

// pyLimiterDecoratorRe captures slowapi / flask-limiter `@limiter.limit("…")`
// (any receiver name ending in a `.limit(` call). Group 1 = receiver, group 2 =
// the rate string.
var pyLimiterDecoratorRe = regexp.MustCompile(
	`@\s*([A-Za-z_][\w.]*)\.limit\s*\(\s*["']([^"']+)["']`)

// pyRatelimitDecoratorRe captures django-ratelimit `@ratelimit(key='ip',
// rate='5/m')` (also matches the `@ratelimit(...)` import-aliased form). Group 1
// = the full argument list.
var pyRatelimitDecoratorRe = regexp.MustCompile(
	`@\s*(?:[\w.]*\.)?ratelimit\s*\(([^)]*)\)`)

// pyRatelimitRateKwRe / pyRatelimitKeyKwRe pull the rate= and key= kwargs out of
// a django-ratelimit decorator argument list.
var (
	pyRatelimitRateKwRe = regexp.MustCompile(`\brate\s*=\s*["']([^"']+)["']`)
	pyRatelimitKeyKwRe  = regexp.MustCompile(`\bkey\s*=\s*["']([^"']+)["']`)
)

// pyThrottleClassesRe captures DRF `@throttle_classes([UserRateThrottle])` and
// the assignment form `throttle_classes = [UserRateThrottle]`. Group 1 = the
// raw class list.
var pyThrottleClassesRe = regexp.MustCompile(
	`(?:@throttle_classes\s*\(\s*|throttle_classes\s*=\s*)\[([^\]]*)\]`)

// pyThrottleRateAttrRe captures a throttle subclass's `rate = '1000/day'`
// attribute, used to resolve a co-located custom throttle's rate. Group 1 = the
// rate string.
var pyThrottleRateAttrRe = regexp.MustCompile(`\brate\s*=\s*["']([^"']+)["']`)

// drfBuiltinThrottleScope maps the built-in DRF throttle classes to their scope
// key. The rate of these lives in settings (DEFAULT_THROTTLE_RATES), so it is
// honest-partial (rate omitted) unless a co-located subclass declares it.
var drfBuiltinThrottleScope = map[string]string{
	"UserRateThrottle":   "user",
	"AnonRateThrottle":   "anon",
	"ScopedRateThrottle": "endpoint",
}

// normalizeRatelimitScope maps a django-ratelimit `key=` value to the scope
// vocabulary. `ip` / `user` / `header:…` → ip/user/endpoint.
func normalizeRatelimitScope(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch {
	case key == "ip" || strings.HasPrefix(key, "ip"):
		return "ip"
	case key == "user" || strings.HasPrefix(key, "user"):
		return "user"
	case key == "":
		return ""
	default:
		return "endpoint"
	}
}

// resolveSlowapiRateLimit scans a decorator block for a slowapi / flask-limiter
// `@<limiter>.limit("rate")` decorator.
func resolveSlowapiRateLimit(decoratorBlock string) pyRateLimit {
	if m := pyLimiterDecoratorRe.FindStringSubmatch(decoratorBlock); m != nil {
		return pyRateLimit{
			Rate:   strings.TrimSpace(m[2]),
			Source: m[1] + ".limit",
			found:  true,
		}
	}
	return pyRateLimit{}
}

// resolveDjangoRatelimit scans a decorator block for a django-ratelimit
// `@ratelimit(key=…, rate=…)` decorator.
func resolveDjangoRatelimit(decoratorBlock string) pyRateLimit {
	m := pyRatelimitDecoratorRe.FindStringSubmatch(decoratorBlock)
	if m == nil {
		return pyRateLimit{}
	}
	r := pyRateLimit{Source: "ratelimit", found: true}
	if rm := pyRatelimitRateKwRe.FindStringSubmatch(m[1]); rm != nil {
		r.Rate = strings.TrimSpace(rm[1])
	}
	if km := pyRatelimitKeyKwRe.FindStringSubmatch(m[1]); km != nil {
		r.Scope = normalizeRatelimitScope(km[1])
	}
	return r
}

// resolveDRFThrottle scans a decorator/body block for DRF throttle_classes and,
// when a co-located throttle subclass in `source` declares a `rate`, resolves
// it. `source` is the whole file so a custom throttle's rate attribute can be
// found; pass "" to skip rate resolution (built-in throttles → honest-partial).
func resolveDRFThrottle(block, source string) pyRateLimit {
	m := pyThrottleClassesRe.FindStringSubmatch(block)
	if m == nil {
		return pyRateLimit{}
	}
	classes := splitThrottleClasses(m[1])
	if len(classes) == 0 {
		return pyRateLimit{}
	}
	r := pyRateLimit{Source: classes[0], found: true}
	// Scope from a recognised built-in.
	if scope, ok := drfBuiltinThrottleScope[classes[0]]; ok {
		r.Scope = scope
	}
	// Resolve the rate from a co-located custom throttle subclass:
	//   class BurstRateThrottle(UserRateThrottle): rate = '60/min'
	if source != "" {
		if rate := drfThrottleRateForClass(source, classes[0]); rate != "" {
			r.Rate = rate
		}
	}
	return r
}

// drfThrottleRateForClass finds `class <name>(...): ... rate = '…'` in source
// and returns the rate, or "" when the class is not locally defined / has no
// literal rate (honest-partial).
func drfThrottleRateForClass(source, className string) string {
	classRe := regexp.MustCompile(`class\s+` + regexp.QuoteMeta(className) + `\s*\([^)]*\)\s*:`)
	loc := classRe.FindStringIndex(source)
	if loc == nil {
		return ""
	}
	// Look at the class body (next ~400 chars) for a `rate = '…'` attribute.
	end := loc[1] + 400
	if end > len(source) {
		end = len(source)
	}
	body := source[loc[1]:end]
	// Stop at the next top-level `class`/`def` at column 0 to avoid bleeding.
	if cut := regexp.MustCompile(`\n(?:class|def)\s`).FindStringIndex(body); cut != nil {
		body = body[:cut[0]]
	}
	if rm := pyThrottleRateAttrRe.FindStringSubmatch(body); rm != nil {
		return strings.TrimSpace(rm[1])
	}
	return ""
}

// splitThrottleClasses splits a `[A, B, C]` class list into trimmed symbols.
func splitThrottleClasses(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// resolvePyEndpointRateLimit is the unified resolver: it tries each recognised
// surface against the supplied decorator block, returning the first match.
// `source` is the whole file (for DRF custom-throttle rate resolution).
func resolvePyEndpointRateLimit(decoratorBlock, source string) pyRateLimit {
	if r := resolveSlowapiRateLimit(decoratorBlock); r.found {
		return r
	}
	if r := resolveDjangoRatelimit(decoratorBlock); r.found {
		return r
	}
	if r := resolveDRFThrottle(decoratorBlock, source); r.found {
		return r
	}
	return pyRateLimit{}
}
