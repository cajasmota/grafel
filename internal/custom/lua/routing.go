// routing.go — Lua route extraction for OpenResty and Lapis frameworks.
//
// Covers route_extraction, endpoint_synthesis, handler_attribution for:
//
//	OpenResty (nginx+Lua):
//	  - content_by_lua_block / content_by_lua_file directives in nginx.conf
//	  - ngx.req.get_method() + path matching via ngx.var.uri
//	  - location /path { content_by_lua_block { ... } } nginx config stanzas
//
//	Lapis (OpenResty/MoonScript web framework):
//	  - app:get("/path", handler) / app:post("/path", handler)
//	  - app:match("name", "/path", handler)
//	  - lapis.Application() route definitions
//	  - respond_to({ GET=..., POST=... }) verb tables
//
// All cells are partial: regex-based detection without full AST parsing.
package lua

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("lua_routing", &luaRoutingExtractor{})
}

// luaRoutingExtractor detects route definitions in OpenResty and Lapis files.
type luaRoutingExtractor struct{}

func (e *luaRoutingExtractor) Language() string { return "lua_routing" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// OpenResty: location /path { ... content_by_lua_block { ... } }
	// Matches: location /some/path { or location = /path {
	reNginxLocation = regexp.MustCompile(
		`(?m)^\s*location\s+(?:=\s+)?["']?(/[^\s"'{;]*)["']?\s*\{`)

	// OpenResty: ngx.var.uri-based routing with explicit path strings
	// matches: if ngx.var.uri == "/path" or if ngx.var.uri:match("/path")
	reNginxURIMatch = regexp.MustCompile(
		`(?m)\bngx\.var\.uri\s*(?:==\s*["'](/[^"'\s]+)["']|:match\s*\(\s*["']([^"'\s]+)["'])`)

	// Lapis: app:get("/path", handler) / app:post("/path", ...)
	// verb methods on an Application instance
	reLapisVerb = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*(get|post|put|patch|delete|options|head)\s*\(\s*["']([^"']+)["']`)

	// Lapis: app:match("name", "/path", handler)
	reLapisMatch = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*match\s*\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']`)

	// Lapis: app:match("/path", handler) — unnamed form whose FIRST argument
	// is the path (starts with `/`); the named form's first arg is a route
	// name with no leading slash.
	reLapisAnonMatch = regexp.MustCompile(
		`(?m)\b(\w+)\s*:\s*match\s*\(\s*["'](/[^"']*)["']`)

	// Lapis: respond_to({ GET = handler, POST = handler })
	reLapisRespondTo = regexp.MustCompile(
		`(?m)\brespond_to\s*\(\s*\{`)

	// respond_to verb key inside table: GET = ..., POST = ...
	reLapisRespondToVerb = regexp.MustCompile(
		`(?m)^\s*(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\s*=`)

	// lapis.Application() constructor
	reLapisApp = regexp.MustCompile(
		`(?m)\blapis\.Application\s*\(\s*\)`)

	// OpenResty handler: content_by_lua_block or content_by_lua_file
	reContentByLua = regexp.MustCompile(
		`(?m)\bcontent_by_lua(?:_block|_file)\b`)
)

// Extract implements extractor.Extractor.
func (e *luaRoutingExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	ext := strings.ToLower(file.Path)

	// Fast guard: only process .lua, .conf, or nginx config files.
	isLua := strings.HasSuffix(ext, ".lua") || strings.HasSuffix(ext, ".moon")
	isConf := strings.HasSuffix(ext, ".conf") || strings.Contains(ext, "nginx")
	if !isLua && !isConf {
		return nil, nil
	}

	// Further guard: skip files with no routing signals.
	hasRouting := strings.Contains(src, "location") ||
		strings.Contains(src, "ngx.var.uri") ||
		strings.Contains(src, ":get(") || strings.Contains(src, ":post(") ||
		strings.Contains(src, ":put(") || strings.Contains(src, ":delete(") ||
		strings.Contains(src, ":patch(") || strings.Contains(src, ":match(") ||
		strings.Contains(src, "respond_to") || strings.Contains(src, "lapis.Application")
	if !hasRouting {
		return nil, nil
	}

	var out []types.EntityRecord

	// --- OpenResty nginx location blocks ---
	for _, idx := range reNginxLocation.FindAllStringSubmatchIndex(src, -1) {
		path := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("location:"+path, string(types.EntityKindRoute), "http_route", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "routing",
			"framework", "openresty",
			"kind", "nginx_location",
			"path", path,
			"canonical_path", luaCanonicalPath(path),
		)
		out = append(out, entity)
	}

	// --- OpenResty ngx.var.uri path matching ---
	for _, idx := range reNginxURIMatch.FindAllStringSubmatchIndex(src, -1) {
		path := ""
		if idx[2] >= 0 {
			path = src[idx[2]:idx[3]]
		} else if idx[4] >= 0 {
			path = src[idx[4]:idx[5]]
		}
		if path == "" {
			continue
		}
		ln := lineOf(src, idx[0])
		entity := makeEntity("uri_match:"+path, string(types.EntityKindRoute), "http_route", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "routing",
			"framework", "openresty",
			"kind", "uri_match",
			"path", path,
		)
		out = append(out, entity)
	}

	// --- Lapis verb methods: app:get/post/put/delete/patch ---
	for _, idx := range reLapisVerb.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(src[idx[4]:idx[5]])
		path := src[idx[6]:idx[7]]
		ln := lineOf(src, idx[0])
		entity := makeEntity(verb+":"+path, string(types.EntityKindRoute), "http_route", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "routing",
			"framework", "lapis",
			"kind", "verb_route",
			"method", verb,
			"path", path,
			"canonical_path", luaCanonicalPath(path),
		)
		out = append(out, entity)
	}

	// --- Lapis app:match("name", "/path", ...) ---
	for _, idx := range reLapisMatch.FindAllStringSubmatchIndex(src, -1) {
		name := src[idx[4]:idx[5]]
		path := src[idx[6]:idx[7]]
		// Skip the unnamed form where the first arg is actually the path
		// (handled by reLapisAnonMatch); a route NAME never starts with `/`.
		if strings.HasPrefix(name, "/") {
			continue
		}
		ln := lineOf(src, idx[0])
		entity := makeEntity("match:"+name+":"+path, string(types.EntityKindRoute), "http_route", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "routing",
			"framework", "lapis",
			"kind", "named_route",
			"route_name", name,
			"path", path,
			"canonical_path", luaCanonicalPath(path),
		)
		out = append(out, entity)
	}

	// --- Lapis app:match("/path", ...) unnamed ---
	for _, idx := range reLapisAnonMatch.FindAllStringSubmatchIndex(src, -1) {
		path := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("match:"+path, string(types.EntityKindRoute), "http_route", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "routing",
			"framework", "lapis",
			"kind", "anon_route",
			"method", "ANY",
			"path", path,
			"canonical_path", luaCanonicalPath(path),
		)
		out = append(out, entity)
	}

	// --- Lapis respond_to({ GET = ..., POST = ... }) ---
	if reLapisRespondTo.MatchString(src) {
		for _, idx := range reLapisRespondToVerb.FindAllStringSubmatchIndex(src, -1) {
			verb := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			entity := makeEntity("respond_to:"+verb, string(types.EntityKindPattern), "http_handler", file.Path, "lua", ln)
			setProps(&entity,
				"signal", "routing",
				"framework", "lapis",
				"kind", "respond_to_verb",
				"method", verb,
			)
			out = append(out, entity)
		}
	}

	// --- OpenResty content_by_lua_block / content_by_lua_file ---
	if reContentByLua.MatchString(src) {
		for _, idx := range reContentByLua.FindAllStringIndex(src, -1) {
			ln := lineOf(src, idx[0])
			directive := src[idx[0]:idx[1]]
			entity := makeEntity("content_handler:line"+strings.TrimSpace(directive), string(types.EntityKindPattern), "http_handler", file.Path, "lua", ln)
			setProps(&entity,
				"signal", "routing",
				"framework", "openresty",
				"kind", "content_by_lua",
			)
			out = append(out, entity)
		}
	}

	return out, nil
}
