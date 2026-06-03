// http_endpoint_python_ratelimit.go — cross-framework rate-limit / throttle
// stamping for the YAML-synthesized Python backend-HTTP frameworks (child of
// #3628, "[api] endpoint rate-limit / throttle stamping").
//
// A `rate_limit` SCOPE.Pattern node already records "this file uses a rate
// limiter" (internal/patterns/rate_limit_extractor.go). This pass attributes the
// limiter to the SPECIFIC synthesized endpoint and resolves the numeric rate,
// stamping the SAME flat property contract the JS/TS (#2853), Java (#4082) and
// PHP (#4073) passes use, so the graph answers "which endpoints are throttled
// and at what rate?" uniformly across the stack.
//
// Where Flask + FastAPI already get this via the custom AST extractors
// (internal/custom/python/{flask,fastapi}.go → rate_limit_endpoint.go), this
// pass brings the SIBLING frameworks synthesized from the engine YAML rules to
// parity — Starlette / Sanic / Litestar / Quart / aiohttp / Bottle / CherryPy /
// Falcon / Hug / Tornado / Pyramid — by binding decorator-based limiters to the
// handler def name the synthesizer recorded in `source_handler`.
//
// Recognised surfaces (decorator stacked on the handler def):
//
//	slowapi / flask-limiter — `@limiter.limit("100/minute")` (any receiver ending
//	                          in `.limit(`). rate = the literal, scope omitted.
//	django-ratelimit        — `@ratelimit(key='ip', rate='10/m')`. rate + scope
//	                          resolved from the kwargs.
//	DRF (APIView/ViewSet)   — class-body `throttle_classes = [UserRateThrottle]`
//	                          (scope from the built-in) / `throttle_scope =
//	                          'burst'` (scope=endpoint, named); rate resolved from
//	                          a co-located custom throttle subclass `rate = '...'`,
//	                          otherwise honest-partial (lives in settings).
//
// Output (stamped only on producer-side http_endpoint_definition entities this
// file emitted):
//
//	rate_limited           — "true" when a limiter applies to the endpoint.
//	rate_limit             — human rate "100/minute" when statically resolvable;
//	                         OMITTED (honest-partial) when config-/settings-driven.
//	rate_limit_scope       — "user" | "anon" | "ip" | "endpoint" — the throttle key.
//	rate_limit_source      — the recognised throttle symbol / decorator (evidence).
//	rate_limit_scope_name  — the DRF `throttle_scope` literal (evidence), when set.
//
// Honest-partial: a DRF built-in throttle's rate lives in
// REST_FRAMEWORK['DEFAULT_THROTTLE_RATES'] and is not fabricated; the scope is
// still recorded. DRF view attributes bind only when the view class is defined
// in the SAME file as its endpoints (no cross-file ViewSet attribution).
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// pyRLPosture is the resolved throttle posture for one endpoint.
type pyRLPosture struct {
	rate      string // "100/minute", "10/m", … or "" (honest-partial)
	scope     string // "user" | "anon" | "ip" | "endpoint" | ""
	source    string // evidence symbol / decorator
	scopeName string // DRF throttle_scope literal, when set
}

// stamp writes the flat rate-limit contract onto an endpoint Properties map.
func (p pyRLPosture) stamp(props map[string]string) {
	props["rate_limited"] = "true"
	if p.rate != "" {
		props["rate_limit"] = p.rate
	}
	if p.scope != "" {
		props["rate_limit_scope"] = p.scope
	}
	if p.source != "" {
		props["rate_limit_source"] = p.source
	}
	if p.scopeName != "" {
		props["rate_limit_scope_name"] = p.scopeName
	}
}

// pyRLLimiterDecoratorRe captures slowapi / flask-limiter `@limiter.limit("…")`
// (any receiver name ending in a `.limit(` call). Group 1 = receiver, group 2 =
// the rate string.
var pyRLLimiterDecoratorRe = regexp.MustCompile(
	`@\s*([A-Za-z_][\w.]*)\.limit\s*\(\s*["']([^"']+)["']`)

// pyRLRatelimitDecoratorRe captures django-ratelimit `@ratelimit(key='ip',
// rate='10/m')` (also the import-aliased `@<mod>.ratelimit(...)` form). Group 1 =
// the full argument list.
var pyRLRatelimitDecoratorRe = regexp.MustCompile(
	`@\s*(?:[\w.]*\.)?ratelimit\s*\(([^)]*)\)`)

var (
	pyRLRateKwRe = regexp.MustCompile(`\brate\s*=\s*["']([^"']+)["']`)
	pyRLKeyKwRe  = regexp.MustCompile(`\bkey\s*=\s*["']([^"']+)["']`)
)

// pyRLThrottleClassesRe captures DRF class-body `throttle_classes = [Cls, …]`.
// Group 1 = the raw class list.
var pyRLThrottleClassesRe = regexp.MustCompile(
	`throttle_classes\s*=\s*\[([^\]]*)\]`)

// pyRLThrottleScopeRe captures DRF `throttle_scope = 'burst'` (ScopedRateThrottle
// discriminator). Group 1 = the scope literal.
var pyRLThrottleScopeRe = regexp.MustCompile(
	`throttle_scope\s*=\s*["']([^"']+)["']`)

// pyRLDefRe finds a `def <name>(` / `async def <name>(`. Group 1 = the def name.
var pyRLDefRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// pyRLClassRe finds a `class <Name>(<bases>):` or a base-less `class <Name>:`.
// Group 1 = name, group 2 = bases (empty for a base-less class).
var pyRLClassRe = regexp.MustCompile(`(?m)^class\s+([A-Za-z_]\w*)\s*(?:\(([^)]*)\))?\s*:`)

// pyRLDrfBuiltinScope maps the built-in DRF throttle classes to their scope key.
// The rate of these lives in settings, so it is honest-partial (rate omitted)
// unless a co-located subclass declares it.
var pyRLDrfBuiltinScope = map[string]string{
	"UserRateThrottle":   "user",
	"AnonRateThrottle":   "anon",
	"ScopedRateThrottle": "endpoint",
}

// applyPythonRateLimit resolves a per-endpoint rate-limit posture from the
// decorator/view signals in `content` and stamps it onto every synthetic
// http_endpoint_definition in `entities[before:]` that came from `path`.
func applyPythonRateLimit(content, path string, entities []types.EntityRecord, before int) {
	if len(content) == 0 || before >= len(entities) {
		return
	}

	decoByHandler := indexPyHandlerRateLimit(content)
	throttleByClass := indexPyDRFThrottle(content)
	throttleByHandler := pyDRFThrottleByHandler(content, throttleByClass)
	// DRF ViewSets registered on a router bind to the composed `/<prefix>`
	// endpoints (whose source_handler is the route path, not an action method),
	// so resolve the throttle posture by URL prefix too — same binding the
	// middleware pass uses (indexDRFRouterPrefixes).
	throttleByPrefix := pyDRFThrottleByPrefix(content, throttleByClass)
	if len(decoByHandler) == 0 && len(throttleByHandler) == 0 && len(throttleByPrefix) == 0 {
		return
	}

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path || e.Properties == nil {
			continue
		}
		// Don't clobber a posture an upstream pass already resolved (Flask /
		// FastAPI custom extractors stamp rate_limited directly).
		if e.Properties["rate_limited"] == "true" {
			continue
		}
		handler := pythonHandlerName(e.Properties["source_handler"])
		if handler != "" {
			if p, ok := decoByHandler[handler]; ok {
				p.stamp(e.Properties)
				continue
			}
			if p, ok := throttleByHandler[handler]; ok {
				p.stamp(e.Properties)
				continue
			}
		}
		if p, ok := pyDRFPrefixThrottleFor(e.Properties["path"], throttleByPrefix); ok {
			p.stamp(e.Properties)
		}
	}
}

// pyDRFRouterRegisterRe captures a `router.register(r"prefix", ViewSetClass)`
// call. Group 1 = URL prefix, group 2 = ViewSet class name.
var pyDRFRouterRegisterRe = regexp.MustCompile(
	`\.register\s*\(\s*r?["']([^"'\n\r]+)["']\s*,\s*([A-Za-z_]\w*)`)

// pyDRFThrottleByPrefix maps each router-registered URL prefix to the throttle
// posture of the ViewSet class it registers (when that class is same-file). The
// prefix is normalized to a leading-slash, trailing-slash-trimmed form.
func pyDRFThrottleByPrefix(content string, byClass map[string]pyRLPosture) map[string]pyRLPosture {
	out := map[string]pyRLPosture{}
	if len(byClass) == 0 || !strings.Contains(content, ".register(") {
		return out
	}
	for _, m := range pyDRFRouterRegisterRe.FindAllStringSubmatch(content, -1) {
		prefix := "/" + strings.Trim(strings.TrimSpace(m[1]), "/")
		cls := strings.TrimSpace(m[2])
		if p, ok := byClass[cls]; ok {
			out[prefix] = p
		}
	}
	return out
}

// pyDRFPrefixThrottleFor returns the throttle posture bound to the DRF router
// prefix a canonical endpoint path belongs to (equal to, or directly under, the
// prefix regardless of the mount segment) — same match shape as the middleware
// pass's drfPrefixChainFor.
func pyDRFPrefixThrottleFor(canonical string, byPrefix map[string]pyRLPosture) (pyRLPosture, bool) {
	if len(byPrefix) == 0 {
		return pyRLPosture{}, false
	}
	for prefix, p := range byPrefix {
		if prefix == "/" {
			continue
		}
		if canonical == prefix ||
			strings.HasSuffix(canonical, prefix) ||
			strings.Contains(canonical, prefix+"/") {
			return p, true
		}
	}
	return pyRLPosture{}, false
}

// indexPyHandlerRateLimit maps each handler def name (lowercased) to the
// rate-limit posture resolved from the decorators stacked immediately above it.
// Methods defined inside a class are ALSO indexed under `classname.method`
// (lowercased) so a Tornado/CBV class-method endpoint (whose source_handler is
// `MainHandler.get`) binds precisely without bleeding the bare `get` posture
// onto a different handler class's same-named method.
func indexPyHandlerRateLimit(content string) map[string]pyRLPosture {
	out := map[string]pyRLPosture{}
	for _, m := range pyRLDefRe.FindAllStringSubmatchIndex(content, -1) {
		name := strings.ToLower(content[m[2]:m[3]])
		block := pyDecoratorBlockAbove(content, m[0])
		if block == "" {
			continue
		}
		p, ok := resolvePyDecoratorRateLimit(block)
		if !ok {
			continue
		}
		// Top-level function: key on the bare def name.
		if cls := pyEnclosingClass(content, m[0]); cls != "" {
			out[strings.ToLower(cls)+"."+name] = p
		} else {
			out[name] = p
		}
	}
	return out
}

// pyEnclosingClass returns the name of the indentation-enclosing `class` that a
// `def` at byte offset `defStart` belongs to, or "" when the def is top-level.
// A def is a class method when its line is indented and the nearest preceding
// `class <Name>(...):` header sits at a smaller indentation.
func pyEnclosingClass(content string, defStart int) string {
	// pyRLDefRe is `(?m)^\s*...def`, so defStart points at the line start; the
	// indentation is the leading whitespace of the def line itself.
	lineEnd := strings.IndexByte(content[defStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(content) - defStart
	}
	defIndent := pyIndentWidth(content[defStart : defStart+lineEnd])
	if defIndent == 0 {
		return ""
	}
	// Pick the CLOSEST preceding `class` header at a smaller indent.
	var best string
	for _, m := range pyRLClassRe.FindAllStringSubmatchIndex(content[:defStart], -1) {
		clsLineStart := strings.LastIndexByte(content[:m[0]+1], '\n') + 1
		clsIndent := pyIndentWidth(content[clsLineStart:m[0]])
		if clsIndent < defIndent {
			best = content[m[2]:m[3]]
		}
	}
	return best
}

// pyIndentWidth returns the leading-whitespace width of a line prefix (tabs
// counted as one column — sufficient for the relative comparison used here).
func pyIndentWidth(prefix string) int {
	n := 0
	for _, c := range prefix {
		if c == ' ' || c == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// resolvePyDecoratorRateLimit recognises a slowapi/flask-limiter or
// django-ratelimit decorator in a stacked-decorator block.
func resolvePyDecoratorRateLimit(block string) (pyRLPosture, bool) {
	if m := pyRLLimiterDecoratorRe.FindStringSubmatch(block); m != nil {
		return pyRLPosture{
			rate:   strings.TrimSpace(m[2]),
			source: m[1] + ".limit",
		}, true
	}
	if m := pyRLRatelimitDecoratorRe.FindStringSubmatch(block); m != nil {
		p := pyRLPosture{source: "ratelimit"}
		if rm := pyRLRateKwRe.FindStringSubmatch(m[1]); rm != nil {
			p.rate = strings.TrimSpace(rm[1])
		}
		if km := pyRLKeyKwRe.FindStringSubmatch(m[1]); km != nil {
			p.scope = normalizePyRLScope(km[1])
		}
		return p, true
	}
	return pyRLPosture{}, false
}

// normalizePyRLScope maps a django-ratelimit `key=` value to the scope vocabulary.
func normalizePyRLScope(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch {
	case key == "":
		return ""
	case strings.HasPrefix(key, "ip"):
		return "ip"
	case strings.HasPrefix(key, "user"):
		return "user"
	default:
		return "endpoint"
	}
}

// pyDecoratorBlockAbove returns the contiguous block of decorator/comment/blank
// lines immediately preceding the byte offset `defStart` (the start of a `def`
// line), so a stacked limiter decorator above the handler is visible.
func pyDecoratorBlockAbove(content string, defStart int) string {
	start := defStart
	for start > 0 {
		nl := strings.LastIndexByte(content[:start-1], '\n')
		lineStart := nl + 1 // 0 when no earlier newline
		line := strings.TrimSpace(content[lineStart : start-1])
		if line == "" || strings.HasPrefix(line, "@") || strings.HasPrefix(line, "#") {
			start = lineStart
			if lineStart == 0 {
				break
			}
			continue
		}
		break
	}
	if start >= defStart {
		return ""
	}
	return content[start:defStart]
}

// indexPyDRFThrottle maps each DRF view/throttle class name to the posture it
// declares (throttle_classes / throttle_scope), resolving a custom throttle
// subclass's literal rate from elsewhere in the file when possible.
func indexPyDRFThrottle(content string) map[string]pyRLPosture {
	out := map[string]pyRLPosture{}
	if !strings.Contains(content, "throttle_") {
		return out
	}
	for _, m := range pyRLClassRe.FindAllStringSubmatchIndex(content, -1) {
		className := content[m[2]:m[3]]
		body := pyClassBody(content, m[1])

		var p pyRLPosture
		found := false
		if tm := pyRLThrottleClassesRe.FindStringSubmatch(body); tm != nil {
			classes := splitPyClassList(tm[1])
			if len(classes) > 0 {
				p.source = classes[0]
				if scope, ok := pyRLDrfBuiltinScope[classes[0]]; ok {
					p.scope = scope
				} else if scope := pyDRFScopeFromBase(content, classes[0]); scope != "" {
					// Custom throttle subclass: inherit the scope from its
					// built-in base (e.g. DailyThrottle(UserRateThrottle) → user).
					p.scope = scope
				}
				if rate := pyDRFRateForClass(content, classes[0]); rate != "" {
					p.rate = rate
				}
				found = true
			}
		}
		if sm := pyRLThrottleScopeRe.FindStringSubmatch(body); sm != nil {
			p.scopeName = strings.TrimSpace(sm[1])
			if p.scope == "" {
				// ScopedRateThrottle keys on a named scope; rate lives in settings.
				p.scope = "endpoint"
			}
			if p.source == "" {
				p.source = "ScopedRateThrottle"
			}
			found = true
		}
		if found {
			out[className] = p
		}
	}
	return out
}

// pyDRFThrottleByHandler binds each DRF view class's throttle posture to the
// (lowercased) HTTP-method handler names the class defines (`def get`, `def
// post`, `def create`, action methods, …), so a synthesized endpoint whose
// source_handler is one of those inherits the view posture.
func pyDRFThrottleByHandler(content string, byClass map[string]pyRLPosture) map[string]pyRLPosture {
	out := map[string]pyRLPosture{}
	if len(byClass) == 0 {
		return out
	}
	for _, m := range pyRLClassRe.FindAllStringSubmatchIndex(content, -1) {
		className := content[m[2]:m[3]]
		p, ok := byClass[className]
		if !ok {
			continue
		}
		body := pyClassBody(content, m[1])
		clsLower := strings.ToLower(className)
		for _, dm := range pyRLDefRe.FindAllStringSubmatch(body, -1) {
			name := strings.ToLower(dm[1])
			if name == "__init__" {
				continue
			}
			// Class-qualified key (`viewclass.action`) — matches the endpoint's
			// source_handler form and avoids bleeding onto a same-named method
			// of a different view class.
			key := clsLower + "." + name
			if _, exists := out[key]; !exists {
				out[key] = p
			}
		}
	}
	return out
}

// pyDRFRateForClass returns the literal `rate = '…'` declared in the body of
// `class <className>(...)`, or "" when not locally defined / no literal rate.
func pyDRFRateForClass(content, className string) string {
	re := regexp.MustCompile(`(?m)^class\s+` + regexp.QuoteMeta(className) + `\s*\([^)]*\)\s*:`)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return ""
	}
	body := pyClassBody(content, loc[1])
	if rm := pyRLRateKwRe.FindStringSubmatch(body); rm != nil {
		return strings.TrimSpace(rm[1])
	}
	return ""
}

// pyDRFScopeFromBase resolves the scope of a custom throttle subclass from the
// built-in DRF throttle it extends (e.g. `class DailyThrottle(UserRateThrottle)`
// → "user"), or "" when not locally defined / no recognised built-in base.
func pyDRFScopeFromBase(content, className string) string {
	re := regexp.MustCompile(`(?m)^class\s+` + regexp.QuoteMeta(className) + `\s*\(([^)]*)\)\s*:`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	for _, base := range splitPyClassList(m[1]) {
		if dot := strings.LastIndex(base, "."); dot >= 0 {
			base = base[dot+1:]
		}
		if scope, ok := pyRLDrfBuiltinScope[base]; ok {
			return scope
		}
	}
	return ""
}

// pyClassBody returns the indented body that follows the class header ending at
// `headerEnd`, stopping at the next top-level (column-0) `class`/`def`.
func pyClassBody(content string, headerEnd int) string {
	rest := content[headerEnd:]
	if cut := regexp.MustCompile(`\n(?:class|def)\s`).FindStringIndex(rest); cut != nil {
		return rest[:cut[0]]
	}
	return rest
}

// splitPyClassList splits a `[A, B, C]` class list into trimmed symbols.
func splitPyClassList(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
