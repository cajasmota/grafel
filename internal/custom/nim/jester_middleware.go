// jester_middleware.go — Nim Jester / Prologue middleware coverage (#5367,
// epic #5360). Companion to the route-synthesis pass in
// internal/engine/http_endpoint_jester.go, which already recovers the route
// VERB+PATH but has no middleware awareness.
//
// Nim's two dominant web frameworks register cross-cutting middleware as
// follows:
//
//	Jester (Sinatra-like, indentation-delimited `routes:` block):
//	  routes:
//	    before:                 # runs before EVERY request
//	      …
//	    before re"/admin/.*":   # path-scoped pre-filter
//	      …
//	    after:                  # runs after EVERY request
//	      …
//	  Each before/after block is a middleware filter in the request lifecycle.
//
//	Prologue (ASGI-ish, explicit middleware list / registration):
//	  let app = newApp(settings = settings, middlewares = @[debugRequestMiddleware()])
//	  app.use(staticFileMiddleware("static"))           # global middleware
//	  app.use(@[corsMiddleware(), sessionMiddleware()]) # several at once
//	  Each registered middleware proc is a pipeline stage.
//
// What this extractor emits (mirrors the Lua/Rust middleware shape — one
// SCOPE.Pattern per middleware with framework / provenance / middleware_type):
//   - Jester: one SCOPE.Pattern "middleware:before"/"middleware:after" per
//     before/after filter block, with `phase` (before|after) and the optional
//     path scope stamped.
//   - Prologue: one SCOPE.Pattern "middleware:<name>" per middleware referenced
//     in `app.use(...)` or a `middlewares = @[...]` newApp argument, with the
//     middleware proc name as `middleware_type`.
//
// All cells are honest PARTIAL: regex-based detection without full AST parsing;
// dynamically-built middleware lists and conditionally-registered middleware are
// not recovered (no fabricated middleware).
//
// Registration key: "custom_nim_jester_middleware".
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_nim_jester_middleware", &nimJesterMiddlewareExtractor{})
}

type nimJesterMiddlewareExtractor struct{}

func (e *nimJesterMiddlewareExtractor) Language() string {
	return "custom_nim_jester_middleware"
}

var (
	// nimJesterFilterRe matches a Jester `before:` / `after:` filter block header
	// inside a routes: block, optionally path-scoped (`before re"/admin/.*":` or
	// `before "/x":`). Group 1 = phase (before|after); group 2 = the optional path
	// scope literal (without the re prefix). The trailing `:` opens the block.
	nimJesterFilterRe = regexp.MustCompile(
		`(?m)^[ \t]*(before|after)\b(?:\s+(?:re)?"([^"\n\r]*)")?\s*:`)

	// nimPrologueUseRe matches a Prologue `app.use(<middleware>)` / `router.use(…)`
	// registration. Group 1 is the argument tail captured to end of line (so a
	// `@[a(), b()]` sequence with its own nested parens is fully covered); the
	// individual middleware calls are mined by nimMiddlewareCallRe, gated to
	// *Middleware names so trailing non-middleware tokens never misfire.
	nimPrologueUseRe = regexp.MustCompile(
		`(?m)\b[A-Za-z_][A-Za-z0-9_]*\s*\.\s*use\s*\(\s*(.*)$`)

	// nimPrologueNewAppMwRe matches the `middlewares = @[ … ]` named argument of a
	// Prologue `newApp(...)` call. Group 1 is the sequence body.
	nimPrologueNewAppMwRe = regexp.MustCompile(
		`(?s)\bmiddlewares\s*=\s*@\[([^\]]*)\]`)

	// nimMiddlewareCallRe extracts a middleware proc reference (`fooMiddleware()`
	// or a bare `fooMiddleware`). Group 1 is the proc name. Only identifiers that
	// look like middleware (end in "Middleware" or "middleware") or are explicit
	// `()` calls are accepted, keeping attribution honest.
	nimMiddlewareCallRe = regexp.MustCompile(
		`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

// nimJesterMwHasMarker is a fast pre-filter: a Jester routes: block with a
// before/after filter, or a Prologue middleware registration.
func nimJesterMwHasMarker(content string) bool {
	if (strings.Contains(content, "routes:") || strings.Contains(content, "jester")) &&
		(strings.Contains(content, "before") || strings.Contains(content, "after")) {
		return true
	}
	return strings.Contains(content, ".use(") || strings.Contains(content, "middlewares")
}

func (e *nimJesterMiddlewareExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimJesterMwHasMarker(src) {
		return nil, nil
	}

	var out []types.EntityRecord

	// --- Jester before/after filters (only inside a Jester context) ----------
	if strings.Contains(src, "routes:") || strings.Contains(src, "jester") {
		for _, m := range nimJesterFilterRe.FindAllStringSubmatchIndex(src, -1) {
			phase := src[m[2]:m[3]]
			pathScope := ""
			if m[4] >= 0 && m[5] >= 0 {
				pathScope = src[m[4]:m[5]]
			}
			props := map[string]string{
				"framework":       "jester",
				"provenance":      "INFERRED_FROM_JESTER_FILTER",
				"middleware_type": phase + "_filter",
				"phase":           phase,
			}
			if pathScope != "" {
				props["path_scope"] = pathScope
			}
			ent := types.EntityRecord{
				Name:       "middleware:" + phase,
				Kind:       "SCOPE.Pattern",
				Subtype:    "middleware",
				SourceFile: file.Path,
				Language:   "nim",
				StartLine:  nimLineOf(src, m[0]),
				EndLine:    nimLineOf(src, m[0]),
				Properties: props,
			}
			ent.ID = ent.ComputeID()
			out = append(out, ent)
		}
	}

	// --- Prologue app.use(...) + newApp(middlewares = @[...]) -----------------
	seen := map[string]bool{}
	emitPrologueMw := func(name string, line int) {
		if name == "" || seen[name] {
			return
		}
		// Honest gate: only accept identifiers that look like middleware.
		lc := strings.ToLower(name)
		if !strings.Contains(lc, "middleware") {
			return
		}
		seen[name] = true
		ent := types.EntityRecord{
			Name:       "middleware:" + name,
			Kind:       "SCOPE.Pattern",
			Subtype:    "middleware",
			SourceFile: file.Path,
			Language:   "nim",
			StartLine:  line,
			EndLine:    line,
			Properties: map[string]string{
				"framework":       "prologue",
				"provenance":      "INFERRED_FROM_PROLOGUE_MIDDLEWARE",
				"middleware_type": name,
			},
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}
	for _, m := range nimPrologueUseRe.FindAllStringSubmatchIndex(src, -1) {
		argTail := src[m[2]:m[3]]
		line := nimLineOf(src, m[0])
		for _, cm := range nimMiddlewareCallRe.FindAllStringSubmatch(argTail, -1) {
			emitPrologueMw(cm[1], line)
		}
	}
	for _, m := range nimPrologueNewAppMwRe.FindAllStringSubmatchIndex(src, -1) {
		body := src[m[2]:m[3]]
		line := nimLineOf(src, m[0])
		for _, cm := range nimMiddlewareCallRe.FindAllStringSubmatch(body, -1) {
			emitPrologueMw(cm[1], line)
		}
	}

	return out, nil
}
