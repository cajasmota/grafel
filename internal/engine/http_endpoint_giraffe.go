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
// subRoute / forward prefix folding (#4940)
// -----------------------------------------
// Giraffe mounts a sub-app under a path prefix with `subRoute "/api" (...)` or
// `forward "/api" (...)`. The nested child routes are written un-prefixed and
// the real served path is the concatenation of every enclosing mount prefix and
// the child's own path:
//
//	subRoute "/api" (choose [ GET >=> route "/users" >=> listUsers ])
//	                                          → GET /api/users   (folded, #4940)
//
// Nesting composes left-to-right (`subRoute "/api" (subRoute "/v1" (...))` →
// `/api/v1/...`). This pass tracks the parenthesised span opened by each
// subRoute/forward mount and folds the accumulated prefix into every route that
// falls inside it. Only string-literal mount prefixes fold; an interpolated /
// variable mount prefix is dropped (the child still emits un-prefixed).
//
// routex / routeStartsWith (#4940)
// --------------------------------
// `routeStartsWith "/api"` registers a prefix match — folded as a literal path
// (the resolver's segment matcher tolerates the prefix semantics). `routex` is a
// regex route (`routex "/users/(\d+)"`); the regex body is canonicalised to the
// positional `{}` wildcard per capture group so a routable path still emits,
// rather than dropping the endpoint entirely.
//
// named-handler IMPLEMENTS (#4940)
// --------------------------------
// When a Giraffe/Saturn route names a same-file `let`-bound HttpHandler as its
// handler (`route "/users" >=> listUsers`, `get "/users" listUsers`), the
// handler symbol is captured and passed as the synthesizer's refName so the
// shared synthesis-time structural bridge (#4319) emits an endpoint→handler
// IMPLEMENTS edge. A composed/anonymous handler (a `>=>` chain, a lambda, a
// dotted/qualified name) yields no name — the endpoint still emits, just without
// the named bridge.
//
// Honest exclusions (no fabricated routes)
// ----------------------------------------
//   - Interpolated / variable paths (`route basePath`, `route (prefix + "/x")`)
//     — not statically recoverable, dropped (only string-literal paths emit).
//   - An interpolated / variable subRoute/forward mount prefix does not fold
//     (the nested child still emits at its own un-prefixed path).
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

// giraffeRouteRe matches a Giraffe route combinator preceded (on the same
// composition line) by an HTTP-verb HttpHandler, and OPTIONALLY captures the
// trailing handler symbol:
//
//	GET >=> route "/users"                → verb GET, path /users
//	POST >=> routef "/users/%i" getUser   → verb POST, path /users/%i, handler getUser
//	GET >=> routeCi "/x"                   (routeCi accepted — case-insensitive)
//	GET >=> routeStartsWith "/api"        (#4940 prefix match — accepted)
//	GET >=> routex "/users/(\d+)"         (#4940 regex route — body → {})
//
// Capture group 1 is the verb; group 2 the route token suffix (`f`/`x`/`Ci`/
// `StartsWith`/empty); group 3 the path literal; group 4 (optional) the trailing
// handler symbol, present when the route is followed by `>=> <bareName>` with no
// further composition. A composed handler (another `>=>` chain) or a lambda is
// left uncaptured (group 4 empty), keeping the named-handler bridge honest.
var giraffeRouteRe = regexp.MustCompile(
	`(?m)\b(GET|POST|PUT|DELETE|PATCH|OPTIONS|HEAD)\b\s*>=>\s*` +
		`route(f|x|Ci|StartsWith)?\s+"([^"\n\r]*)"` +
		`(?:\s*>=>\s*([A-Za-z_][A-Za-z0-9_']*)\s*$)?`,
)

// saturnRouteRe matches a Saturn `router { ... }` verb operation taking a
// leading string-literal path and OPTIONALLY a trailing handler symbol:
//
//	get  "/users"     listUsers   → verb GET, path /users, handler listUsers
//	getf "/users/%i"  getUser
//	post "/users"     createUser
//	delete "/users/:id" deleteUser
//
// Anchored at a statement boundary (`^[ \t]*`) so an arbitrary `obj.get "..."`
// is not captured. The optional trailing `f` (`getf`/`postf`/…) is the
// format-string variant. Capture group 1 is the verb; group 2 the path; group 3
// (optional) the trailing same-file handler symbol (bare identifier only).
var saturnRouteRe = regexp.MustCompile(
	`(?m)^[ \t]*(get|post|put|delete|patch|options|head)f?\s+"([^"\n\r]*)"` +
		`(?:\s+([A-Za-z_][A-Za-z0-9_']*))?\s*$`,
)

// giraffeMountRe matches a Giraffe `subRoute`/`forward` mount with a string-
// literal prefix, capturing the prefix and the byte offset of the `(` that opens
// the nested sub-app span (#4940). Group 1 is the prefix; the `(` follows the
// closing quote (possibly after whitespace).
var giraffeMountRe = regexp.MustCompile(
	`(?m)\b(?:subRoute|forward)\s+"([^"\n\r]*)"\s*\(`,
)

// ---------------------------------------------------------------------------
// #5114 — the non-db tail of #4941: Falco / Suave / Oxpecker / ASP.NET
// minimal-API (F#) route extraction, alongside the existing Giraffe/Saturn
// coverage (#4906). Each follows the same emit path (one canonical
// http_endpoint_definition per statically-known (verb,path) via
// httproutes.Canonicalize(FrameworkGiraffe)), so the shared resolver + e2e
// route-test linker light up uniformly.
//
//   - Oxpecker is Giraffe-COMPATIBLE (`GET >=> route "/users"` / `routef
//     "/users/%i"` inside `choose [ ... ]`), so it is already captured by
//     giraffeRouteRe — only the pre-filter marker is widened.
//   - Falco endpoint DSL (`get "/users" handler` / `post "/users" handler`,
//     and `mapGet`/`mapPost` register helpers) shares the bare verb-then-
//     literal shape with Saturn, so the plain forms ride saturnRouteRe; the
//     `mapVerb` register helpers get their own recogniser (falcoMapRe).
//   - Suave composes a verb HttpHandler with the `path`/`pathScan`/`pathCi`
//     combinator via `>=>` (`GET >=> path "/users"`, `POST >=> pathScan
//     "/users/%d" handler`) — suaveRouteRe.
//   - ASP.NET minimal-API in F# uses the parenthesised, comma-separated
//     `app.MapGet("/users", handler)` / `app.MapPost(...)` / `app.MapMethods`
//     shape (the same as C#, but reached from the F# producer branch) —
//     fsharpMinimalApiRe.
// ---------------------------------------------------------------------------

// falcoMapRe matches a Falco register-helper route — `mapGet "/users" handler`
// (and `Routing.mapGet`, `Router.mapGet`, etc.), the function-style register
// idiom that is NOT caught by the bare `get "/x"` saturnRouteRe. Capture group 1
// is the verb; group 2 the path literal; group 3 (optional) the bare handler
// symbol for the named IMPLEMENTS bridge.
var falcoMapRe = regexp.MustCompile(
	`(?m)\bmap(Get|Post|Put|Delete|Patch|Head|Options)\s+"([^"\n\r]*)"` +
		`(?:\s+([A-Za-z_][A-Za-z0-9_']*))?`,
)

// suaveRouteRe matches a Suave route: a verb HttpHandler composed with a
// `path`/`pathCi`/`pathScan`/`pathStarts` combinator via the `>=>` operator.
//
//	GET  >=> path "/users"                 → GET /users
//	POST >=> pathScan "/users/%d" handler  → POST /users/{}  (printf param)
//	GET  >=> pathCi "/x"                    → GET /x          (case-insensitive)
//
// Group 1 is the verb; group 2 the path-combinator suffix (`Scan`/`Ci`/
// `Starts`/empty); group 3 the path literal; group 4 (optional) the trailing
// bare handler symbol.
var suaveRouteRe = regexp.MustCompile(
	`(?m)\b(GET|POST|PUT|DELETE|PATCH|OPTIONS|HEAD)\b\s*>=>\s*` +
		`path(Scan|Ci|Starts)?\s+"([^"\n\r]*)"` +
		`(?:\s*>=>\s*([A-Za-z_][A-Za-z0-9_']*)\s*$)?`,
)

// fsharpMinimalApiRe matches an ASP.NET Core minimal-API route registration in
// F#: the parenthesised, comma-separated `app.MapGet("/users", handler)` shape.
//
//	app.MapGet("/users", listUsers)        → GET /users
//	builder.MapPost("/users", createUser)  → POST /users
//	app.MapDelete("/users/{id}", ...)      → DELETE /users/{id}
//
// Group 1 is the verb; group 2 the path literal; group 3 (optional) the bare
// handler symbol immediately after the comma.
var fsharpMinimalApiRe = regexp.MustCompile(
	`(?m)\.Map(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*"([^"\n\r]*)"` +
		`(?:\s*,\s*([A-Za-z_][A-Za-z0-9_']*)\s*\))?`,
)

// ---------------------------------------------------------------------------
// #5368 (epic #5360) — Saturn `controller { }` computation-expression routing.
//
// Saturn's RESTful resource `controller { }` CE is the headline Saturn routing
// idiom that the bare `router { get "/x" h }` recogniser does NOT cover: a
// controller maps CONVENTIONAL CRUD ACTION KEYWORDS to a fixed REST verb+path
// shape, never naming the path literally. The action set is the Saturn contract:
//
//	let userController = controller {
//	    index   (fun ctx -> ...)        // GET    <prefix>           (collection)
//	    show    (fun ctx id -> ...)     // GET    <prefix>/{id}      (one item)
//	    add     (fun ctx -> ...)        // GET    <prefix>/add       (new form)
//	    edit    (fun ctx id -> ...)     // GET    <prefix>/{id}/edit (edit form)
//	    create  (fun ctx -> ...)        // POST   <prefix>           (insert)
//	    update  (fun ctx id -> ...)     // PUT    <prefix>/{id}      (replace)
//	    patch   (fun ctx id -> ...)     // PATCH  <prefix>/{id}      (partial)
//	    delete  (fun ctx id -> ...)     // DELETE <prefix>/{id}      (remove)
//	    deleteAll (fun ctx -> ...)      // DELETE <prefix>           (remove all)
//	}
//
// The controller is mounted under a path PREFIX where it is `forward`ed inside a
// parent router (`forward "/users" userController`) — Saturn's idiomatic mount.
// We resolve that prefix by matching `forward "<prefix>" <controllerName>`
// against the controller's `let`-bound name; a controller with no resolvable
// mount emits its actions at the bare convention path (honest — the action↔verb
// mapping is statically certain even when the mount prefix is not).
//
// Honest exclusions (no fabricated routes):
//   - `pipeline { }` is a Saturn MIDDLEWARE CE (plug/set-header/…), not a route
//     emitter, so it is deliberately NOT synthesised here.
//   - A `subController` / nested-resource mount prefix that is interpolated or
//     unresolvable does not fold; the actions still emit at the conventional
//     path. An action whose keyword is not in the Saturn contract is ignored.
// ---------------------------------------------------------------------------

// saturnControllerAction maps a Saturn controller action keyword to its
// conventional REST (verb, path-suffix) shape. The path suffix is appended to
// the controller's mount prefix; "{id}" is the canonical single-resource param.
type saturnControllerAction struct {
	verb   string
	suffix string // appended to the mount prefix ("" = bare collection path)
}

// saturnControllerActions is the Saturn resource-controller action contract.
// Each present action keyword in a `controller { }` block emits one endpoint.
var saturnControllerActions = map[string]saturnControllerAction{
	"index":     {"GET", ""},
	"show":      {"GET", "/{id}"},
	"add":       {"GET", "/add"},
	"edit":      {"GET", "/{id}/edit"},
	"create":    {"POST", ""},
	"update":    {"PUT", "/{id}"},
	"patch":     {"PATCH", "/{id}"},
	"delete":    {"DELETE", "/{id}"},
	"deleteAll": {"DELETE", ""},
}

// saturnControllerDeclRE matches a `let <name> = controller {` binding and
// captures (1) the controller's bound name. The `controller {` CE opener is
// anchored so an unrelated value named `controller` is not matched.
var saturnControllerDeclRE = regexp.MustCompile(
	`(?m)\blet\s+([A-Za-z_][A-Za-z0-9_']*)\s*=\s*controller\s*\{`,
)

// saturnControllerActionRE matches an action operation line inside a controller
// block: a leading action keyword at a statement boundary. Capture group 1 is
// the action keyword. The keyword is validated against saturnControllerActions.
var saturnControllerActionRE = regexp.MustCompile(
	`(?m)^[ \t]*([A-Za-z]+)\b`,
)

// saturnForwardMountRE matches a Saturn `forward "<prefix>" <controllerName>`
// mount inside a parent router, capturing (1) the mount prefix and (2) the
// forwarded controller's bound name. Resolves a controller's mount path.
var saturnForwardMountRE = regexp.MustCompile(
	`(?m)\bforward\s+"([^"\n\r]*)"\s+([A-Za-z_][A-Za-z0-9_']*)`,
)

// synthesizeSaturnControllers scans for Saturn `controller { }` resource blocks
// and emits one http_endpoint_definition per present action, folding any
// resolvable `forward "<prefix>" <controllerName>` mount prefix.
func synthesizeSaturnControllers(content string, emitRoute func(verb, path, handler string, pos int)) {
	if !strings.Contains(content, "controller {") {
		return
	}
	// Resolve controllerName -> mount prefix from `forward "<p>" <name>` mounts.
	prefixByController := map[string]string{}
	for _, m := range saturnForwardMountRE.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			prefixByController[m[2]] = m[1]
		}
	}

	for _, loc := range saturnControllerDeclRE.FindAllStringSubmatchIndex(content, -1) {
		name := submatch(content, loc, 1)
		open := loc[1] - 1 // byte offset of the `{` (regex ends just past it)
		close := matchCloseBrace(content, open+1)
		if close < 0 {
			continue
		}
		body := content[open+1 : close]
		prefix := strings.TrimRight(prefixByController[name], "/")

		seenAction := map[string]bool{}
		for _, am := range saturnControllerActionRE.FindAllStringSubmatch(body, -1) {
			if len(am) < 2 {
				continue
			}
			kw := am[1]
			act, ok := saturnControllerActions[kw]
			if !ok || seenAction[kw] {
				continue
			}
			seenAction[kw] = true
			path := prefix + act.suffix
			if path == "" {
				path = "/"
			}
			// The controller is the handler scope for its actions.
			emitRoute(act.verb, path, name, open)
		}
	}
}

// matchCloseBrace returns the byte offset of the `}` closing the `{` whose body
// starts at `open`, or -1 if unbalanced. String-literal aware so a brace inside
// a quoted path does not unbalance the count.
func matchCloseBrace(content string, open int) int {
	depth := 1
	inStr := false
	for i := open; i < len(content); i++ {
		c := content[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// giraffeHasRoute is a fast pre-filter: the file must reference an F# web marker
// (Giraffe / Saturn) AND a route token to be worth scanning, so we never misfire
// on arbitrary F# code.
func giraffeHasRoute(content string) bool {
	hasMarker := strings.Contains(content, "Giraffe") ||
		strings.Contains(content, "giraffe") ||
		strings.Contains(content, "Saturn") ||
		strings.Contains(content, "saturn") ||
		// #5114 — the non-db tail of #4941: Falco / Suave / Oxpecker /
		// ASP.NET minimal-API (F#) markers.
		strings.Contains(content, "Falco") ||
		strings.Contains(content, "Suave") ||
		strings.Contains(content, "Oxpecker") ||
		strings.Contains(content, "endpoints") ||
		// #5114 — minimal-API marker: the `app.Map<Verb>(` registration shape.
		strings.Contains(content, ".Map") ||
		strings.Contains(content, ">=>") ||
		strings.Contains(content, "router {") ||
		// #5368: Saturn resource-controller CE marker.
		strings.Contains(content, "controller {") ||
		strings.Contains(content, "HttpHandler") ||
		strings.Contains(content, "subRoute") ||
		strings.Contains(content, "forward") ||
		strings.Contains(content, "choose [")
	if !hasMarker {
		return false
	}
	return strings.Contains(content, "route") ||
		strings.Contains(content, "router {") ||
		// #5368: Saturn resource-controller CE.
		strings.Contains(content, "controller {") ||
		// #5114: Falco `mapGet`/bare-verb, Suave `path`, minimal-API `.Map*`.
		strings.Contains(content, "path ") ||
		strings.Contains(content, ".Map") ||
		strings.Contains(content, "mapGet") ||
		strings.Contains(content, "mapPost") ||
		strings.Contains(content, "endpoints")
}

// giraffeMount is a resolved subRoute/forward mount: a string-literal prefix
// and the [open,close) byte span of its parenthesised sub-app body (#4940).
type giraffeMount struct {
	prefix string
	open   int // byte offset just after the opening `(`
	close  int // byte offset of the matching `)`
}

// collectGiraffeMounts finds every subRoute/forward mount with a literal prefix
// and resolves the byte span of its parenthesised body by balanced-paren
// scanning (string-literal aware so a `(` inside a quoted path is ignored). A
// route whose match start falls inside [open,close) inherits the prefix.
func collectGiraffeMounts(content string) []giraffeMount {
	var mounts []giraffeMount
	for _, loc := range giraffeMountRe.FindAllStringSubmatchIndex(content, -1) {
		// loc: [matchStart,matchEnd, g1Start,g1End]; match ends just past the `(`.
		prefix := content[loc[2]:loc[3]]
		open := loc[1] // index right after the opening `(`
		close := matchCloseParen(content, open)
		if close < 0 {
			continue
		}
		mounts = append(mounts, giraffeMount{prefix: prefix, open: open, close: close})
	}
	return mounts
}

// matchCloseParen returns the byte offset of the `)` that closes the `(` whose
// body starts at `open` (one paren already consumed), or -1 if unbalanced. It
// skips over double-quoted string literals so a `(` / `)` inside a path literal
// does not unbalance the count.
func matchCloseParen(content string, open int) int {
	depth := 1
	inStr := false
	for i := open; i < len(content); i++ {
		c := content[i]
		if inStr {
			if c == '\\' {
				i++ // skip escaped char
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// prefixAt folds the accumulated mount prefixes that enclose byte offset `pos`,
// left-to-right by nesting (outermost mount first), into a single path prefix.
func prefixAt(mounts []giraffeMount, pos int) string {
	type p struct {
		open   int
		prefix string
	}
	var enclosing []p
	for _, m := range mounts {
		if pos >= m.open && pos < m.close {
			enclosing = append(enclosing, p{m.open, m.prefix})
		}
	}
	// Sort by open offset ascending = outermost (earliest) mount first.
	for i := 1; i < len(enclosing); i++ {
		for j := i; j > 0 && enclosing[j].open < enclosing[j-1].open; j-- {
			enclosing[j], enclosing[j-1] = enclosing[j-1], enclosing[j]
		}
	}
	var b strings.Builder
	for _, e := range enclosing {
		seg := strings.Trim(strings.TrimSpace(e.prefix), "/")
		if seg == "" {
			continue
		}
		b.WriteString("/")
		b.WriteString(seg)
	}
	return b.String()
}

// canonicalizeRoutex rewrites a `routex` regex body into a routable path by
// replacing each parenthesised capture group with the printf-style `%s`
// placeholder (#4940), which httproutes.Canonicalize then maps to the
// positional `{}` wildcard. `%s` (not a literal `{}`) is used so the path does
// NOT trip the interpolation guard in emitRoute (which drops any literal `{`).
// `/users/(\d+)` → `/users/%s`; `/x/(\w+)/y` → `/x/%s/y`.
func canonicalizeRoutex(raw string) string {
	return routexGroupRe.ReplaceAllString(raw, "%s")
}

var routexGroupRe = regexp.MustCompile(`\([^)]*\)`)

// canonicalizeMinimalApiCurly rewrites an ASP.NET minimal-API curly-brace path
// param (`{id}`, `{id:int}`, `{*slug}`) into the printf `%s` placeholder so the
// downstream FrameworkGiraffe canonicaliser maps it to the positional `{}`
// wildcard — and so the emitRoute interpolation guard (which drops any literal
// `{`) does not discard the whole route (#5114). A static `/users` path with no
// `{` passes through unchanged.
func canonicalizeMinimalApiCurly(raw string) string {
	if !strings.Contains(raw, "{") {
		return raw
	}
	return minimalApiCurlyRe.ReplaceAllString(raw, "%s")
}

var minimalApiCurlyRe = regexp.MustCompile(`\{[^}/]*\}`)

// synthesizeGiraffeRoutes scans an F# source file for Giraffe / Saturn route
// registrations and emits one http_endpoint_definition per statically-known
// (verb, path). subRoute/forward mount prefixes are folded into nested child
// routes; routex/routeStartsWith variants are handled; a same-file handler
// symbol drives a named IMPLEMENTS bridge (#4940).
func synthesizeGiraffeRoutes(content string, emit emitFn) {
	if !giraffeHasRoute(content) {
		return
	}
	mounts := collectGiraffeMounts(content)
	seen := map[string]bool{}

	// emitRoute applies the enclosing mount prefix (at byte offset `pos`),
	// canonicalises, dedups, and emits — passing a captured same-file handler
	// symbol so the synthesis-time bridge can wire a named IMPLEMENTS edge.
	emitRoute := func(verbRaw, rawPath, handler string, pos int) {
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
		// Fold any enclosing subRoute/forward mount prefix (#4940).
		raw = prefixAt(mounts, pos) + raw
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
		// convention. A captured same-file handler symbol (#4940) drives the
		// synthesis-time named IMPLEMENTS bridge; empty leaves the endpoint
		// un-bridged (no forced/fabricated handler).
		emit(verb, canonical, "giraffe", "Controller", handler)
	}

	for _, loc := range giraffeRouteRe.FindAllStringSubmatchIndex(content, -1) {
		verb := submatch(content, loc, 1)
		variant := submatch(content, loc, 2)
		path := submatch(content, loc, 3)
		handler := submatch(content, loc, 4)
		if variant == "x" {
			path = canonicalizeRoutex(path)
		}
		emitRoute(verb, path, handler, loc[0])
	}
	for _, loc := range saturnRouteRe.FindAllStringSubmatchIndex(content, -1) {
		verb := submatch(content, loc, 1)
		path := submatch(content, loc, 2)
		handler := submatch(content, loc, 3)
		emitRoute(verb, path, handler, loc[0])
	}
	// #5114 — Falco register helpers (`mapGet "/users" handler`). The bare
	// Falco form `get "/users" handler` already rides saturnRouteRe above.
	for _, loc := range falcoMapRe.FindAllStringSubmatchIndex(content, -1) {
		verb := submatch(content, loc, 1)
		path := submatch(content, loc, 2)
		handler := submatch(content, loc, 3)
		emitRoute(verb, path, handler, loc[0])
	}
	// #5114 — Suave (`GET >=> path "/users"` / `>=> pathScan "/users/%d" h`).
	// `pathScan` carries printf placeholders handled by the FrameworkGiraffe
	// canonicaliser exactly like Giraffe `routef`.
	for _, loc := range suaveRouteRe.FindAllStringSubmatchIndex(content, -1) {
		verb := submatch(content, loc, 1)
		path := submatch(content, loc, 3)
		handler := submatch(content, loc, 4)
		emitRoute(verb, path, handler, loc[0])
	}
	// #5114 — ASP.NET minimal-API in F# (`app.MapGet("/users", handler)`).
	// Minimal-API paths use the ASP.NET curly-brace param convention
	// (`/users/{id}`); the emitRoute interpolation guard drops any literal `{`,
	// so we pre-canonicalise each `{name}` to the printf `%s` token (which
	// FrameworkGiraffe then maps to the positional `{}` wildcard), mirroring the
	// routex pre-canonicalisation. A constrained param (`{id:int}`) collapses to
	// a single `%s` too.
	for _, loc := range fsharpMinimalApiRe.FindAllStringSubmatchIndex(content, -1) {
		verb := submatch(content, loc, 1)
		path := canonicalizeMinimalApiCurly(submatch(content, loc, 2))
		handler := submatch(content, loc, 3)
		emitRoute(verb, path, handler, loc[0])
	}
	// Oxpecker is Giraffe-compatible (`GET >=> route "/users"` / `routef`) and
	// is captured by the giraffeRouteRe loop above — no separate recogniser.

	// #5368 — Saturn `controller { }` resource controllers. The conventional
	// action paths carry the `{id}` curly param, which emitRoute's interpolation
	// guard would drop; pre-canonicalise it to the printf `%s` token (the same
	// treatment minimal-API curly params get) so FrameworkGiraffe maps it to the
	// positional `{}` wildcard.
	synthesizeSaturnControllers(content, func(verb, path, handler string, pos int) {
		emitRoute(verb, canonicalizeMinimalApiCurly(path), handler, pos)
	})
}
