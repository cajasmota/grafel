// http_endpoint_giraffe.go — F# Giraffe / Saturn route registration → canonical
// http_endpoint_definition synthesis (#4749, the F# slice of the
// coverage-linkage tail epic #4749/#4615; mirrors the Crystal/Kemal slice #4760
// and the Swift/Vapor slice #4755).
//
// Background
// ----------
// The F# base extractor (internal/extractors/fsharp/extractor.go) is a
// regex-based structural extractor: it mines modules / namespaces / let
// bindings / members / types and CALLS edges, but has NO web-framework
// awareness — Giraffe `GET >=> route "/users" >=> handler` combinator chains and
// Saturn `router { get "/users/:id" handler }` blocks are not recognised as HTTP
// endpoints, and no `http_endpoint_definition` entity is ever produced for F#.
// The shared e2e route-test linker (linkE2ERouteTestsToEndpoints, #4351) matches
// a test's route hit against `http_endpoint_definition` + `path`, so an F# route
// could never be hit by a route-string test. As with Crystal (#4760) and
// Swift/Vapor (#4755), the PRODUCER-side gap has to be closed first.
//
// This pass emits one canonical http_endpoint_definition per statically-known
// F# route, in the SAME shape axum / Rocket / Express / Vapor / Kemal emit, so
// the existing resolver and the language-agnostic e2e route-test linker light up
// for F# exactly as they do for the flagship stacks.
//
// F# route syntax
// ---------------
// Giraffe (the dominant F# web framework) composes a route from the verb
// HttpHandler (`GET`/`POST`/…), the `route`/`routef` path combinator, and the
// handler, joined by the Kleisli `>=>` operator, typically inside a
// `choose [ ... ]` list:
//
//	GET  >=> route "/users"      >=> listUsers          → GET /users
//	GET  >=> routef "/users/%i"  getUser                → GET /users/{}  (typed param)
//	POST >=> route "/users"      >=> createUser         → POST /users
//	subRoute "/api" (choose [ GET >=> route "/x" ... ]) → (prefix not folded — see exclusions)
//
// `route` carries a fully static literal path; `routef` carries printf-style
// typed placeholders (`%i`/`%s`/`%O`/…) canonicalised to `{}` by
// FrameworkGiraffe.
//
// Saturn (an opinionated MVC layer over Giraffe) registers routes inside a
// `router { ... }` computation expression with verb operations taking a
// string-literal path and a handler:
//
//	router {
//	  get  "/users"     listUsers
//	  getf "/users/%i"  getUser
//	  post "/users"     createUser
//	}
//
// Saturn paths use the Sinatra/Express-style `:name` colon convention as well as
// the Giraffe `%fmt` form (getf/postf/…); FrameworkGiraffe handles both.
//
// Honest exclusions (no fabricated routes)
// ----------------------------------------
//   - Interpolated / variable paths (`route basePath`, `route (prefix + "/x")`)
//     — not statically recoverable, dropped (only string-literal paths emit).
//   - `subRoute "/api" (...)` / `forward "/api" (...)` mount prefixes are NOT
//     folded into the nested child routes here (the nested routes still emit at
//     their own un-prefixed path). Prefix folding is a documented follow-up; the
//     resolver/segment-matcher tolerates a missing api-version mount prefix on
//     either side, so the un-prefixed definition still binds the common case.
//   - Giraffe `routeCi` / `routeStartsWith` / `routex` (regex) variants beyond
//     `route`/`routef` are a documented follow-up.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// giraffeRouteRe matches a Giraffe `route`/`routef` combinator preceded (on the
// same composition line) by an HTTP-verb HttpHandler:
//
//	GET >=> route "/users"
//	POST >=> routef "/users/%i"
//	GET >=> routeCi "/x"      (routeCi accepted — case-insensitive variant)
//
// Capture group 1 is the verb; group 2 is the path literal. The verb and the
// `route`/`routef` token may be separated by `>=>` and whitespace. Requiring the
// verb on the same line as the path combinator is what distinguishes a real
// route registration from an arbitrary `route` helper call.
var giraffeRouteRe = regexp.MustCompile(
	`(?m)\b(GET|POST|PUT|DELETE|PATCH|OPTIONS|HEAD)\b\s*>=>\s*` +
		`route[fxi]?(?:Ci)?\s+"([^"\n\r]*)"`,
)

// saturnRouteRe matches a Saturn `router { ... }` verb operation taking a
// leading string-literal path:
//
//	get  "/users"     listUsers
//	getf "/users/%i"  getUser
//	post "/users"     createUser
//	delete "/users/:id" deleteUser
//
// Anchored at a statement boundary (`^[ \t]*`) so an arbitrary `obj.get "..."`
// is not captured. The optional trailing `f` (`getf`/`postf`/…) is the
// format-string variant. Capture group 1 is the verb; group 2 is the path.
var saturnRouteRe = regexp.MustCompile(
	`(?m)^[ \t]*(get|post|put|delete|patch|options|head)f?\s+"([^"\n\r]*)"`,
)

// giraffeHasRoute is a fast pre-filter: the file must reference an F# web marker
// (Giraffe / Saturn) AND a route token to be worth scanning, so we never misfire
// on arbitrary F# code.
func giraffeHasRoute(content string) bool {
	hasMarker := strings.Contains(content, "Giraffe") ||
		strings.Contains(content, "giraffe") ||
		strings.Contains(content, "Saturn") ||
		strings.Contains(content, "saturn") ||
		strings.Contains(content, ">=>") ||
		strings.Contains(content, "router {") ||
		strings.Contains(content, "HttpHandler") ||
		strings.Contains(content, "choose [")
	if !hasMarker {
		return false
	}
	return strings.Contains(content, "route") ||
		strings.Contains(content, "router {")
}

// synthesizeGiraffeRoutes scans an F# source file for Giraffe / Saturn route
// registrations and emits one http_endpoint_definition per statically-known
// (verb, path).
func synthesizeGiraffeRoutes(content string, emit emitFn) {
	if !giraffeHasRoute(content) {
		return
	}
	seen := map[string]bool{}
	emitRoute := func(verbRaw, rawPath string) {
		verb := strings.ToUpper(verbRaw)
		raw := strings.TrimSpace(rawPath)
		// Drop interpolated / empty literals — not statically recoverable.
		// F# interpolated strings use `$"..{x}.."`; a `{` in the literal that
		// is not a `%fmt`-derived token signals interpolation, so drop it.
		if raw == "" || strings.Contains(raw, "{") {
			return
		}
		if !strings.HasPrefix(raw, "/") {
			raw = "/" + raw
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkGiraffe, raw)
		if canonical == "" || canonical == "/" {
			return
		}
		key := verb + "\x00" + canonical
		if seen[key] {
			return
		}
		seen[key] = true
		// Handler kind "Controller" maps to SCOPE.Operation in the resolver, the
		// kind F# `let` handlers land as — matching the axum/vapor/kemal
		// convention so ResolveHTTPEndpointHandlers can bind a handler IMPLEMENTS
		// edge when a same-named handler exists (no name forces a handler when
		// absent).
		emit(verb, canonical, "giraffe", "Controller", "")
	}

	for _, m := range giraffeRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		emitRoute(m[1], m[2])
	}
	for _, m := range saturnRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		emitRoute(m[1], m[2])
	}
}
