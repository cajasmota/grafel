// http_endpoint_php_producer.go — Laravel route → http_endpoint_definition synthesis.
//
// Covers:
//   - Route::get/post/put/patch/delete/options/any('/path', ...)
//   - Route::resource('name', Controller::class) → 7 standard CRUD endpoints
//   - Route::apiResource('name', Controller::class) → 5 API CRUD endpoints
//     (excludes the two browser form routes /create and /{id}/edit)
//
// Handler extraction:
//   - Array syntax:  [Controller::class, 'method']   → "Controller@method"
//   - String syntax: 'ControllerName@method'         → "ControllerName@method"
//   - Closure:       function($request){...}         → "" (no static handler ref)
//
// The canonical path uses httproutes.FrameworkExpress (Express-style {param})
// because Laravel path parameters use the {param} syntax natively.
//
// Refs #1419.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Compiled regexps
// ---------------------------------------------------------------------------

// laravelVerbRouteRe matches:
//
//	Route::get('/path', [Controller::class, 'method'])
//	Route::post('/path', 'ControllerName@method')
//	Route::get('/path', function() { ... })
//
// Capture groups: 1=verb, 2=path (double-quoted), 3=path (single-quoted),
// 4=controller class (array form), 5=method name (array form),
// 6=controller@method string (single-quoted), 7=controller@method string (double-quoted).
var laravelVerbRouteRe = regexp.MustCompile(
	`(?m)Route::(get|post|put|patch|delete|options|any)\s*\(\s*` +
		`(?:"([^"]{1,500})"|'([^']{1,500})')` + // path: groups 2, 3
		`\s*,\s*` +
		`(?:` +
		`\[\s*([\w\\]+)::class\s*,\s*'(\w+)'\s*\]` + // array form: groups 4, 5
		`|` +
		`'([\w\\@]+)'` + // string @method form (single-quoted): group 6
		`|` +
		`"([\w\\@]+)"` + // double-quoted @method form: group 7
		`)?`,
)

// laravelResourceRe matches:
//
//	Route::resource('name', Controller::class)
//	Route::resource('name', 'ControllerName')
//
// Capture groups: 1=resource name.
var laravelResourceRe = regexp.MustCompile(
	`(?m)Route::resource\s*\(\s*['"]([^'"]{1,200})['"]`,
)

// laravelApiResourceRe matches:
//
//	Route::apiResource('name', Controller::class)
//
// Capture groups: 1=resource name.
var laravelApiResourceRe = regexp.MustCompile(
	`(?m)Route::apiResource\s*\(\s*['"]([^'"]{1,200})['"]`,
)

// ---------------------------------------------------------------------------
// CRUD route tables
// ---------------------------------------------------------------------------

// laravelResourceRoutes are the 7 standard routes emitted by Route::resource.
var laravelResourceRoutes = []struct{ method, suffix string }{
	{"GET", ""},           // index
	{"POST", ""},          // store
	{"GET", "/create"},    // create (form)
	{"GET", "/{id}"},      // show
	{"GET", "/{id}/edit"}, // edit (form)
	{"PUT", "/{id}"},      // update
	{"DELETE", "/{id}"},   // destroy
}

// laravelApiResourceRoutes are the 5 routes emitted by Route::apiResource
// (excludes /create and /{id}/edit — no form views in API mode).
var laravelApiResourceRoutes = []struct{ method, suffix string }{
	{"GET", ""},         // index
	{"POST", ""},        // store
	{"GET", "/{id}"},    // show
	{"PUT", "/{id}"},    // update
	{"DELETE", "/{id}"}, // destroy
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
		strings.Contains(content, "Route::apiResource")
}

// ---------------------------------------------------------------------------
// Handler extraction helpers
// ---------------------------------------------------------------------------

// laravelHandlerFromMatch returns a "Controller@method" string from a regex
// match of laravelVerbRouteRe. Returns "" when the handler is a closure or
// cannot be statically determined.
//
// laravelVerbRouteRe capture groups (indices into FindAllStringSubmatchIndex output):
//
//	m[0..1]   = full match
//	m[2..3]   = group 1 (verb)
//	m[4..5]   = group 2 (path double-quoted)
//	m[6..7]   = group 3 (path single-quoted)
//	m[8..9]   = group 4 (controller class, array form)
//	m[10..11] = group 5 (method name, array form)
//	m[12..13] = group 6 (string @method, single-quoted)
//	m[14..15] = group 7 (string @method, double-quoted)
func laravelHandlerFromMatch(src string, m []int) string {
	// Array form: [ControllerClass::class, 'method'] — groups 4+5.
	if len(m) >= 12 && m[8] >= 0 && m[10] >= 0 {
		cls := src[m[8]:m[9]]
		// Strip leading namespace backslashes to get bare class name.
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
	return ""
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// synthesizeLaravel scans a PHP source file for Laravel route registrations
// and calls emit for each (verb, canonical-path, framework, handlerKind, handlerName)
// tuple discovered. It is the producer-side counterpart to synthesizePHPClient.
func synthesizeLaravel(content string, emit emitFn) {
	if !phpHasAnyLaravelRoute(content) {
		return
	}

	// --- Explicit verb routes: Route::get/post/... ---
	for _, m := range laravelVerbRouteRe.FindAllStringSubmatchIndex(content, -1) {
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

		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		// Laravel routes are always in a separate file from their controller
		// classes (PSR-4 autoloading). Emitting the controller reference as
		// source_handler causes the resolve pass to drop the synthetic when
		// the controller entity is not in the same file (which is the normal
		// case for routes/*.php files). We pass empty handler so the
		// synthetic gets NoHandlerProp treatment (kept unconditionally) and
		// survives as an http_endpoint_definition for cross-repo linking.
		// The controller reference is captured by laravelHandlerFromMatch but
		// intentionally not forwarded here — it can be added as a plain
		// property in a follow-up when the cross-file resolver supports it.
		emit(verb, canonical, "laravel", "", "")
	}

	// --- Route::resource → 7 CRUD endpoints ---
	for _, m := range laravelResourceRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		base := "/" + strings.Trim(name, "/")
		for _, r := range laravelResourceRoutes {
			path := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emit(r.method, canonical, "laravel_resource", "", "")
		}
	}

	// --- Route::apiResource → 5 API CRUD endpoints ---
	for _, m := range laravelApiResourceRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		base := "/" + strings.Trim(name, "/")
		for _, r := range laravelApiResourceRoutes {
			path := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emit(r.method, canonical, "laravel_api_resource", "", "")
		}
	}
}
