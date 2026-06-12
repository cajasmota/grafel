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

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
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
		strings.Contains(content, "subRoute") ||
		strings.Contains(content, "forward") ||
		strings.Contains(content, "choose [")
	if !hasMarker {
		return false
	}
	return strings.Contains(content, "route") ||
		strings.Contains(content, "router {")
}

// giraffeMount is a resolved subRoute/forward mount: a string-literal prefix
// and the [open,close) byte span of its parenthesised sub-app body (#4940).
type giraffeMount struct {
	prefix     string
	open       int // byte offset just after the opening `(`
	close      int // byte offset of the matching `)`
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
}
