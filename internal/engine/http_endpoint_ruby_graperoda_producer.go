// http_endpoint_ruby_graperoda_producer.go — Grape + Roda block-route DSLs →
// http_endpoint_definition synthesis with inline-handler IMPLEMENTS bridge (#4417).
//
// Follow-up to the Sinatra inline-handler work (#4385). Two more Ruby web
// frameworks attach their handler as an ANONYMOUS block — there is no named
// handler method — so each route must be signalled as an inline handler
// (refKind=inlineHandlerRefKind) so makeEmit synthesizes a stable
// `<inline VERB /path>` handler entity + a same-file IMPLEMENTS bridge, instead
// of leaving the endpoint a handler-less graph island (the #4324/#4385 mechanism
// is framework-general — #4319).
//
//   - Grape  (`class API < Grape::API`)
//
//     resource :users do
//     get do ... end            → GET  /users
//     get ':id' do ... end      → GET  /users/{id}
//     post '/x' do ... end      → POST /users/x
//     namespace :v1 do
//     get do ... end          → GET  /users/v1
//     end
//     end
//
//     `resource`/`resources`/`namespace`/`group`/`segment` blocks compose a
//     path prefix (`:users` → `/users`); the verb block at the leaf contributes
//     the (optional) trailing path. Grape path parameters use the Sinatra/
//     Express `:name` colon convention, so `:id` → `{id}`.
//
//   - Roda  (`class App < Roda`)
//
//     route do |r|
//     r.on "users" do
//     r.get do ... end        → GET  /users
//     r.get Integer do |id|   → GET  /users/{id}
//     ... end
//     r.is "x" do
//     r.post do ... end       → POST /users/x
//     ... end
//     end
//     end
//
//     Roda is a routing TREE: `r.on`/`r.is` branch openers contribute path
//     segments, and the terminal verb (`r.get`/`r.post`/...) sits at the leaf.
//     A branch matcher that is a class (`String`/`Integer`/`Float`) or a symbol
//     (`:id`) is a dynamic capture — normalised to the `:param` colon form so
//     canonicalisation yields `{param}`. This is best-effort over the dynamic
//     tree: only string-literal / class / symbol branch matchers are composed;
//     regexp and array matchers are skipped (the leaf verb still emits at the
//     prefix accumulated so far).
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Grape
// ---------------------------------------------------------------------------

// grapeGateRe matches a Grape API class definition — the entry-point signal for
// a Grape source. We require `< Grape::API` so the synthesizer no-ops on
// arbitrary Ruby (a bare `get do ... end` would otherwise false-positive).
var grapeGateRe = regexp.MustCompile(`(?m)<\s*Grape::API\b`)

// grapeResourceOpenRe matches a Grape resource/namespace/group prefix block:
//
//	resource :users do
//	resources :widgets do
//	namespace :v1 do
//	group :admin do
//	segment :books do
//	resource 'users' do        (string form)
//
// Capture group 1 = the symbol/string body (the path fragment).
var grapeResourceOpenRe = regexp.MustCompile(
	`(?m)^\s*(?:resources?|namespace|group|segment)\s+` +
		`(?::(\w+)|['"]([^'"\n\r]{1,200})['"])\s+do\b`,
)

// grapeVerbBlockRe matches a Grape verb registration inside an API class:
//
//	get do
//	get ':id' do
//	post '/items' do
//	put ':id' do |id|
//	delete ':id', requirements: { id: /\d+/ } do
//
// The path literal is OPTIONAL (a bare `get do` registers the resource root).
// Capture group 1 = verb, group 2 = path literal (may be empty/absent).
var grapeVerbBlockRe = regexp.MustCompile(
	`(?m)^\s*(get|post|put|patch|delete|head|options)\b` +
		`(?:\s+['"]([^'"\n\r]{0,500})['"])?` +
		`[^\n\r]*?\bdo\b`,
)

// synthesizeGrape scans a Ruby file for Grape verb-block registrations and emits
// one inline-handler endpoint per route. Grape route handlers are ALWAYS
// anonymous blocks, so every route is signalled as an inline handler (#4417,
// mirroring Sinatra #4385). resource/namespace/group prefixes compose onto the
// route path.
func synthesizeGrape(content, path string, emit emitFileFn) {
	if !grapeGateRe.MatchString(content) {
		return
	}
	walkGrapeBlocks(content, "", path, emit)
}

// walkGrapeBlocks recursively emits verb routes at the current pathPrefix, then
// descends into nested resource/namespace/group blocks composing their fragment
// onto the prefix. Routes inside nested prefix blocks are stripped from the body
// scanned at this level so they emit exactly once with the correct prefix
// (mirrors walkSinatraBlocks).
func walkGrapeBlocks(content, pathPrefix, path string, emit emitFileFn) {
	flat := stripGrapeResourceBlocks(content)
	emitGrapeVerbRoutes(flat, pathPrefix, path, emit)
	for _, blk := range findGrapeResourceBlocks(content) {
		childPrefix := joinPathFragments(pathPrefix, "/"+blk.name)
		walkGrapeBlocks(blk.body, childPrefix, path, emit)
	}
}

// emitGrapeVerbRoutes emits one inline-handler endpoint for each verb-block route
// in content, prefixing with pathPrefix before canonicalization.
func emitGrapeVerbRoutes(content, pathPrefix, path string, emit emitFileFn) {
	for _, idx := range grapeVerbBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := ""
		if idx[4] >= 0 {
			raw = content[idx[4]:idx[5]]
		}
		full := pathPrefix
		if raw != "" {
			full = joinPathFragments(pathPrefix, ensureLeadingSlash(raw))
		}
		if full == "" {
			full = "/"
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkGrape, full)
		// #4417 — a Grape route handler is ALWAYS an anonymous block; signal
		// refKind=inlineHandlerRefKind (empty refName) so makeEmit synthesizes a
		// stable `<inline VERB /path>` handler + a same-file IMPLEMENTS bridge.
		// handlerFile is left empty (same-file by construction).
		defLine := lineOfOffset(content, idx[2])
		emit(verb, canonical, "grape", inlineHandlerRefKind, "", "", defLine)
	}
}

// grapeBlock describes one resource/namespace/group prefix block.
type grapeBlock struct {
	name      string // path fragment, e.g. "users" / "v1"
	body      string
	bodyStart int
	bodyEnd   int
}

// findGrapeResourceBlocks returns every top-level resource/namespace/group block
// in content, pairing each opener with its matching `end` via the shared
// do/end balancer (extractRailsBlockBody).
func findGrapeResourceBlocks(content string) []grapeBlock {
	var out []grapeBlock
	i := 0
	for i < len(content) {
		m := grapeResourceOpenRe.FindStringSubmatchIndex(content[i:])
		if m == nil {
			break
		}
		name := ""
		if m[2] >= 0 {
			name = content[i+m[2] : i+m[3]]
		} else if m[4] >= 0 {
			name = content[i+m[4] : i+m[5]]
		}
		bodyStart := i + m[1]
		body, after, ok := extractRubyBlockBody(content, bodyStart)
		if !ok {
			i = i + m[1]
			continue
		}
		out = append(out, grapeBlock{
			name:      name,
			body:      body,
			bodyStart: bodyStart,
			bodyEnd:   bodyStart + len(body),
		})
		i = after
	}
	return out
}

// stripGrapeResourceBlocks blanks the bodies of top-level resource/namespace
// blocks (preserving newlines for line-offset stability) so verb routes inside
// them aren't double-emitted at the un-prefixed parent scope.
func stripGrapeResourceBlocks(content string) string {
	out := []byte(content)
	for _, blk := range findGrapeResourceBlocks(content) {
		for j := blk.bodyStart; j < blk.bodyEnd && j < len(out); j++ {
			if out[j] != '\n' && out[j] != '\r' {
				out[j] = ' '
			}
		}
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// Roda
// ---------------------------------------------------------------------------

// rodaGateRe matches a Roda app class definition. We require `< Roda` so the
// synthesizer no-ops on arbitrary Ruby.
var rodaGateRe = regexp.MustCompile(`(?m)<\s*Roda\b`)

// rodaBranchOpenRe matches a Roda routing-tree branch opener:
//
//	r.on "users" do
//	r.on "api", "v1" do          (multiple string segments)
//	r.is "users", Integer do     (terminal segment + capture)
//	r.on Integer do |id|
//	r.on :id do |id|
//
// The matcher argument list (group 1) is parsed by parseRodaMatchers into path
// segments. The receiver name is flexible (`r`, `req`, ...). Capture group 1 =
// the raw matcher argument list (everything between the method and `do`).
var rodaBranchOpenRe = regexp.MustCompile(
	`(?m)^\s*\w+\.(?:on|is)\b([^\n\r]*?)\bdo\b`,
)

// rodaVerbRe matches a Roda terminal verb at a leaf:
//
//	r.get do
//	r.post do
//	r.get Integer do |id|
//	r.get "x" do
//	r.is "y"; r.get do
//
// Capture group 1 = verb, group 2 = the matcher argument list (may be empty).
var rodaVerbRe = regexp.MustCompile(
	`(?m)\b\w+\.(get|post|put|patch|delete|head|options)\b([^\n\r]*?)\bdo\b`,
)

// synthesizeRoda scans a Ruby file for Roda routing-tree verb registrations and
// emits one inline-handler endpoint per leaf verb. Roda handlers are ALWAYS
// anonymous blocks (#4417). r.on/r.is branch segments compose onto the path;
// the verb sits at the leaf. Best-effort over the dynamic tree.
func synthesizeRoda(content, path string, emit emitFileFn) {
	if !rodaGateRe.MatchString(content) {
		return
	}
	walkRodaBranches(content, "", path, emit)
}

// walkRodaBranches recursively emits leaf verbs at the current pathPrefix, then
// descends into nested r.on/r.is branches composing their segments onto the
// prefix. Branch bodies are stripped from the level-scan so leaf verbs inside
// them emit exactly once at the correct prefix.
func walkRodaBranches(content, pathPrefix, path string, emit emitFileFn) {
	flat := stripRodaBranchBlocks(content)
	emitRodaVerbs(flat, pathPrefix, path, emit)
	for _, blk := range findRodaBranchBlocks(content) {
		childPrefix := joinPathFragments(pathPrefix, blk.segments)
		walkRodaBranches(blk.body, childPrefix, path, emit)
	}
}

// emitRodaVerbs emits one inline-handler endpoint per leaf verb in content. A
// verb may itself carry an inline matcher (`r.get Integer do |id|`) which
// contributes a trailing path segment.
func emitRodaVerbs(content, pathPrefix, path string, emit emitFileFn) {
	for _, idx := range rodaVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		args := ""
		if idx[4] >= 0 {
			args = content[idx[4]:idx[5]]
		}
		seg := parseRodaMatchers(args)
		full := joinPathFragments(pathPrefix, seg)
		if full == "" {
			full = "/"
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkRoda, full)
		defLine := lineOfOffset(content, idx[2])
		emit(verb, canonical, "roda", inlineHandlerRefKind, "", "", defLine)
	}
}

// rodaBranch describes one r.on/r.is branch block.
type rodaBranch struct {
	segments  string // composed path fragment, e.g. "/users" or "/users/:param"
	body      string
	bodyStart int
	bodyEnd   int
}

// findRodaBranchBlocks returns every top-level r.on/r.is branch block in
// content, pairing each opener with its matching `end` via the shared do/end
// balancer. Branch matchers are parsed into a path fragment.
func findRodaBranchBlocks(content string) []rodaBranch {
	var out []rodaBranch
	i := 0
	for i < len(content) {
		m := rodaBranchOpenRe.FindStringSubmatchIndex(content[i:])
		if m == nil {
			break
		}
		args := ""
		if m[2] >= 0 {
			args = content[i+m[2] : i+m[3]]
		}
		bodyStart := i + m[1]
		body, after, ok := extractRubyBlockBody(content, bodyStart)
		if !ok {
			i = i + m[1]
			continue
		}
		out = append(out, rodaBranch{
			segments:  parseRodaMatchers(args),
			body:      body,
			bodyStart: bodyStart,
			bodyEnd:   bodyStart + len(body),
		})
		i = after
	}
	return out
}

// stripRodaBranchBlocks blanks the bodies of top-level r.on/r.is branch blocks
// (preserving newlines) so leaf verbs inside them aren't double-emitted at the
// parent prefix.
func stripRodaBranchBlocks(content string) string {
	out := []byte(content)
	for _, blk := range findRodaBranchBlocks(content) {
		for j := blk.bodyStart; j < blk.bodyEnd && j < len(out); j++ {
			if out[j] != '\n' && out[j] != '\r' {
				out[j] = ' '
			}
		}
	}
	return string(out)
}

// rubyBlockOpenerRe detects a line that OPENS a `do` block. Unlike the Rails
// endsWithDo helper (which only recognises a `do` token at end-of-line, with an
// optional `|args|` suffix and no preceding identifier), this handles the Grape/
// Roda forms that carry tokens before `do` AND a block-param list after it:
//
//	get ':id' do                  → opener
//	r.get Integer do |id|         → opener  (endsWithDo misses this)
//	resource :users do
//	r.on "users" do
//
// It requires the `do` to be a standalone token (word-bounded) followed only by
// optional whitespace, an optional `|params|` list, optional whitespace, and
// (optionally) a trailing comment — i.e. the block body starts on the NEXT line.
// A `do` followed by other code on the same line (`x do y end`) is intentionally
// NOT treated as a multi-line opener here (rare in route DSLs; the inline form is
// handled by the body never being entered).
var rubyBlockOpenerRe = regexp.MustCompile(`\bdo\b(?:\s*\|[^|]*\|)?\s*(?:#.*)?$`)

// extractRubyBlockBody returns the text between the `do` token at start and its
// matching `end` line, plus the byte offset just past the `end`. It is a
// do/end balancer for the Grape/Roda block DSLs that correctly counts openers
// carrying a `do |args|` block-parameter list (which the shared Rails
// extractRailsBlockBody / endsWithDo misses). Comments are stripped before
// opener/closer detection.
func extractRubyBlockBody(content string, start int) (body string, after int, ok bool) {
	depth := 1
	pos := start
	for pos < len(content) {
		newline := strings.IndexByte(content[pos:], '\n')
		var line string
		var lineEnd int
		if newline < 0 {
			line = content[pos:]
			lineEnd = len(content)
		} else {
			line = content[pos : pos+newline]
			lineEnd = pos + newline + 1
		}
		trimmed := strings.TrimSpace(line)
		if hashIdx := strings.IndexByte(trimmed, '#'); hashIdx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:hashIdx])
		}
		if trimmed != "" {
			if rubyBlockOpenerRe.MatchString(trimmed) {
				depth++
			}
			if trimmed == "end" {
				depth--
				if depth == 0 {
					return content[start:pos], lineEnd, true
				}
			}
		}
		pos = lineEnd
		if newline < 0 {
			break
		}
	}
	return "", 0, false
}

// rodaMatcherArgRe pulls individual matcher arguments out of a Roda branch /
// verb argument list. Recognised tokens:
//   - "literal" / 'literal'        → static path segment
//   - String / Integer / Float     → dynamic capture (→ :param)
//   - :symbol                      → dynamic capture (→ :param)
//
// Block params, hashes, regexps and array matchers are deliberately ignored
// (best-effort over the dynamic tree).
var rodaMatcherArgRe = regexp.MustCompile(
	`['"]([^'"\n\r]+)['"]` + // 1: string literal
		`|\b(String|Integer|Float)\b` + // 2: class matcher
		`|:(\w+)`, // 3: symbol matcher
)

// parseRodaMatchers turns a Roda matcher argument list into a composed path
// fragment, e.g. `"users", Integer` → "/users/:param", `"api", "v1"` →
// "/api/v1". Class/symbol captures collapse to the Express `:param` colon form
// so canonicalization yields `{param}`. Returns "" when no matchers are present
// (a bare `r.get do` leaf).
func parseRodaMatchers(args string) string {
	// Stop at a block-param pipe (`do |id|`) — params are not path segments.
	if p := strings.IndexByte(args, '|'); p >= 0 {
		args = args[:p]
	}
	var seg strings.Builder
	for _, m := range rodaMatcherArgRe.FindAllStringSubmatch(args, -1) {
		switch {
		case m[1] != "":
			seg.WriteString("/" + strings.Trim(m[1], "/"))
		case m[2] != "" || m[3] != "":
			seg.WriteString("/:param")
		}
	}
	return seg.String()
}
