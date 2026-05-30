// lua_routes.go — deep Lua HTTP routing synthesis (#3484).
//
// The custom regex extractors in internal/custom/lua/ emit Route/Pattern
// SIGNAL entities (framework + path properties) for the coverage lanes. This
// file lifts Lua routing to the TS/JS bar by emitting canonical
// `http:<VERB>:<path>` synthetics with handler attribution and `:id`→`{id}`
// normalisation, for the two first-class Lua web surfaces:
//
//   - synthesizeLapis     — Lapis `app:get/post/put/patch/delete/options/head`,
//     named/unnamed `app:match`, and `respond_to({...})`
//     verb tables. Handlers are attributed to the app +
//     verb (the handler is the inline function literal).
//   - synthesizeOpenResty — OpenResty nginx `location /path { ... }` stanzas
//     (literal prefix, ANY verb, content_by_lua handler)
//     and `lua-resty-router` `r:get("/users/:id", fn)`
//     DSL routes.
//
// Each synthesizer is gated on a cheap file-level signal so it is a no-op on
// unrelated Lua files, and each asserts a specific (verb, canonical-path) —
// never a bare length check.
//
// NB: engine synthesis runs only on files the classifier tags as language
// "lua" (i.e. *.lua). Pure nginx.conf `location` blocks are NOT lua-classified
// and are covered by the internal/custom/lua/routing.go extractor instead; the
// OpenResty synthesizer here fires on `location` blocks embedded in *.lua
// config-driver files and on lua-resty-router DSL usage.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Lapis — app:<verb>("/path", handler) / app:match(...) / respond_to({...})
// ---------------------------------------------------------------------------

var (
	// Lapis application signal: `lapis.Application()` or `require("lapis")`.
	luaLapisSignalRe = regexp.MustCompile(`\blapis\.Application\b|\brequire\s*\(?\s*["']lapis["']`)

	// `app:get("/users/:id", ...)` — receiver:verb("/path", ...).
	luaLapisVerbRe = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*(get|post|put|patch|delete|options|head)\s*\(\s*["']([^"']+)["']`)

	// `app:match("name", "/path", ...)` — named two-string match.
	luaLapisNamedMatchRe = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*match\s*\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']`)

	// `app:match("/path", ...)` — unnamed match whose FIRST argument is the
	// path (starts with `/`). The named form's first arg is a route NAME with
	// no leading slash, so the `/`-anchor disambiguates the two.
	luaLapisAnonMatchRe = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*match\s*\(\s*["'](/[^"']*)["']`)

	// `respond_to({ GET = ..., POST = ... })` head + per-verb keys.
	luaLapisRespondToHeadRe = regexp.MustCompile(`\brespond_to\s*\(\s*\{`)
	luaLapisRespondToVerbRe = regexp.MustCompile(
		`(?m)^\s*(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\s*=`)

	// A leading verb route is the natural anchor to attribute respond_to
	// verbs to the same path; we extract the nearest preceding string literal
	// path on the respond_to line's owning route. respond_to is most often the
	// handler argument to a verb/match route, so we attribute to the enclosing
	// route's path when discoverable, else to the app receiver.
)

// luaPathHint returns the most recent `["']/path["']` string literal appearing
// before offset `at` in src, used to attribute a respond_to verb table to its
// enclosing route path. Returns "" when none is found within the preceding
// 240 bytes (a respond_to call sits within a few chars of its route string).
var luaRoutePathBeforeRe = regexp.MustCompile(`["'](/[^"'\r\n]*)["'][^"'\r\n]*$`)

func luaPathHint(src string, at int) string {
	lo := at - 240
	if lo < 0 {
		lo = 0
	}
	window := src[lo:at]
	if m := luaRoutePathBeforeRe.FindStringSubmatch(window); len(m) > 1 {
		return m[1]
	}
	return ""
}

// synthesizeLapis emits one canonical endpoint per Lapis route declaration.
func synthesizeLapis(content string, emit emitFn) {
	if !luaLapisSignalRe.MatchString(content) {
		return
	}

	// Verb routes: app:get("/users/:id", fn).
	for _, m := range luaLapisVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		app := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := content[m[6]:m[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkLapis, raw)
		if canonical == "" {
			continue
		}
		emit(verb, canonical, "lapis", "SCOPE.Component", app)
	}

	// Named match routes: app:match("name", "/path", fn). Verb is unknown
	// (the handler dispatches internally), so emit ANY attributed to the
	// route name.
	for _, m := range luaLapisNamedMatchRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		name := content[m[4]:m[5]]
		raw := content[m[6]:m[7]]
		// Guard: the first arg must be a route NAME (no slash); if it starts
		// with `/` this is actually the anonymous two-arg form mis-greedy and
		// is handled by the anon matcher below.
		if strings.HasPrefix(name, "/") {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkLapis, raw)
		if canonical == "" {
			continue
		}
		emit("ANY", canonical, "lapis", "SCOPE.Operation", name)
	}

	// Unnamed match routes: app:match("/path", fn) → ANY.
	for _, m := range luaLapisAnonMatchRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		app := content[m[2]:m[3]]
		raw := content[m[4]:m[5]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkLapis, raw)
		if canonical == "" {
			continue
		}
		emit("ANY", canonical, "lapis", "SCOPE.Component", app)
	}

	// respond_to({ GET = ..., POST = ... }) verb tables. Each verb key becomes
	// its own endpoint on the enclosing route's path (the nearest preceding
	// `"/path"` literal). When no path is discoverable the table is skipped —
	// a respond_to without an attachable route would emit a pathless endpoint.
	for _, head := range luaLapisRespondToHeadRe.FindAllStringIndex(content, -1) {
		path := luaPathHint(content, head[0])
		if path == "" {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkLapis, path)
		if canonical == "" {
			continue
		}
		// Scan the verb keys that follow the respond_to head, bounded by the
		// next respond_to head or 600 bytes (a verb table is compact).
		end := head[1] + 600
		if end > len(content) {
			end = len(content)
		}
		body := content[head[1]:end]
		for _, vm := range luaLapisRespondToVerbRe.FindAllStringSubmatch(body, -1) {
			verb := strings.ToUpper(vm[1])
			emit(verb, canonical, "lapis_respond_to", "SCOPE.Component", "respond_to")
		}
	}
}

// ---------------------------------------------------------------------------
// OpenResty — nginx `location /path { ... }` + lua-resty-router DSL
// ---------------------------------------------------------------------------

var (
	// `location /path {` or `location = /path {` (exact-match modifier).
	luaNginxLocationRe = regexp.MustCompile(
		`(?m)^\s*location\s+(?:[=~^]+\*?\s+)?["']?(/[^\s"'{;]*)["']?\s*\{`)

	// content_by_lua handler signal inside a location block.
	luaContentByLuaRe = regexp.MustCompile(`\bcontent_by_lua(?:_block|_file)\b`)

	// lua-resty-router DSL: `r:get("/users/:id", fn)` where r is a router
	// instance from `require("resty.router")`. Distinguished from Lapis by the
	// resty.router require signal.
	luaRestyRouterSignalRe = regexp.MustCompile(`\brequire\s*\(?\s*["'](?:resty\.router|router)["']`)
	luaRestyRouterVerbRe   = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*(get|post|put|patch|delete|options|head)\s*\(\s*["']([^"']+)["']`)
)

// synthesizeOpenResty emits endpoints for nginx `location` stanzas embedded in
// lua-classified files and for lua-resty-router DSL routes.
func synthesizeOpenResty(content string, emit emitFn) {
	// --- nginx location blocks (ANY verb; nginx dispatches on $request_method
	//     inside the lua handler). Gated on content_by_lua presence so plain
	//     static-file location blocks are not treated as app routes. ---
	if luaContentByLuaRe.MatchString(content) {
		for _, m := range luaNginxLocationRe.FindAllStringSubmatchIndex(content, -1) {
			if len(m) < 4 {
				continue
			}
			raw := content[m[2]:m[3]]
			canonical := httproutes.Canonicalize(httproutes.FrameworkOpenResty, raw)
			if canonical == "" {
				continue
			}
			emit("ANY", canonical, "openresty", "SCOPE.Component", "content_by_lua")
		}
	}

	// --- lua-resty-router DSL routes (gated on the resty.router require). ---
	if luaRestyRouterSignalRe.MatchString(content) {
		for _, m := range luaRestyRouterVerbRe.FindAllStringSubmatchIndex(content, -1) {
			if len(m) < 8 {
				continue
			}
			router := content[m[2]:m[3]]
			verb := strings.ToUpper(content[m[4]:m[5]])
			raw := content[m[6]:m[7]]
			canonical := httproutes.Canonicalize(httproutes.FrameworkOpenResty, raw)
			if canonical == "" {
				continue
			}
			emit(verb, canonical, "lua-resty-router", "SCOPE.Component", router)
		}
	}
}
