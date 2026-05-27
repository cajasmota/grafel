// http_endpoint_phoenix.go — Elixir Phoenix router → http_endpoint_definition synthesis.
//
// Phoenix declares routes in a `router.ex` file as a block of `scope` /
// `pipe_through` / verb / `resources` calls:
//
//	scope "/api", MyAppWeb do
//	    pipe_through :api
//	    get "/users", UserController, :index
//	    post "/users", UserController, :create
//	    resources "/widgets", WidgetController
//	end
//
// The action functions (`def index(...)`, `def create(...)`, ...) live in
// the matching controller file, e.g. `controllers/user_controller.ex`,
// inside `defmodule MyAppWeb.UserController do`. The Elixir extractor
// emits each `def` as `SCOPE.Operation:<func>` (no module qualification),
// so a naive (kind, name) cross-file lookup would resolve `index` to the
// FIRST `def index` found anywhere in the merged set — which collides
// across controllers in any non-trivial app.
//
// Disambiguation: we stamp the controller module's snake_case basename
// (e.g. `user_controller`) on the synthetic as a `handler_file` property
// hint. The shared resolver (http_endpoint_resolve.go) consumes the hint
// as a SUBSTRING match against every candidate handler's source_file when
// the exact-file lookup misses — the #2692 extension of the #2691 Rails
// `handler_file` mechanism, generalised to bare-basename hints.
//
// Coverage:
//
//   - `get|post|put|patch|delete|head|options "/path", MyController, :action`
//   - `resources "/widgets", WidgetController`               — 7 CRUD verbs
//   - `resources "/widgets", WidgetController, only: [:index, :show]`
//   - `resources "/widgets", WidgetController, except: [:delete]`
//   - `scope "/prefix", MyAppWeb do ... end` — prefix is prepended; the
//     module alias on the scope line is recorded so unqualified
//     `MyController` references in the body can be resolved against it
//     (not currently needed — we only consume the basename).
//   - Nested scopes — prefixes compose by string concatenation.
//
// Out of scope (phase 1):
//
//   - `forward "/admin", AdminPlug`                       — Plug forwards
//   - `live "/path", LiveView, :live_action`              — Phoenix LiveView
//   - `pipe_through` semantics — informational only; we do not gate
//     synthesis on pipeline membership.
//
// Refs #2692.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// phoenixVerbRouteRe matches `<verb> "/path", MyController, :action`
// (with optional trailing kwargs).
//
// Capture groups:
//
//	1 = verb
//	2 = path
//	3 = controller module (may be qualified with `.` like MyAppWeb.UserController)
//	4 = action atom name
var phoenixVerbRouteRe = regexp.MustCompile(
	`(?m)^\s*(get|post|put|patch|delete|head|options)\s+` +
		`"([^"\r\n]+)"\s*,\s*` +
		`([A-Za-z_][\w.]*)\s*,\s*` +
		`:([A-Za-z_]\w*)`,
)

// phoenixResourcesRe matches `resources "/path", MyController` with
// optional kwargs. The `only:` / `except:` lists are extracted from the
// kwargs blob captured in group 3 by parsePhoenixResourceFilter.
//
// Capture groups:
//
//	1 = path
//	2 = controller module
//	3 = trailing args (may be empty or contain `only:` / `except:` / `do` etc.)
var phoenixResourcesRe = regexp.MustCompile(
	`(?m)^\s*resources\s+"([^"\r\n]+)"\s*,\s*([A-Za-z_][\w.]*)([^\r\n]*)`,
)

// phoenixScopeRe matches `scope "/prefix", MaybeModule do` and `scope MaybeModule do`.
//
// Capture groups:
//
//	1 = optional quoted prefix (may be empty when only the module form is used)
var phoenixScopeRe = regexp.MustCompile(
	`(?m)^\s*scope\s+(?:"([^"\r\n]*)"|[A-Za-z_][\w.]*)` +
		`(?:\s*,\s*[A-Za-z_][\w.]*)?` +
		`(?:\s*,\s*\[[^\]]*\])?\s*do\b`,
)

// phoenixEndRe matches a bare `end` keyword at the start of a line. We use
// it to close `scope ... do` blocks for prefix composition.
var phoenixEndRe = regexp.MustCompile(`(?m)^\s*end\s*$`)

// phoenixResourceCRUDActions enumerates the 7 standard actions
// `resources` generates plus the verb + path-suffix each implies.
// The action name doubles as the disambiguation hint when looking up the
// controller-module's `def <action>` entity.
var phoenixResourceCRUDActions = []struct{ action, verb, suffix string }{
	{"index", "GET", ""},
	{"new", "GET", "/new"},
	{"create", "POST", ""},
	{"show", "GET", "/{id}"},
	{"edit", "GET", "/{id}/edit"},
	{"update", "PUT", "/{id}"},
	{"delete", "DELETE", "/{id}"},
}

// phoenixHasRouter is a fast pre-filter: any non-router file lacks both
// `Phoenix.Router` and the `scope` / verb / `resources` macros.
func phoenixHasRouter(content string) bool {
	if !strings.Contains(content, "Phoenix.Router") &&
		!strings.Contains(content, "use Phoenix.Router") &&
		!strings.Contains(content, "scope ") {
		return false
	}
	return strings.Contains(content, "get ") || strings.Contains(content, "post ") ||
		strings.Contains(content, "put ") || strings.Contains(content, "patch ") ||
		strings.Contains(content, "delete ") || strings.Contains(content, "resources ") ||
		strings.Contains(content, "head ") || strings.Contains(content, "options ")
}

// phoenixControllerHint converts a Phoenix controller module reference
// (possibly qualified with `MyAppWeb.`) into the snake_case basename the
// matching controller file uses. Example:
//
//	MyAppWeb.UserController        → user_controller
//	WidgetController               → widget_controller
//	MyAppWeb.Admin.UserController  → user_controller
//
// The trailing `Controller` suffix is preserved (Phoenix file naming
// convention includes it) and the camel-cased prefix is converted to
// snake_case with an underscore before every capital except the first.
func phoenixControllerHint(modRef string) string {
	last := modRef
	if i := strings.LastIndexByte(modRef, '.'); i >= 0 {
		last = modRef[i+1:]
	}
	return camelToSnake(last)
}

// camelToSnake converts CamelCase / PascalCase to snake_case. Consecutive
// upper-case letters (e.g. "HTTPServer") are preserved as a single run with
// a single leading underscore (`http_server`). Non-letter characters are
// passed through verbatim.
func camelToSnake(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	runes := []rune(s)
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		if isUpper && i > 0 {
			prev := runes[i-1]
			prevUpper := prev >= 'A' && prev <= 'Z'
			// Insert an underscore on every uppercase boundary except
			// inside an all-caps run (HTTPServer → http_server).
			if !prevUpper {
				b.WriteByte('_')
			} else if i+1 < len(runes) {
				next := runes[i+1]
				if next >= 'a' && next <= 'z' {
					// "HTTPServer" — emit underscore before the start of the
					// next word's leading capital ('S' before 'erver').
					b.WriteByte('_')
				}
			}
		}
		if isUpper {
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parsePhoenixResourceFilter inspects the trailing kwargs blob of a
// `resources "...", Ctrl, only: [:show, :index]` declaration and returns
// the filter set + filter mode:
//
//	mode "only"   — emit only the listed actions
//	mode "except" — emit all actions except the listed ones
//	mode ""       — no filter, emit all 7
//
// The returned set contains action atoms (without the leading colon).
func parsePhoenixResourceFilter(args string) (mode string, set map[string]bool) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", nil
	}
	for _, kw := range []string{"only", "except"} {
		needle := kw + ":"
		idx := strings.Index(args, needle)
		if idx < 0 {
			continue
		}
		tail := args[idx+len(needle):]
		open := strings.IndexByte(tail, '[')
		close := strings.IndexByte(tail, ']')
		if open < 0 || close < 0 || close <= open {
			continue
		}
		body := tail[open+1 : close]
		out := map[string]bool{}
		for _, tok := range strings.Split(body, ",") {
			tok = strings.TrimSpace(tok)
			tok = strings.TrimPrefix(tok, ":")
			if tok != "" {
				out[tok] = true
			}
		}
		return kw, out
	}
	return "", nil
}

// phoenixScopePrefixAt returns the active scope prefix that should be
// prepended to a route declaration at byte offset `off`. We walk every
// `scope ... do` opener and `end` closer in the file and maintain a
// running prefix stack. The current stack at `off` is concatenated.
//
// This is a single linear scan over the file content — O(n) regardless
// of how many routes the file declares.
func phoenixScopePrefixAt(content string, off int) string {
	type evt struct {
		offset int
		open   bool
		prefix string
	}
	var events []evt
	for _, m := range phoenixScopeRe.FindAllStringSubmatchIndex(content, -1) {
		prefix := ""
		if len(m) >= 4 && m[2] >= 0 {
			prefix = content[m[2]:m[3]]
		}
		events = append(events, evt{offset: m[0], open: true, prefix: prefix})
	}
	for _, m := range phoenixEndRe.FindAllStringIndex(content, -1) {
		events = append(events, evt{offset: m[0], open: false})
	}
	// Sort by offset.
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j-1].offset > events[j].offset; j-- {
			events[j-1], events[j] = events[j], events[j-1]
		}
	}
	var stack []string
	for _, e := range events {
		if e.offset >= off {
			break
		}
		if e.open {
			stack = append(stack, e.prefix)
		} else if len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	var b strings.Builder
	for _, p := range stack {
		b.WriteString(p)
	}
	return b.String()
}

// phoenixEmitFn extends emitFn with the controller-module file hint so
// the synth can stamp `handler_file` on the synthetic without coupling
// to the dispatch-internal emitFile closure shape. The dispatch wraps
// emit to set the property after emission.
type phoenixEmitFn func(method, canonicalPath, framework, refKind, refName, fileHint string)

// synthesizePhoenix scans an Elixir router file for Phoenix route
// declarations and calls emit for each (verb, canonical-path, framework,
// handlerKind, handlerName, fileHint) tuple. The resolver consumes
// fileHint via the `handler_file` property to cross-file-rebind
// source_file / start_line to the matching `def <action>` (#2680, #2692).
func synthesizePhoenix(content string, emit phoenixEmitFn) {
	if !phoenixHasRouter(content) {
		return
	}

	// --- Explicit verb routes ---
	for _, m := range phoenixVerbRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		modRef := content[m[6]:m[7]]
		action := content[m[8]:m[9]]

		prefix := phoenixScopePrefixAt(content, m[0])
		full := joinPathFragments(prefix, raw)
		canonical := httproutes.Canonicalize(httproutes.FrameworkPhoenix, full)
		if canonical == "" {
			continue
		}
		emit(verb, canonical, "phoenix", "SCOPE.Operation", action, phoenixControllerHint(modRef))
	}

	// --- resources → up to 7 CRUD endpoints ---
	for _, m := range phoenixResourcesRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		raw := content[m[2]:m[3]]
		modRef := content[m[4]:m[5]]
		args := content[m[6]:m[7]]

		mode, filter := parsePhoenixResourceFilter(args)
		prefix := phoenixScopePrefixAt(content, m[0])
		base := joinPathFragments(prefix, raw)
		hint := phoenixControllerHint(modRef)

		for _, r := range phoenixResourceCRUDActions {
			switch mode {
			case "only":
				if !filter[r.action] {
					continue
				}
			case "except":
				if filter[r.action] {
					continue
				}
			}
			path := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkPhoenix, path)
			if canonical == "" {
				continue
			}
			emit(r.verb, canonical, "phoenix_resources", "SCOPE.Operation", r.action, hint)
		}
	}
}
