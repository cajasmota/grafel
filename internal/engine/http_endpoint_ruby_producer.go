// http_endpoint_ruby_producer.go — Rails routes.rb + Sinatra DSL → http_endpoint_definition synthesis.
//
// Two Ruby web frameworks share this file:
//
//   - Rails  (config/routes.rb)
//     get '/things/search', to: 'things#search'
//     resources :things                          → 7 standard CRUD endpoints
//     namespace :api do
//     resources :widgets                       → prefixed with /api
//     end
//     scope '/v1' do
//     get '/health', to: 'health#index'
//     end
//
//     Handlers live in app/controllers/<name>_controller.rb (Rails convention).
//     The synthesizer derives the expected controller file path from the
//     "users#index" handler reference + the current namespace stack and
//     emits it as a `handler_file` property. The shared resolver rebind
//     (#2680) consumes this property to perform a *path-targeted* same-file
//     lookup, which prevents the global (kind, name) fallback from picking
//     the wrong controller for a method name shared across many controllers
//     (every Rails app has dozens of `index` / `show` / `create` methods).
//
//   - Sinatra (app.rb / inline DSL)
//     get '/things' do
//     ...
//     end
//
//     Handlers are inline blocks in the same file; we emit the endpoint
//     with `def_line` pointing at the verb's line so the resolver
//     post-pass leaves source_file/start_line on the registration site —
//     which IS the handler site by construction.
//
// Refs #2691.
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Fast-path gates
// ---------------------------------------------------------------------------

// railsRoutesGateRe matches the routes.rb signature block. Files without
// this anchor are skipped so the synthesizer doesn't fire on arbitrary Ruby.
var railsRoutesGateRe = regexp.MustCompile(`(?m)\b(?:Rails\.application\.routes\.draw|Routes\.draw)\b`)

// sinatraGateRe matches a Sinatra import. We deliberately require an explicit
// `require 'sinatra'` (or `require "sinatra/base"`) before scanning verb
// blocks — bare `get '/x' do ... end` in arbitrary Ruby would otherwise
// false-positive on Rails routes.rb itself.
var sinatraGateRe = regexp.MustCompile(`(?m)require\s+['"]sinatra(?:/base|/main)?['"]`)

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

// railsVerbRouteRe matches an explicit verb route inside routes.rb:
//
//	get '/things', to: 'things#index'
//	post "/things/:id", to: "things#update"
//	delete '/things/:id'                  (no handler — bare path only)
//
// Capture groups: 1=verb, 2=path, 3=handler ref (e.g. "things#index").
var railsVerbRouteRe = regexp.MustCompile(
	`(?m)^\s*(get|post|put|patch|delete|head|options)\s+` +
		`['"]([^'"\n\r]{1,500})['"]` +
		`(?:\s*,\s*to:\s*['"]([\w/]+#\w+)['"])?`,
)

// railsResourcesRe matches `resources :name [, only: [...]] [, except: [...]]`.
// Capture group 1 = the resource name (symbol body).
var railsResourcesRe = regexp.MustCompile(`(?m)^\s*resources\s+:(\w+)\b`)

// railsResourceSingularRe matches singular `resource :name` (Rails singular
// resource — 6 routes, no index, no :id segment). We emit the same shape as
// resources for now; the dominant convention in real apps is plural.
var railsResourceSingularRe = regexp.MustCompile(`(?m)^\s*resource\s+:(\w+)\b`)

// railsNamespaceOpenRe matches `namespace :name do`. The closing `end`
// is tracked separately by walkRailsBlocks.
var railsNamespaceOpenRe = regexp.MustCompile(`(?m)^\s*namespace\s+:(\w+)\s+do\b`)

// railsScopeOpenRe matches `scope '/prefix' do` (path-only form). The
// `scope module: …` and `scope path: …, module: …` forms are also accepted
// — we look for the first single- or double-quoted string after `scope`.
var railsScopeOpenRe = regexp.MustCompile(
	`(?m)^\s*scope\s+(?:(?:path\s*:\s*)?['"]([^'"\n\r]+)['"]|:(\w+))[^\n\r]*\bdo\b`,
)

// railsBlockEndRe matches a bare `end` line — the close of a namespace /
// scope block. We use a regex (rather than tokenising) because Rails routes
// files are flat and `end` tokens inside string literals are vanishingly
// rare in the wild.
var railsBlockEndRe = regexp.MustCompile(`(?m)^\s*end\s*$`)

// sinatraVerbBlockRe matches a Sinatra verb registration:
//
//	get '/things' do
//	post "/things/:id" do |id|
//
// Capture groups: 1=verb, 2=path.
var sinatraVerbBlockRe = regexp.MustCompile(
	`(?m)^\s*(get|post|put|patch|delete|head|options)\s+` +
		`['"]([^'"\n\r]{1,500})['"]\s*` +
		`(?:\|[^|]*\|\s*)?` +
		`do\b`,
)

// ---------------------------------------------------------------------------
// CRUD expansion tables
// ---------------------------------------------------------------------------

// railsResourcesRoutes are the 7 standard routes emitted by `resources :name`.
// Naming follows Rails' canonical action names so the handler resolves to
// the matching controller method.
var railsResourcesRoutes = []struct{ method, suffix, action string }{
	{"GET", "", "index"},
	{"POST", "", "create"},
	{"GET", "/new", "new"},
	{"GET", "/:id", "show"},
	{"GET", "/:id/edit", "edit"},
	{"PUT", "/:id", "update"},
	{"PATCH", "/:id", "update"},
	{"DELETE", "/:id", "destroy"},
}

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// emitFileFn extends emitFn with a handler-file hint (used by Rails to point
// the resolver at the controller file derived from "users#index") and a
// 1-based def-line (used by Sinatra to anchor the synthetic at the verb
// block's line in the same file). Either may be empty / 0 for callers that
// don't need them.
type emitFileFn func(method, canonicalPath, framework, refKind, refName, handlerFile string, defLine int)

// synthesizeRailsRoutes scans a Ruby file for Rails routes.rb registrations
// and calls emit per (verb, canonical-path, framework, handlerKind,
// handlerName, handlerFile, defLine). routes.rb is matched by the
// `Rails.application.routes.draw` sentinel — non-matching files are skipped.
//
// `routesPath` is the path of the routes.rb file. It's used to derive the
// project root so handler files can be located at `app/controllers/<name>_controller.rb`.
func synthesizeRailsRoutes(content, routesPath string, emit emitFileFn, emitResource emitResourceFn) {
	if !railsRoutesGateRe.MatchString(content) {
		return
	}
	rootDir := deriveRailsRoot(routesPath)
	walkRailsBlocks(content, "", "", rootDir, emit, emitResource)
}

// sinatraNamespaceOpenRe matches a sinatra-contrib `namespace` opener:
//
//	namespace '/api' do
//	namespace "/v1" do
//
// Capture group 1 is the path fragment (string literal). Non-string-literal
// namespaces (e.g. `namespace %r{/foo}` regex routes) are deliberately not
// matched — those are rare and would need regex-route handling out of scope here.
var sinatraNamespaceOpenRe = regexp.MustCompile(
	`(?m)^\s*namespace\s+['"]([^'"\n\r]{1,200})['"]\s*do\b`,
)

// synthesizeSinatra scans a Ruby file for Sinatra verb-block registrations
// and emits one inline-handler endpoint per route. Sinatra route handlers are
// ALWAYS anonymous blocks (`get '/x' do ... end`) — there is no named handler
// method — so every route is signalled as an inline handler (#4385). Supports
// classic top-level routes, modular `class App < Sinatra::Base` routes, and
// sinatra-contrib `namespace '/api' do ... end` path-prefix composition.
func synthesizeSinatra(content, path string, emit emitFileFn) {
	if !sinatraGateRe.MatchString(content) {
		return
	}
	walkSinatraBlocks(content, "", path, emit)
}

// walkSinatraBlocks recursively emits verb routes at the current `pathPrefix`,
// then descends into nested `namespace` blocks composing their path fragment
// onto the prefix. Routes inside namespace blocks are stripped from the body
// scanned at this level so they are emitted exactly once, with the correct
// prefix, by the recursive call (mirrors walkRailsBlocks).
func walkSinatraBlocks(content, pathPrefix, path string, emit emitFileFn) {
	// Emit verb routes that are NOT inside a nested namespace block at this level.
	flat := stripSinatraNamespaceBlocks(content)
	emitSinatraVerbRoutes(flat, pathPrefix, path, emit)
	// Recurse into each nested namespace block with the composed prefix.
	for _, blk := range findSinatraNamespaceBlocks(content) {
		childPrefix := joinPathFragments(pathPrefix, blk.name)
		walkSinatraBlocks(blk.body, childPrefix, path, emit)
	}
}

// emitSinatraVerbRoutes emits one inline-handler endpoint for each verb-block
// route literal in content, prefixing the route with pathPrefix before
// canonicalization.
func emitSinatraVerbRoutes(content, pathPrefix, path string, emit emitFileFn) {
	for _, idx := range sinatraVerbBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		full := raw
		if pathPrefix != "" {
			full = joinPathFragments(pathPrefix, raw)
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, full)
		// def_line points at the verb block's own line; the synthetic's
		// source_file is the registration file (which is also the
		// handler file for Sinatra).
		//
		// #4385 — a Sinatra route handler is ALWAYS an anonymous block
		// (`get '/x' do ... end`); there is no named handler method the
		// resolver could bind to. Signal refKind=inlineHandlerRefKind (empty
		// refName) so makeEmit synthesizes a stable `<inline VERB /path>`
		// handler entity + a same-file IMPLEMENTS bridge, instead of leaving
		// the endpoint a handler-less graph island. handlerFile is left empty
		// on purpose: the block body lives in THIS file, so the same-file
		// synthesis-time bridge is correct and must NOT be dropped as a
		// cross-file hint (emitFile drops the bridge only when handlerFile != "").
		defLine := lineOfOffset(content, idx[2])
		emit(verb, canonical, "sinatra", inlineHandlerRefKind, "", "", defLine)
	}
}

// sinatraBlock describes one `namespace` block discovered in a Sinatra source.
type sinatraBlock struct {
	name      string // path fragment, e.g. "/api"
	body      string // content between `do` and the matching `end`
	bodyStart int    // absolute byte offset of body in the scanned content
	bodyEnd   int    // absolute byte offset just past body
}

// findSinatraNamespaceBlocks returns every top-level `namespace '/x' do ... end`
// block in content, pairing each opener with its matching `end` by counting
// block tokens (reusing extractRailsBlockBody's do/end balancer).
func findSinatraNamespaceBlocks(content string) []sinatraBlock {
	var out []sinatraBlock
	i := 0
	for i < len(content) {
		m := sinatraNamespaceOpenRe.FindStringSubmatchIndex(content[i:])
		if m == nil {
			break
		}
		name := content[i+m[2] : i+m[3]]
		bodyStart := i + m[1]
		body, after, ok := extractRailsBlockBody(content, bodyStart)
		if !ok {
			i = i + m[1]
			continue
		}
		out = append(out, sinatraBlock{
			name:      name,
			body:      body,
			bodyStart: bodyStart,
			bodyEnd:   bodyStart + len(body),
		})
		i = after
	}
	return out
}

// stripSinatraNamespaceBlocks blanks out the bodies of top-level namespace
// blocks (preserving line/offset counts) so verb routes inside them are not
// double-emitted at the un-prefixed parent scope.
func stripSinatraNamespaceBlocks(content string) string {
	out := []byte(content)
	for _, blk := range findSinatraNamespaceBlocks(content) {
		// Blank the body in-place. Preserving newlines (blank only non-newline
		// bytes) keeps lineOfOffset correct for sibling routes at this scope.
		for j := blk.bodyStart; j < blk.bodyEnd && j < len(out); j++ {
			if out[j] != '\n' && out[j] != '\r' {
				out[j] = ' '
			}
		}
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// Rails routes.rb block walker
// ---------------------------------------------------------------------------

// walkRailsBlocks recursively scans `content` for verb routes, resources,
// nested namespaces / scopes. `pathPrefix` and `modulePrefix` are the
// composed prefixes from enclosing blocks: namespace :api contributes both
// "/api" and "Api::" (module prefix maps to controller subdirectory and
// class name in Rails' default conventions).
func walkRailsBlocks(content, pathPrefix, modulePrefix, rootDir string, emit emitFileFn, emitResource emitResourceFn) {
	// Process verb routes at this scope level first. We match across the
	// whole content (no scope-aware filtering) because the dominant
	// convention is to declare verb routes at the same depth as resources
	// — but only when no enclosing block introduces a different prefix.
	// Real-world routes files almost never mix `get` at root scope with a
	// `get` inside `namespace :admin do` for the same path, so this
	// simplification is safe.
	if pathPrefix == "" && modulePrefix == "" {
		// Strip nested `namespace`/`scope` blocks so verb routes inside
		// them don't get double-counted (they'll be re-emitted by the
		// recursive call with the correct prefix).
		flat := stripRailsNestedBlocks(content)
		emitRailsVerbRoutes(flat, "", "", rootDir, emit)
		emitRailsResources(flat, "", "", rootDir, emit, emitResource)
	} else {
		// At a nested level we receive only the body of the enclosing
		// block (already stripped of its own nested children). Match
		// every verb route + resources in this body.
		emitRailsVerbRoutes(content, pathPrefix, modulePrefix, rootDir, emit)
		emitRailsResources(content, pathPrefix, modulePrefix, rootDir, emit, emitResource)
	}
	// Recurse into nested namespace / scope blocks.
	for _, blk := range findRailsBlocks(content) {
		childPath := pathPrefix
		childModule := modulePrefix
		switch blk.kind {
		case "namespace":
			childPath = joinPathFragments(pathPrefix, "/"+blk.name)
			if modulePrefix == "" {
				childModule = capitalize(blk.name)
			} else {
				childModule = modulePrefix + "::" + capitalize(blk.name)
			}
		case "scope_path":
			childPath = joinPathFragments(pathPrefix, blk.name)
		case "scope_module":
			if modulePrefix == "" {
				childModule = capitalize(blk.name)
			} else {
				childModule = modulePrefix + "::" + capitalize(blk.name)
			}
		}
		// Strip THIS block's own nested children before recursing one
		// level deeper so verb routes inside grand-children aren't
		// emitted at the wrong prefix.
		body := stripRailsNestedBlocks(blk.body)
		emitRailsVerbRoutes(body, childPath, childModule, rootDir, emit)
		emitRailsResources(body, childPath, childModule, rootDir, emit, emitResource)
		// Recurse for grand-children.
		walkRailsBlocks(blk.body, childPath, childModule, rootDir, emit, emitResource)
	}
}

// railsBlock describes one namespace / scope block discovered in routes.rb.
type railsBlock struct {
	kind string // "namespace" | "scope_path" | "scope_module"
	name string // "/api" for scope_path, "api" for namespace/scope_module
	body string // content between `do` and matching `end`
}

// findRailsBlocks returns every top-level namespace/scope block in content,
// pairing each `do` with its matching `end` by counting block tokens.
func findRailsBlocks(content string) []railsBlock {
	var out []railsBlock
	// Find the next `namespace ... do` or `scope ... do` opener.
	type opener struct {
		kind, name string
		start, end int // byte offset of the matching `end` line, computed lazily
	}
	// Single pass: walk character by character, when we hit an opener, find
	// its matching `end` by counting any other `do` / `end` tokens between.
	i := 0
	for i < len(content) {
		// Try namespace first, then scope, at the current position.
		if m := railsNamespaceOpenRe.FindStringSubmatchIndex(content[i:]); m != nil && m[0] == 0 {
			body, after, ok := extractRailsBlockBody(content, i+m[1])
			if !ok {
				i = i + m[1]
				continue
			}
			out = append(out, railsBlock{kind: "namespace", name: content[i+m[2] : i+m[3]], body: body})
			i = after
			continue
		}
		if m := railsScopeOpenRe.FindStringSubmatchIndex(content[i:]); m != nil && m[0] == 0 {
			// Path form (group 1) or module form (group 2)?
			kind := "scope_path"
			var name string
			if m[2] >= 0 {
				name = content[i+m[2] : i+m[3]]
				if !strings.HasPrefix(name, "/") {
					name = "/" + name
				}
			} else if m[4] >= 0 {
				kind = "scope_module"
				name = content[i+m[4] : i+m[5]]
			}
			body, after, ok := extractRailsBlockBody(content, i+m[1])
			if !ok {
				i = i + m[1]
				continue
			}
			out = append(out, railsBlock{kind: kind, name: name, body: body})
			i = after
			continue
		}
		i++
	}
	return out
}

// extractRailsBlockBody returns the text between the `do` token at start and
// its matching `end` line, plus the byte offset just past the `end`. Nested
// `do`/`end` pairs are tracked so blocks like `namespace :a do; namespace :b
// do; end; end` close at the correct point.
//
// Detection of `do` / `end` is simplified: we treat any line starting with
// `end` (after whitespace) as a closer, and any line containing the regex
// matches in railsNamespaceOpenRe / railsScopeOpenRe / a `do` token at end
// of line followed by newline as an opener. This is sufficient for Rails
// routes.rb, which is by convention a flat declarative DSL.
func extractRailsBlockBody(content string, start int) (body string, after int, ok bool) {
	depth := 1
	// Walk line by line so we can use the end-of-line regex anchors.
	pos := start
	for pos < len(content) {
		// Find next line.
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
		// Count openers on this line: any `... do` at end of line OR a
		// `do |...|` pattern. We treat namespace/scope openers and bare
		// `do` blocks uniformly: if the line ends with `do` or `do |…|`,
		// it opens a block.
		trimmed := strings.TrimSpace(line)
		// Strip trailing comment.
		if hashIdx := strings.IndexByte(trimmed, '#'); hashIdx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:hashIdx])
		}
		if trimmed != "" {
			if endsWithDo(trimmed) {
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

// endsWithDo reports whether a trimmed routes.rb line ends with a `do`
// token (with or without a `|args|` block-parameter list).
func endsWithDo(line string) bool {
	if strings.HasSuffix(line, " do") || line == "do" {
		return true
	}
	// `do |x, y|` form — strip the `|...|` and re-check.
	if idx := strings.LastIndexByte(line, '|'); idx > 0 {
		if pre := strings.TrimSpace(line[:idx]); strings.HasSuffix(pre, "|") {
			// Find the preceding `|`.
			j := strings.IndexByte(pre[:len(pre)-1], '|')
			if j > 0 {
				before := strings.TrimSpace(pre[:j])
				if strings.HasSuffix(before, " do") || before == "do" {
					return true
				}
			}
		}
	}
	return false
}

// stripRailsNestedBlocks returns content with every top-level namespace /
// scope block replaced by an equivalent number of newlines (so line offsets
// are preserved). Used so verb routes inside a nested block aren't
// double-counted when emitting the outer scope's routes.
func stripRailsNestedBlocks(content string) string {
	if !strings.Contains(content, " do") {
		return content
	}
	var b strings.Builder
	b.Grow(len(content))
	i := 0
	for i < len(content) {
		blocked := false
		for _, re := range []*regexp.Regexp{railsNamespaceOpenRe, railsScopeOpenRe} {
			if m := re.FindStringSubmatchIndex(content[i:]); m != nil && m[0] == 0 {
				_, after, ok := extractRailsBlockBody(content, i+m[1])
				if !ok {
					break
				}
				// Replace the [i, after) range with newlines preserving count.
				for j := i; j < after; j++ {
					if content[j] == '\n' {
						b.WriteByte('\n')
					} else {
						b.WriteByte(' ')
					}
				}
				i = after
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		b.WriteByte(content[i])
		i++
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Per-line emitters
// ---------------------------------------------------------------------------

// emitRailsVerbRoutes scans content for explicit verb routes and calls emit.
// pathPrefix is prepended to the matched path; modulePrefix is used when the
// `to:` handler reference is namespaced via a current `namespace`/`scope
// module:` block (it modifies the controller file path).
func emitRailsVerbRoutes(content, pathPrefix, modulePrefix, rootDir string, emit emitFileFn) {
	for _, m := range railsVerbRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		raw := m[2]
		fullPath := joinPathFragments(pathPrefix, ensureLeadingSlash(raw))
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, fullPath)
		// Handler ref: "controller#action" → Controller class + method name.
		// Namespace prefix is applied to the controller path lookup.
		handlerKind, handlerName, handlerFile := "", "", ""
		if len(m) >= 4 && m[3] != "" {
			handlerKind, handlerName, handlerFile = parseRailsHandlerRef(m[3], modulePrefix, rootDir)
		}
		emit(verb, canonical, "rails", handlerKind, handlerName, handlerFile, 0)
	}
}

// emitRailsResources expands `resources :name` and `resource :name` macros
// into the 7 (or 6) canonical CRUD routes.
func emitRailsResources(content, pathPrefix, modulePrefix, rootDir string, emit emitFileFn, emitResource emitResourceFn) {
	for _, m := range railsResourcesRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		base := joinPathFragments(pathPrefix, "/"+name)
		for _, r := range railsResourcesRoutes {
			raw := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
			handlerKind, handlerName, handlerFile := resolveRailsAction(name, r.action, modulePrefix, rootDir)
			// T10 #3842 — stamp framework_synthesized provenance + per-action
			// effective contract (create→201, destroy→204, ...) via emitResource.
			emitResource(r.method, canonical, "rails_resources", handlerKind, handlerName, handlerFile, r.action)
		}
	}
	for _, m := range railsResourceSingularRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		base := joinPathFragments(pathPrefix, "/"+name)
		// Singular: 6 routes (no index, no :id segment since it's a single resource).
		singular := []struct{ method, suffix, action string }{
			{"POST", "", "create"},
			{"GET", "/new", "new"},
			{"GET", "", "show"},
			{"GET", "/edit", "edit"},
			{"PUT", "", "update"},
			{"PATCH", "", "update"},
			{"DELETE", "", "destroy"},
		}
		for _, r := range singular {
			raw := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
			handlerKind, handlerName, handlerFile := resolveRailsAction(name, r.action, modulePrefix, rootDir)
			emitResource(r.method, canonical, "rails_resource", handlerKind, handlerName, handlerFile, r.action)
		}
	}
}

// ---------------------------------------------------------------------------
// Handler-ref / controller-file derivation
// ---------------------------------------------------------------------------

// parseRailsHandlerRef parses a Rails `to:` handler reference of the form
// "controller#action" or "namespace/controller#action" into the
// (handlerKind="SCOPE.Operation", handlerName=<action>, handlerFile=<controller .rb path>).
//
// Rails convention: 'users#index' → UsersController#index in
// app/controllers/users_controller.rb. The Ruby extractor emits methods
// with bare names (Name="index"), so the qualified name is just the
// action; the resolver uses handlerFile to disambiguate when the same
// action name appears in multiple controllers.
func parseRailsHandlerRef(ref, modulePrefix, rootDir string) (kind, name, file string) {
	hash := strings.IndexByte(ref, '#')
	if hash <= 0 || hash == len(ref)-1 {
		return "", "", ""
	}
	controllerPath := ref[:hash]
	action := ref[hash+1:]
	// "namespace/controller" segments map to subdirectories under
	// app/controllers/. Module prefix from `namespace :api do` similarly
	// maps to a subdirectory.
	parts := strings.Split(controllerPath, "/")
	dir := "app/controllers"
	if modulePrefix != "" {
		dir = filepath.Join(dir, strings.ToLower(strings.ReplaceAll(modulePrefix, "::", "/")))
	}
	for i := 0; i < len(parts)-1; i++ {
		dir = filepath.Join(dir, parts[i])
	}
	leaf := parts[len(parts)-1] + "_controller.rb"
	rel := filepath.Join(dir, leaf)
	if rootDir != "" {
		rel = filepath.Join(rootDir, rel)
	}
	return "SCOPE.Operation", action, filepath.ToSlash(rel)
}

// resolveRailsAction derives the controller file path + action name for a
// resources expansion. `resourceName` is the (plural) resource symbol;
// `action` is one of index/show/create/update/destroy/new/edit.
func resolveRailsAction(resourceName, action, modulePrefix, rootDir string) (kind, name, file string) {
	// `resources :things` → ThingsController in app/controllers/things_controller.rb.
	dir := "app/controllers"
	if modulePrefix != "" {
		dir = filepath.Join(dir, strings.ToLower(strings.ReplaceAll(modulePrefix, "::", "/")))
	}
	leaf := resourceName + "_controller.rb"
	rel := filepath.Join(dir, leaf)
	if rootDir != "" {
		rel = filepath.Join(rootDir, rel)
	}
	return "SCOPE.Operation", action, filepath.ToSlash(rel)
}

// deriveRailsRoot walks up from routesPath until the parent of `config/`
// (i.e. the Rails project root). Returns the project root with forward
// slashes; empty when no `config/` ancestor is found (which would be
// abnormal — routes.rb should always live in config/).
func deriveRailsRoot(routesPath string) string {
	norm := filepath.ToSlash(routesPath)
	idx := strings.LastIndex(norm, "/config/")
	if idx < 0 {
		// File is named config/routes.rb at repo root.
		if strings.HasPrefix(norm, "config/") {
			return ""
		}
		// Fallback: assume the parent dir of the file's directory is root.
		return filepath.ToSlash(filepath.Dir(filepath.Dir(norm)))
	}
	return norm[:idx]
}

// capitalize uppercases the first byte of s. Used to convert a Ruby symbol
// (`:api`) into the corresponding module name (`Api`) for namespace
// composition.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// ensureLeadingSlash prepends "/" when missing so joinPathFragments composes
// correctly regardless of whether the source omitted the leading slash
// (`get 'things'` is legal Rails, equivalent to `get '/things'`).
func ensureLeadingSlash(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		return "/" + p
	}
	return p
}
