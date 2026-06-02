// Cross-framework rate-limit / throttle stamping for the JS/TS backend-HTTP
// frameworks (child of #3628, "[api] endpoint rate-limit / throttle stamping").
//
// This pass answers "which endpoints are throttled and at what rate?" by
// resolving a per-endpoint rate-limit posture from static middleware signals
// and stamping it onto the route op with the same FLAT-property model the auth
// pass (http_endpoint_jsts_auth.go, #2852) and the deprecation pass use. It
// adds NO parallel node — a separate `rate_limit` SCOPE.Pattern entity already
// exists (internal/patterns/rate_limit_extractor.go) for the "this file uses a
// rate limiter" signal; this pass attributes the limiter to the SPECIFIC
// endpoint and resolves the numeric rate, which the pattern node cannot do.
//
// Recognised express-rate-limit (and the API-compatible express-slow-down /
// rate-limiter-flexible Express middleware) shapes:
//
//   - a limiter binding: `const limiter = rateLimit({ windowMs: 60000, max: 100 })`
//     — captured so its resolved rate can be attached wherever the binding is
//     applied.
//   - app/router-level application: `app.use(limiter)` — applies to every
//     endpoint registered in the same file scope.
//   - route-level application: `router.get('/x', limiter, handler)` — applies to
//     that one endpoint (strongest signal).
//   - an inline limiter passed directly to a route, e.g.
//     `app.get('/x', rateLimit({ windowMs: 60000, max: 100 }), h)`.
//
// Output (stamped only on producer-side http_endpoint_definition entities):
//
//	rate_limited      — "true" when a limiter applies to the endpoint.
//	rate_limit        — human rate "100/60s" when windowMs+max resolve
//	                    statically; OMITTED (honest-partial) when the limiter is
//	                    config-/env-driven and cannot be resolved without
//	                    fabrication.
//	rate_limit_scope  — "route" | "app" (where the limiter was applied).
//	rate_limit_source — the recognised limiter symbol / library (evidence key).
//
// Refs the #3628 rate-limit child ticket.
package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// jsRateLimitFactoryNames are the express-rate-limit-compatible factory calls.
// All accept an options object with `windowMs` + `max` (express-rate-limit,
// express-slow-down) so the same resolver handles them.
var jsRateLimitFactoryNames = map[string]bool{
	"ratelimit":     true, // express-rate-limit default export
	"ratelimiter":   true, // common alias
	"slowdown":      true, // express-slow-down
	"ratelimiterrl": true, // rate-limiter-flexible express helper (rare alias)
}

// jsRateLimitFactoryCallRe captures a limiter factory call + its options-object
// body, e.g. `rateLimit({ windowMs: 60000, max: 100 })`. Group 1 = factory
// name, group 2 = the brace body (may be empty for arg-less / spread configs).
var jsRateLimitFactoryCallRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*)\s*\(\s*\{([^{}]*)\}\s*\)`)

// jsRateLimitBindingRe captures `const limiter = rateLimit({...})` so a named
// binding can be resolved back to its rate when applied elsewhere. Group 1 =
// the binding identifier, group 2 = factory name, group 3 = options body.
var jsRateLimitBindingRe = regexp.MustCompile(
	`\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*([A-Za-z_$][\w$]*)\s*\(\s*\{([^{}]*)\}\s*\)`)

// jsRateLimitWindowRe / jsRateLimitMaxRe pull the numeric windowMs / max out of
// an options body. windowMs may be a bare ms number or a simple `n * unit`
// arithmetic product (`15 * 60 * 1000`); max is a bare integer.
var (
	jsRateLimitWindowRe = regexp.MustCompile(`\bwindowMs\s*:\s*([0-9][0-9_*\s]*[0-9]|[0-9]+)`)
	jsRateLimitMaxRe    = regexp.MustCompile(`\b(?:max|limit|points)\s*:\s*([0-9]+)`)
)

// jsRateLimitNamesLower is the set of binding identifiers that conventionally
// hold a rate limiter, used to recognise an applied limiter whose factory call
// is in another module (imported) and therefore not resolvable to a rate.
var jsRateLimitNameHintRe = regexp.MustCompile(`(?i)(rate.?limit|throttl|slow.?down|limiter)`)

// jsRateLimiter is a resolved limiter: its evidence symbol and (when static)
// human-readable rate.
type jsRateLimiter struct {
	source string // evidence symbol / library
	rate   string // "100/60s" or "" when unresolved
}

// resolveJSRate turns a windowMs + max options body into a human rate string,
// or "" when either is missing / non-numeric (honest-partial).
func resolveJSRate(optsBody string) string {
	wm := jsRateLimitWindowRe.FindStringSubmatch(optsBody)
	mx := jsRateLimitMaxRe.FindStringSubmatch(optsBody)
	if wm == nil || mx == nil {
		return ""
	}
	windowMs := evalJSNumberProduct(wm[1])
	if windowMs <= 0 {
		return ""
	}
	max, err := strconv.Atoi(strings.TrimSpace(mx[1]))
	if err != nil || max <= 0 {
		return ""
	}
	// Render the window in seconds ("100/60s") — the canonical express-rate-limit
	// representation, which keeps windowMs:60000 ⇒ "60s" (not "1m") so the rate
	// is read back exactly as configured. Sub-second windows fall back to ms.
	seconds := windowMs / 1000
	if seconds <= 0 || windowMs%1000 != 0 {
		return strconv.Itoa(max) + "/" + strconv.Itoa(windowMs) + "ms"
	}
	return strconv.Itoa(max) + "/" + strconv.Itoa(seconds) + "s"
}

// evalJSNumberProduct evaluates a bare integer or a `n * n * n` product (with
// optional `_` digit separators), returning 0 on any non-numeric token.
func evalJSNumberProduct(expr string) int {
	expr = strings.ReplaceAll(expr, "_", "")
	parts := strings.Split(expr, "*")
	product := 1
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return 0
		}
		product *= n
	}
	return product
}

// recognizeRateLimitFactory reports whether arg is an express-rate-limit-style
// limiter application (a factory call or a binding that looks like a limiter),
// returning the resolved limiter. Returns ok=false for non-limiter args.
func recognizeRateLimitFactory(arg string, bindings map[string]jsRateLimiter) (jsRateLimiter, bool) {
	raw := strings.TrimSpace(arg)
	if raw == "" {
		return jsRateLimiter{}, false
	}
	// A named binding applied directly, e.g. `app.use(limiter)`.
	if lim, ok := bindings[raw]; ok {
		return lim, true
	}
	// An inline factory call, e.g. `rateLimit({ windowMs, max })`.
	if fm := jsRateLimitFactoryCallRe.FindStringSubmatch(raw); fm != nil {
		if jsRateLimitFactoryNames[strings.ToLower(fm[1])] {
			return jsRateLimiter{source: fm[1], rate: resolveJSRate(fm[2])}, true
		}
	}
	// A bare identifier whose name strongly implies a rate limiter but whose
	// factory call lives in another (imported) module — honest-partial: mark
	// limited, omit the rate.
	if !strings.ContainsAny(raw, "({[") && jsRateLimitNameHintRe.MatchString(raw) {
		return jsRateLimiter{source: symbolHead(raw)}, true
	}
	return jsRateLimiter{}, false
}

// indexJSRateLimitBindings collects `const x = rateLimit({...})` bindings so an
// applied binding resolves to its rate.
func indexJSRateLimitBindings(content string) map[string]jsRateLimiter {
	out := map[string]jsRateLimiter{}
	for _, m := range jsRateLimitBindingRe.FindAllStringSubmatch(content, -1) {
		if !jsRateLimitFactoryNames[strings.ToLower(m[2])] {
			continue
		}
		out[m[1]] = jsRateLimiter{source: m[2], rate: resolveJSRate(m[3])}
	}
	return out
}

// applyJSTSRateLimit resolves and stamps rate_limited / rate_limit /
// rate_limit_scope / rate_limit_source on every JS/TS synthetic backend
// endpoint emitted for this file. It mutates Properties in place and never adds
// or removes entities. `before` is the entity-slice length captured before the
// JS/TS synthesizers ran (same window the auth pass uses).
func applyJSTSRateLimit(content, path string, entities []types.EntityRecord, before int) {
	if len(content) == 0 || before >= len(entities) {
		return
	}
	bindings := indexJSRateLimitBindings(content)

	// App/router-level limiter: `app.use(limiter)` / `app.use(rateLimit({...}))`.
	// Applies to every endpoint in file scope.
	var appLimiter (jsRateLimiter)
	appLimiterSet := false
	for _, m := range jsAppUseRe.FindAllStringSubmatchIndex(content, -1) {
		arg := content[m[4]:m[5]]
		if lim, ok := recognizeRateLimitFactory(strings.TrimSpace(arg), bindings); ok {
			appLimiter = lim
			appLimiterSet = true
			break
		}
	}

	// Route-level limiters indexed by (verb, canonical path).
	routeLimiters := map[string]jsRateLimiter{}
	for _, m := range jsRouteRegistrationRe.FindAllStringSubmatchIndex(content, -1) {
		verb := strings.ToUpper(content[m[4]:m[5]])
		switch verb {
		case "DEL":
			verb = "DELETE"
		case "OPTS":
			verb = "OPTIONS"
		}
		raw := content[m[6]:m[7]]
		rest := content[m[8]:m[9]]
		var found jsRateLimiter
		ok := false
		for _, a := range splitTopLevelArgs(rest) {
			if lim, isLim := recognizeRateLimitFactory(strings.TrimSpace(a), bindings); isLim {
				found, ok = lim, true
				break
			}
		}
		if !ok {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		if canonical == "" {
			continue
		}
		routeLimiters[verb+" "+canonical] = found
	}

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path || e.Properties == nil {
			continue
		}
		verb := strings.ToUpper(e.Properties["verb"])
		canonical := e.Properties["path"]
		key := verb + " " + canonical

		if lim, ok := routeLimiters[key]; ok {
			stampJSRateLimit(e.Properties, lim, "route")
			continue
		}
		if appLimiterSet {
			stampJSRateLimit(e.Properties, appLimiter, "app")
		}
	}
}

// stampJSRateLimit writes the flat rate-limit property contract onto an
// endpoint Properties map. The rate is omitted when it could not be resolved
// statically (honest-partial — never fabricated).
func stampJSRateLimit(props map[string]string, lim jsRateLimiter, scope string) {
	props["rate_limited"] = "true"
	props["rate_limit_scope"] = scope
	if lim.source != "" {
		props["rate_limit_source"] = lim.source
	}
	if lim.rate != "" {
		props["rate_limit"] = lim.rate
	}
}
