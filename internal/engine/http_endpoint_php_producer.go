// http_endpoint_php_producer.go — Laravel route → http_endpoint_definition synthesis.
//
// Covers:
//   - Route::get/post/put/patch/delete/options/any('/path', ...)
//   - Route::resource('name', Controller::class) → 7 standard CRUD endpoints
//     with exact Controller@action attribution (index/store/create/show/edit/update/destroy)
//   - Route::apiResource('name', Controller::class) → 5 API CRUD endpoints
//     with exact Controller@action attribution (excludes /create and /{id}/edit)
//   - Route::group(['prefix'=>'admin', 'middleware'=>['auth']], fn) → prefix-prefixed routes
//     and nested group support
//   - Route::controller(X::class)->group(fn) → controller-scoped groups
//   - Invokable single-action controllers: Route::get('/path', InvokableCtrl::class)
//   - Route model binding: {photo} — handled by FrameworkExpress canonicalization
//   - ->name('x') and ->middleware() chaining — decorative, does not affect synthesis
//
// Handler extraction:
//   - Array syntax:  [Controller::class, 'method']   → "Controller@method"
//   - String syntax: 'ControllerName@method'         → "ControllerName@method"
//   - Invokable:     Controller::class (bare)        → "Controller@__invoke"
//   - Closure:       function($request){...}         → "" (no static handler ref)
//
// The canonical path uses httproutes.FrameworkExpress (Express-style {param})
// because Laravel path parameters use the {param} syntax natively.
//
// Refs #1419, #3393.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Compiled regexps
// ---------------------------------------------------------------------------

// lrRouteVerbRe matches:
//
//	Route::get('/path', [Controller::class, 'method'])
//	Route::post('/path', 'ControllerName@method')
//	Route::get('/path', function() { ... })
//	Route::get('/path', InvokableController::class)
//
// Capture groups: 1=verb, 2=path (double-quoted), 3=path (single-quoted),
// 4=controller class (array form), 5=method name (array form),
// 6=controller@method string (single-quoted), 7=controller@method string (double-quoted),
// 8=invokable class (bare ClassName::class form).
var lrRouteVerbRe = regexp.MustCompile(
	`(?m)Route::(get|post|put|patch|delete|options|any)\s*\(\s*` +
		`(?:"([^"]{1,500})"|'([^']{1,500})')` + // path: groups 2, 3
		`\s*,\s*` +
		`(?:` +
		`\[\s*([\w\\]+)::class\s*,\s*'(\w+)'\s*\]` + // array form: groups 4, 5
		`|` +
		`'([\w\\@]+)'` + // string @method form (single-quoted): group 6
		`|` +
		`"([\w\\@]+)"` + // double-quoted @method form: group 7
		`|` +
		`([\w\\]+)::class` + // invokable: group 8 — bare ClassName::class
		`)?`,
)

// laravelVerbRouteRe is the alias kept for backward-compatibility with
// existing tests in this package that reference it by the old name.
var laravelVerbRouteRe = lrRouteVerbRe

// lrRouteResourceRe matches:
//
//	Route::resource('name', Controller::class)
//	Route::resource('name', 'ControllerName')
//
// Capture groups: 1=resource name, 2=controller class (optional).
var lrRouteResourceRe = regexp.MustCompile(
	`(?m)Route::resource\s*\(\s*['"]([^'"]{1,200})['"]` +
		`(?:\s*,\s*(?:([\w\\]+)::class|'([\w\\]+)'|"([\w\\]+)"))?`,
)

// laravelResourceRe is the alias kept for backward-compatibility.
var laravelResourceRe = regexp.MustCompile(
	`(?m)Route::resource\s*\(\s*['"]([^'"]{1,200})['"]`,
)

// lrRouteApiResourceRe matches:
//
//	Route::apiResource('name', Controller::class)
//
// Capture groups: 1=resource name, 2=controller class (optional).
var lrRouteApiResourceRe = regexp.MustCompile(
	`(?m)Route::apiResource\s*\(\s*['"]([^'"]{1,200})['"]` +
		`(?:\s*,\s*(?:([\w\\]+)::class|'([\w\\]+)'|"([\w\\]+)"))?`,
)

// laravelApiResourceRe is the alias kept for backward-compatibility.
var laravelApiResourceRe = regexp.MustCompile(
	`(?m)Route::apiResource\s*\(\s*['"]([^'"]{1,200})['"]`,
)

// lrRouteGroupPrefixRe matches a Route::group or Route::prefix call that
// carries a 'prefix' key:
//
//	Route::group(['prefix' => 'admin', ...], function() { ... })
//	Route::group(['prefix' => 'api/v1', 'middleware' => ['auth']], ...)
//
// Capture groups: 1=prefix value.
var lrRouteGroupPrefixRe = regexp.MustCompile(
	`(?m)Route::(?:group|prefix)\s*\(\s*\[` +
		`[^\]]*['"]prefix['"]\s*=>\s*['"]([^'"]{1,200})['"]`,
)

// lrRouteGroupMwRe matches the middleware key inside a group options array,
// used to extract group-level middleware annotation.
//
//	Route::group(['middleware' => 'auth', ...], ...)
//	Route::group(['middleware' => ['auth', 'throttle:60'], ...], ...)
//
// Capture groups: 1=middleware value (string or first element).
var lrRouteGroupMwRe = regexp.MustCompile(
	`(?m)Route::(?:group|prefix)\s*\(\s*\[` +
		`[^\]]*['"]middleware['"]\s*=>\s*(?:'([^']{1,200})'|"([^"]{1,200})"|\[['"]([^'"]{1,200})['"])`,
)

// lrRouteControllerGroupRe matches:
//
//	Route::controller(ControllerClass::class)->group(function() { ... })
//
// Capture groups: 1=controller class name.
var lrRouteControllerGroupRe = regexp.MustCompile(
	`(?m)Route::controller\s*\(\s*([\w\\]+)::class\s*\)\s*->\s*group\s*\(`,
)

// ---------------------------------------------------------------------------
// CRUD route tables with action attribution
// ---------------------------------------------------------------------------

// lrResourceRoute describes one of the 7 standard routes from Route::resource.
type lrResourceRoute struct {
	method, suffix, action string
}

// lrResourceRoutes are the 7 standard routes emitted by Route::resource.
var lrResourceRoutes = []lrResourceRoute{
	{"GET", "", "index"},
	{"POST", "", "store"},
	{"GET", "/create", "create"},
	{"GET", "/{id}", "show"},
	{"GET", "/{id}/edit", "edit"},
	{"PUT", "/{id}", "update"},
	{"DELETE", "/{id}", "destroy"},
}

// lrApiResourceRoutes are the 5 routes emitted by Route::apiResource
// (excludes /create and /{id}/edit — no form views in API mode).
var lrApiResourceRoutes = []lrResourceRoute{
	{"GET", "", "index"},
	{"POST", "", "store"},
	{"GET", "/{id}", "show"},
	{"PUT", "/{id}", "update"},
	{"DELETE", "/{id}", "destroy"},
}

// laravelResourceRoutes is the alias kept for backward-compatibility
// with existing tests.
var laravelResourceRoutes = []struct{ method, suffix string }{
	{"GET", ""},
	{"POST", ""},
	{"GET", "/create"},
	{"GET", "/{id}"},
	{"GET", "/{id}/edit"},
	{"PUT", "/{id}"},
	{"DELETE", "/{id}"},
}

// laravelApiResourceRoutes is the alias kept for backward-compatibility.
var laravelApiResourceRoutes = []struct{ method, suffix string }{
	{"GET", ""},
	{"POST", ""},
	{"GET", "/{id}"},
	{"PUT", "/{id}"},
	{"DELETE", "/{id}"},
}

// ---------------------------------------------------------------------------
// Fast-path gate
// ---------------------------------------------------------------------------

func phpHasAnyLaravelRoute(content string) bool {
	return strings.Contains(content, "Route::get") ||
		strings.Contains(content, "Route::post") ||
		strings.Contains(content, "Route::put") ||
		strings.Contains(content, "Route::patch") ||
		strings.Contains(content, "Route::delete") ||
		strings.Contains(content, "Route::options") ||
		strings.Contains(content, "Route::any") ||
		strings.Contains(content, "Route::resource") ||
		strings.Contains(content, "Route::apiResource") ||
		strings.Contains(content, "Route::group") ||
		strings.Contains(content, "Route::controller") ||
		strings.Contains(content, "Route::prefix")
}

// ---------------------------------------------------------------------------
// Handler extraction helpers
// ---------------------------------------------------------------------------

// lrRouteHandlerFromMatch returns a "Controller@method" string from a regex
// match of lrRouteVerbRe. Returns "" when the handler is a closure or
// cannot be statically determined.
//
// lrRouteVerbRe capture groups (indices into FindAllStringSubmatchIndex output):
//
//	m[0..1]   = full match
//	m[2..3]   = group 1 (verb)
//	m[4..5]   = group 2 (path double-quoted)
//	m[6..7]   = group 3 (path single-quoted)
//	m[8..9]   = group 4 (controller class, array form)
//	m[10..11] = group 5 (method name, array form)
//	m[12..13] = group 6 (string @method, single-quoted)
//	m[14..15] = group 7 (string @method, double-quoted)
//	m[16..17] = group 8 (invokable ClassName::class)
func lrRouteHandlerFromMatch(src string, m []int) string {
	// Array form: [ControllerClass::class, 'method'] — groups 4+5.
	if len(m) >= 12 && m[8] >= 0 && m[10] >= 0 {
		cls := src[m[8]:m[9]]
		if i := strings.LastIndex(cls, "\\"); i >= 0 {
			cls = cls[i+1:]
		}
		method := src[m[10]:m[11]]
		return cls + "@" + method
	}
	// String form (single-quoted): 'Controller@method' — group 6.
	if len(m) >= 14 && m[12] >= 0 {
		return src[m[12]:m[13]]
	}
	// Double-quoted string form — group 7.
	if len(m) >= 16 && m[14] >= 0 {
		return src[m[14]:m[15]]
	}
	// Invokable controller: bare ClassName::class — group 8.
	if len(m) >= 18 && m[16] >= 0 {
		cls := src[m[16]:m[17]]
		if i := strings.LastIndex(cls, "\\"); i >= 0 {
			cls = cls[i+1:]
		}
		return cls + "@__invoke"
	}
	return ""
}

// laravelHandlerFromMatch is the backward-compatible alias.
func laravelHandlerFromMatch(src string, m []int) string {
	return lrRouteHandlerFromMatch(src, m)
}

// lrRouteHandlerToScopeOp converts the parsed "Controller@method" form
// produced by lrRouteHandlerFromMatch into the (kind, name) pair that
// matches the PHP extractor's SCOPE.Operation naming convention
// ("Controller.method"). Returns ("","") when the input is empty or not
// in @-method form (e.g. closure handlers).
//
// Refs #2678 — fix Laravel endpoint attribution.
func lrRouteHandlerToScopeOp(ref string) (kind, name string) {
	if ref == "" {
		return "", ""
	}
	at := strings.IndexByte(ref, '@')
	if at <= 0 || at == len(ref)-1 {
		return "", ""
	}
	cls := ref[:at]
	method := ref[at+1:]
	if i := strings.LastIndex(cls, "\\"); i >= 0 {
		cls = cls[i+1:]
	}
	return "SCOPE.Operation", cls + "." + method
}

// laravelHandlerToScopeOp is the backward-compatible alias.
func laravelHandlerToScopeOp(ref string) (kind, name string) {
	return lrRouteHandlerToScopeOp(ref)
}

// ---------------------------------------------------------------------------
// Group prefix extraction helpers
// ---------------------------------------------------------------------------

// lrGroupSpan describes one Route::group (or Route::prefix) call found in
// a PHP source file, carrying the prefix and the byte span of its body.
type lrGroupSpan struct {
	prefix    string // from 'prefix' => '...' in the array
	bodyStart int    // offset of the first byte AFTER the opening brace of the callback body
	bodyEnd   int    // offset of the closing brace of the callback body
}

// lrExtractGroupSpans scans src for Route::group calls that carry a 'prefix'
// key and returns their (prefix, body-span) descriptors. The body is located
// by finding the opening `{` of the callback function/arrow-fn and then
// balance-walking to its matching `}`.
//
// This is intentionally conservative: it only handles the most common
// patterns (`function() { ... }` and arrow-fn `fn() => ...`) and skips any
// group whose body span cannot be reliably determined.
func lrExtractGroupSpans(src string) []lrGroupSpan {
	var spans []lrGroupSpan
	for _, m := range lrRouteGroupPrefixRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		prefix := src[m[2]:m[3]]
		// Locate the callback body: scan forward from the end of the match for `{`.
		start := m[1]
		bodyOpen := -1
		for i := start; i < len(src) && i < start+1000; i++ {
			if src[i] == '{' {
				bodyOpen = i
				break
			}
		}
		if bodyOpen < 0 {
			continue
		}
		// Balance-walk to find the matching `}`.
		bodyEnd := lrFindMatchingBrace(src, bodyOpen)
		if bodyEnd < 0 {
			continue
		}
		spans = append(spans, lrGroupSpan{
			prefix:    prefix,
			bodyStart: bodyOpen + 1, // content starts after `{`
			bodyEnd:   bodyEnd,      // content ends before `}`
		})
	}
	return spans
}

// lrExtractControllerGroupSpans scans src for
// Route::controller(Ctrl::class)->group(...) calls and returns descriptors
// keyed by the controller class name (bare, without namespace).
type lrControllerGroupSpan struct {
	controller string
	bodyStart  int
	bodyEnd    int
}

func lrExtractControllerGroupSpans(src string) []lrControllerGroupSpan {
	var spans []lrControllerGroupSpan
	for _, m := range lrRouteControllerGroupRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		cls := src[m[2]:m[3]]
		if i := strings.LastIndex(cls, "\\"); i >= 0 {
			cls = cls[i+1:]
		}
		start := m[1]
		bodyOpen := -1
		for i := start; i < len(src) && i < start+500; i++ {
			if src[i] == '{' {
				bodyOpen = i
				break
			}
		}
		if bodyOpen < 0 {
			continue
		}
		bodyEnd := lrFindMatchingBrace(src, bodyOpen)
		if bodyEnd < 0 {
			continue
		}
		spans = append(spans, lrControllerGroupSpan{
			controller: cls,
			bodyStart:  bodyOpen + 1,
			bodyEnd:    bodyEnd,
		})
	}
	return spans
}

// lrFindMatchingBrace returns the index of the `}` that closes the `{` at
// position open in src. Returns -1 if not found or if open does not point
// to a `{`. Handles nested braces and skips PHP single-quoted strings.
func lrFindMatchingBrace(src string, open int) int {
	if open < 0 || open >= len(src) || src[open] != '{' {
		return -1
	}
	depth := 1
	i := open + 1
	for i < len(src) && depth > 0 {
		c := src[i]
		switch c {
		case '{':
			depth++
			i++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
			i++
		case '\'':
			// Skip single-quoted PHP string.
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == '\'' {
					i++
					break
				}
				i++
			}
		case '"':
			// Skip double-quoted string.
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == '"' {
					i++
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return -1
}

// lrResourceControllerFromMatch extracts the bare controller class name from a
// match of lrRouteResourceRe (or lrRouteApiResourceRe).
//
//	m[4..5]  = group 2 — ClassName::class form (bare class)
//	m[6..7]  = group 3 — single-quoted string
//	m[8..9]  = group 4 — double-quoted string
func lrResourceControllerFromMatch(src string, m []int) string {
	for _, pair := range [][2]int{{4, 5}, {6, 7}, {8, 9}} {
		lo, hi := pair[0], pair[1]
		if lo < len(m) && m[lo] >= 0 {
			cls := src[m[lo]:m[hi]]
			if i := strings.LastIndex(cls, "\\"); i >= 0 {
				cls = cls[i+1:]
			}
			return cls
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// synthesizeLaravel scans a PHP source file for Laravel route registrations
// and calls emit for each (verb, canonical-path, framework, handlerKind, handlerName)
// tuple discovered. It is the producer-side counterpart to synthesizePHPClient.
// emitResourceFn is the closure the resourceful-route synthesizers use to emit a
// framework-synthesized route AND stamp its provenance + per-verb effective
// contract (T10 #3842). action is the framework's canonical action name
// (index/store/show/update/destroy/...) the routes catalog keys the contract on.
type emitResourceFn func(method, canonicalPath, framework, refKind, refName, handlerFile, action string)

func synthesizeLaravel(content string, emit emitFn, emitResource emitResourceFn) {
	if !phpHasAnyLaravelRoute(content) {
		return
	}

	// Build group-prefix spans so inner routes can be prefixed.
	groupSpans := lrExtractGroupSpans(content)
	// Build controller-group spans for Route::controller(X)->group(...).
	ctrlSpans := lrExtractControllerGroupSpans(content)

	// lrPrefixFor returns the accumulated prefix for a route at byte offset pos.
	// Group spans are in source order (outermost group is found first because it
	// appears earlier in the file). We append each matching prefix left-to-right
	// so outer prefixes come before inner ones in the final path.
	lrPrefixFor := func(pos int) string {
		prefix := ""
		for _, gs := range groupSpans {
			if pos >= gs.bodyStart && pos < gs.bodyEnd {
				prefix = prefix + "/" + strings.Trim(gs.prefix, "/")
			}
		}
		return prefix
	}

	// lrControllerFor returns the bare class name of any Route::controller group
	// that contains byte offset pos, or "" if none.
	lrControllerFor := func(pos int) string {
		for _, cs := range ctrlSpans {
			if pos >= cs.bodyStart && pos < cs.bodyEnd {
				return cs.controller
			}
		}
		return ""
	}

	// --- Explicit verb routes: Route::get/post/... ---
	for _, m := range lrRouteVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])

		// Extract raw path from double-quoted (group 2) or single-quoted (group 3).
		raw := ""
		if m[4] >= 0 {
			raw = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			raw = content[m[6]:m[7]]
		}
		if raw == "" {
			continue
		}

		// Apply any group prefix.
		routePos := m[0]
		prefix := lrPrefixFor(routePos)
		if prefix != "" {
			raw = prefix + "/" + strings.TrimLeft(raw, "/")
		}

		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)

		// Resolve handler: explicit match takes priority; fall back to
		// Route::controller group if no handler was captured.
		ref := lrRouteHandlerFromMatch(content, m)
		if ref == "" {
			// Try Route::controller(X)->group scope.
			if ctrl := lrControllerFor(routePos); ctrl != "" {
				// Route::controller + explicit verb routes imply __invoke
				// only when no method is given; the more common pattern is
				// that each verb route inside the group specifies its own
				// method (e.g. Route::get('/path', 'method')). We only stamp
				// the controller when the handler was truly empty (closure/none).
				_ = ctrl // attribution without method is ambiguous; leave empty
			}
		}

		refKind, refName := lrRouteHandlerToScopeOp(ref)

		// If inside a Route::controller group and no explicit handler was given,
		// we cannot infer the method, so we leave it blank (closure path).
		emit(verb, canonical, "laravel", refKind, refName)
	}

	// --- Route::resource → 7 CRUD endpoints with controller attribution ---
	for _, m := range lrRouteResourceRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		routePos := m[0]
		prefix := lrPrefixFor(routePos)
		base := prefix + "/" + strings.Trim(name, "/")
		ctrl := lrResourceControllerFromMatch(content, m)
		for _, r := range lrResourceRoutes {
			path := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			hKind, hName := "", ""
			if ctrl != "" {
				hKind, hName = "SCOPE.Operation", ctrl+"@"+r.action
			}
			emitResource(r.method, canonical, "laravel_resource", hKind, hName, "", r.action)
		}
	}

	// --- Route::apiResource → 5 API CRUD endpoints with controller attribution ---
	for _, m := range lrRouteApiResourceRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		routePos := m[0]
		prefix := lrPrefixFor(routePos)
		base := prefix + "/" + strings.Trim(name, "/")
		ctrl := lrResourceControllerFromMatch(content, m)
		for _, r := range lrApiResourceRoutes {
			path := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			hKind, hName := "", ""
			if ctrl != "" {
				hKind, hName = "SCOPE.Operation", ctrl+"@"+r.action
			}
			emitResource(r.method, canonical, "laravel_api_resource", hKind, hName, "", r.action)
		}
	}
}

// ---------------------------------------------------------------------------
// Slim Framework → http_endpoint_definition synthesis (#5733, refs #5688).
// ---------------------------------------------------------------------------
//
// Non-Laravel PHP frameworks (Slim, CakePHP, CodeIgniter, Yii, WordPress, …)
// are recognized by the regex-based custom extractors in
// internal/custom/php/frameworks.go, but those extractors only ever emit a
// statement-level SCOPE.Operation(subtype=endpoint) — never the flagship
// http_endpoint_definition shape synthesizeLaravel produces. That leaves
// their Routing cells at definitions:0 in the graph. This section adds the
// first of those (Slim); the shape below is intentionally the smallest unit
// of a small per-framework table so CakePHP/CodeIgniter/Yii/WordPress/etc.
// can each be added later as one more entry, not a bespoke synthesizer.
//
// slimRouteVerbRe matches:
//
//	$app->get('/users/:id', function ($request, $response, $args) { ... });
//	$app->post("/users", 'UserAction');
//
// Capture groups: 1=verb, 2=path (double-quoted), 3=path (single-quoted).
// Slim v3/v4 route methods are get/post/put/patch/delete/options/any — the
// same verb set Laravel's Route:: facade exposes, minus `map`/`group` (which
// are route-REGISTRATION helpers, not verbs, and are intentionally excluded
// here — group-prefix composition is a follow-up, see phpHasAnySlimRoute).
var slimRouteVerbRe = regexp.MustCompile(
	`(?m)\$app->(get|post|put|patch|delete|options|any)\s*\(\s*` +
		`(?:"([^"]{1,500})"|'([^']{1,500})')`,
)

// phpHasAnySlimRoute is the fast-path gate mirroring phpHasAnyLaravelRoute:
// no-op on any PHP file that isn't wiring up Slim routes.
func phpHasAnySlimRoute(content string) bool {
	return strings.Contains(content, "$app->get(") ||
		strings.Contains(content, "$app->post(") ||
		strings.Contains(content, "$app->put(") ||
		strings.Contains(content, "$app->patch(") ||
		strings.Contains(content, "$app->delete(") ||
		strings.Contains(content, "$app->options(") ||
		strings.Contains(content, "$app->any(")
}

// synthesizeSlim scans a PHP source file for Slim route registrations
// ($app->get/post/put/patch/delete/options/any('/path', handler)) and emits
// one http_endpoint_definition per route via emit, using the SAME canonical
// shape synthesizeLaravel produces so the Routing cells, request/response
// shape substrate, and auth/rate-limit stamping passes all reach parity.
//
// Slim's path-parameter syntax is `:name` — identical to Express — so the
// path is canonicalized via httproutes.FrameworkExpress (colon-param walker)
// rather than needing a bespoke Slim canonicalizer.
//
// The handler (closure, invokable class, or 'Controller::method' array/string
// form) is intentionally NOT attributed here: Slim's action classes are
// resolved via a DI container, so — unlike Laravel's static Controller@method
// convention — there is no reliable static handler reference to forward.
// This mirrors the closure-handler case in synthesizeLaravel (empty
// handlerKind/handlerName).
func synthesizeSlim(content string, emit emitFn) {
	if !phpHasAnySlimRoute(content) {
		return
	}
	for _, m := range slimRouteVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])

		raw := ""
		if m[4] >= 0 {
			raw = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			raw = content[m[6]:m[7]]
		}
		if raw == "" {
			continue
		}

		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		emit(verb, canonical, "slim", "", "")
	}
}
