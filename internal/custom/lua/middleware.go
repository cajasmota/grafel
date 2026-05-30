// middleware.go — Lua middleware extractor (middleware_coverage).
//
// Covers middleware detection for Lua web frameworks:
//
//	OpenResty:
//	  - rewrite_by_lua_block / rewrite_by_lua_file (URL rewriting phase)
//	  - access_by_lua_block / access_by_lua_file (access control phase)
//	  - header_filter_by_lua_block (response header manipulation)
//	  - body_filter_by_lua_block (response body manipulation)
//	  - init_by_lua_block / init_worker_by_lua_block (startup hooks)
//	  - log_by_lua_block (logging phase)
//	  - Kong plugin handler phases: init, access, header_filter, body_filter, log
//
//	Lapis:
//	  - before_filter / app:before (request filter hooks)
//	  - app:after (response filter hooks)
//	  - lapis.flow.Flow as middleware pipeline
//	  - error_handler / on_error patterns
//
// All cells are partial: regex-based detection without full AST parsing.
package lua

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// luaPhaseRank gives the canonical OpenResty request-lifecycle order of a phase
// directive, so the emitted middleware chain carries a stable, comparable
// `phase_order` independent of textual position in nginx.conf. Phases not in
// the table sort after the known ones.
func luaPhaseRank(phase string) int {
	switch {
	case strings.HasPrefix(phase, "init_worker_by_lua"):
		return 1
	case strings.HasPrefix(phase, "init_by_lua"):
		return 0
	case strings.HasPrefix(phase, "rewrite_by_lua"):
		return 2
	case strings.HasPrefix(phase, "access_by_lua"):
		return 3
	case strings.HasPrefix(phase, "content_by_lua"):
		return 4
	case strings.HasPrefix(phase, "header_filter_by_lua"):
		return 5
	case strings.HasPrefix(phase, "body_filter_by_lua"):
		return 6
	case strings.HasPrefix(phase, "log_by_lua"):
		return 7
	default:
		return 99
	}
}

func init() {
	extractor.Register("lua_middleware", &luaMiddlewareExtractor{})
}

// luaMiddlewareExtractor detects middleware in Lua source files.
type luaMiddlewareExtractor struct{}

func (e *luaMiddlewareExtractor) Language() string { return "lua_middleware" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// OpenResty phase directives that act as middleware hooks.
	reNginxPhase = regexp.MustCompile(
		`(?m)\b(rewrite_by_lua(?:_block|_file)|access_by_lua(?:_block|_file)|` +
			`header_filter_by_lua(?:_block|_file)|body_filter_by_lua(?:_block|_file)|` +
			`init_by_lua(?:_block|_file)|init_worker_by_lua(?:_block|_file)|` +
			`log_by_lua(?:_block|_file)|content_by_lua(?:_block|_file))\b`)

	// Kong plugin handler methods
	reKongHandler = regexp.MustCompile(
		`(?m)function\s+\w+\s*:\s*(init|init_worker|certificate|rewrite|access|` +
			`header_filter|body_filter|log|preread)\s*\(\s*\w*\s*\)`)

	// Lapis before_filter / app:before
	reLapisBeforeMiddleware = regexp.MustCompile(
		`(?m)(?:\bbefore_filter\b|\bapp\s*:\s*before\s*\()`)

	// Lapis app:after
	reLapisAfter = regexp.MustCompile(
		`(?m)\bapp\s*:\s*after\s*\(`)

	// Lapis error_handler / on_error
	reLapisErrorHandler = regexp.MustCompile(
		`(?m)(?:\berror_handler\b|\bon_error\b|\bhandle_error\b)`)

	// lapis.flow.Flow middleware pipeline
	reLapisFlow = regexp.MustCompile(
		`(?m)\brequire\s*[("']lapis\.flow["']?\)?|\blapis\.flow\b`)
)

// Extract implements extractor.Extractor.
func (e *luaMiddlewareExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	ext := strings.ToLower(file.Path)

	isLua := strings.HasSuffix(ext, ".lua") || strings.HasSuffix(ext, ".moon")
	isConf := strings.HasSuffix(ext, ".conf") || strings.Contains(ext, "nginx")
	if !isLua && !isConf {
		return nil, nil
	}

	hasMiddleware := strings.Contains(src, "_by_lua") ||
		strings.Contains(src, "before_filter") || strings.Contains(src, "app:before") ||
		strings.Contains(src, "app:after") || strings.Contains(src, "error_handler") ||
		strings.Contains(src, "lapis.flow") || strings.Contains(src, ":init(") ||
		strings.Contains(src, ":access(") || strings.Contains(src, ":log(")
	if !hasMiddleware {
		return nil, nil
	}

	var out []types.EntityRecord

	// OpenResty phase directives. `chain_index` records the textual order of
	// appearance; `phase_order` records the canonical request-lifecycle rank
	// so the middleware chain is reconstructable regardless of file layout.
	for chainIdx, idx := range reNginxPhase.FindAllStringSubmatchIndex(src, -1) {
		directive := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("nginx_phase:"+directive, string(types.EntityKindPattern), "middleware_hook", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "middleware",
			"framework", "openresty",
			"kind", "nginx_phase",
			"phase", directive,
			"chain_index", strconv.Itoa(chainIdx),
			"phase_order", strconv.Itoa(luaPhaseRank(directive)),
		)
		out = append(out, entity)
	}

	// Kong plugin phases
	for _, idx := range reKongHandler.FindAllStringSubmatchIndex(src, -1) {
		phase := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("kong_handler:"+phase, string(types.EntityKindPattern), "middleware_hook", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "middleware",
			"framework", "kong",
			"kind", "plugin_phase",
			"phase", phase,
		)
		out = append(out, entity)
	}

	// Lapis before_filter / before middleware. `chain_index` records the
	// textual order so a multi-filter chain is reconstructable.
	for chainIdx, idx := range reLapisBeforeMiddleware.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_before_filter", string(types.EntityKindPattern), "middleware_hook", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "middleware",
			"framework", "lapis",
			"kind", "before_filter",
			"phase", "before",
			"chain_index", strconv.Itoa(chainIdx),
		)
		out = append(out, entity)
	}

	// Lapis app:after
	for _, idx := range reLapisAfter.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_after_filter", string(types.EntityKindPattern), "middleware_hook", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "middleware",
			"framework", "lapis",
			"kind", "after_filter",
		)
		out = append(out, entity)
	}

	// Lapis error handler
	for _, idx := range reLapisErrorHandler.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_error_handler", string(types.EntityKindPattern), "middleware_hook", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "middleware",
			"framework", "lapis",
			"kind", "error_handler",
		)
		out = append(out, entity)
	}

	// Lapis flow pipeline
	if reLapisFlow.MatchString(src) {
		idx := reLapisFlow.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_flow_pipeline", string(types.EntityKindPattern), "middleware_pipeline", file.Path, "lua", ln)
		setProps(&entity,
			"signal", "middleware",
			"framework", "lapis",
			"kind", "flow_pipeline",
		)
		out = append(out, entity)
	}

	return out, nil
}
