// Endpoint synthesis for the Go "trio" of router frameworks that
// follow-up issues #2684 / #2685 / #2686 surfaced as unsupported during
// the #2678 audit:
//
//   - gorilla/mux:  r.HandleFunc("/path", handler).Methods("GET")
//   - net/http:     http.HandleFunc("/path", handler)
//     mux.HandleFunc("/path", handler)         // Go 1.22 ServeMux
//     mux.HandleFunc("GET /users/{id}", h)      // Go 1.22 method prefix
//   - huma:         huma.Register(api, huma.Operation{Method: "GET", Path: "/x"}, h)
//
// All three frameworks share the same producer-side contract as the
// existing gin/echo/chi/fiber synthesizers (#2682): emit one
// http_endpoint per (verb, path, handler) tuple with a
// `source_handler="Controller:<name>"` reference so the shared resolver
// in http_endpoint_resolve.go can rebind source_file / source_line to
// the handler def. The synthesizers therefore only have to extract the
// three tuple components; the rebind side is already framework-agnostic.
//
// Each synthesizer is import-guarded to keep the false-positive rate
// near zero: the regexes use generic method-call shapes that would
// otherwise match unrelated code (e.g. `mux.HandleFunc` inside an
// unrelated wrapper, or `huma.Register` aliased to something else).
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// isGoInlineHandlerToken reports whether a handler token captured by a Go HTTP
// route registration regex is actually the start of an ANONYMOUS / INLINE func
// literal (`func(...) {...}`) rather than a real, addressable handler symbol.
//
// The route regexes capture the handler argument as a `[\w.]+` identifier; when
// the argument is `func(w, r) {...}` the greedy group captures the bare `func`
// keyword. `func` is a Go reserved word and can never be a handler name, so it
// is an unambiguous, name-agnostic signal that the handler is inline. Callers
// turn this into refKind="InlineHandler" so makeEmit synthesizes a stable
// inline-handler entity + merge-stable IMPLEMENTS bridge (#4382, mirroring the
// JS fix #4324) instead of leaving the endpoint a handler-less graph island.
func isGoInlineHandlerToken(handler string) bool {
	return handler == "func"
}

// ---------------------------------------------------------------------------
// gorilla/mux — issue #2684
// ---------------------------------------------------------------------------

// gorillaRouteRe matches a gorilla/mux route registration:
//
//	r.HandleFunc("/path", handler).Methods("GET")
//	api.HandleFunc("/users/{id}", getUser).Methods("GET", "POST")
//	sub.HandleFunc("/x", h.Create).Methods(http.MethodPut)
//
// Group 1 is the path, group 2 is the handler identifier, group 3 is
// the raw .Methods(...) argument list. A subsequent regex parses the
// argument list into individual verbs.
//
// The .Methods(...) suffix is OPTIONAL — gorilla treats a HandleFunc
// without .Methods as accepting any verb. When absent we emit an "ANY"
// endpoint so the route is still discoverable; downstream cross-repo
// matchers treat ANY as wildcard-compatible (see
// http_endpoint_match.go).
var gorillaRouteRe = regexp.MustCompile(
	`\b\w+\s*\.\s*HandleFunc\s*\(\s*` +
		"`" + `?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]` + "`" + `?\s*,\s*([\w.]+)\s*\)` +
		`(?:\s*\.\s*Methods\s*\(([^)]*)\))?`,
)

// gorillaVerbRe extracts each verb literal from the .Methods(...) arg
// list. Accepts both `"GET"` and `http.MethodGet` shapes.
var gorillaVerbRe = regexp.MustCompile(
	`(?:["` + "`" + `](GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)["` + "`" + `])` +
		`|` +
		`(?:http\.Method(Get|Post|Put|Patch|Delete|Head|Options))`,
)

// goFileUsesGorillaMux reports whether the file imports gorilla/mux.
// Used to gate the .HandleFunc synthesizer because `HandleFunc` is also
// the net/http stdlib name — without an import gate the two synthesizers
// would double-emit on the same call site.
func goFileUsesGorillaMux(content string) bool {
	return strings.Contains(content, "github.com/gorilla/mux")
}

// synthesizeGorillaMux scans a Go file for gorilla/mux route
// registrations and emits one synthetic http_endpoint per (verb, path)
// pair. When .Methods(...) lists multiple verbs (e.g. GET + POST on the
// same path), one synthetic per verb is emitted so each entry has a
// distinct canonical ID.
//
// Each synthetic is stamped with the 1-based line number of the
// `.HandleFunc(...)` call in the source file. The shared resolver in
// http_endpoint_resolve.go uses that line to populate
// `registration_start_line` before rebinding StartLine to the handler
// def line — so the route mount-point stays discoverable after the
// rebind.
func synthesizeGorillaMux(content string, emit emitDefFn) {
	if !goFileUsesGorillaMux(content) {
		return
	}
	for _, m := range gorillaRouteRe.FindAllStringSubmatchIndex(content, -1) {
		// FindAllStringSubmatchIndex returns 2*N indices: start/end for
		// the whole match and for each capture group. Groups 1-3 carry
		// path, handler, methods-arg.
		if len(m) < 8 {
			continue
		}
		rawPath := content[m[2]:m[3]]
		handler := content[m[4]:m[5]]
		var methodsArg string
		if m[6] >= 0 {
			methodsArg = content[m[6]:m[7]]
		}
		if i := strings.LastIndex(handler, "."); i >= 0 {
			handler = handler[i+1:]
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, rawPath)
		regLine := lineOfOffset(content, m[0])

		verbs := parseGorillaVerbs(methodsArg)
		if len(verbs) == 0 {
			// .Methods(...) absent or empty → gorilla accepts any verb.
			verbs = []string{"ANY"}
		}
		for _, verb := range verbs {
			emit(verb, canonical, "gorilla", "Controller", handler, regLine)
		}
	}
	synthesizeGorillaMuxInline(content, emit)
}

// gorillaInlineRouteRe matches a gorilla/mux route whose handler is an
// ANONYMOUS / INLINE func literal:
//
//	r.HandleFunc("/widgets", func(w http.ResponseWriter, r *http.Request) {
//	r.HandleFunc("/x", func(w, r) { ... }).Methods("GET")
//
// The named-handler regex (gorillaRouteRe) requires `handler)` immediately
// after the path and so cannot match a func literal (the `)` belongs to the
// func signature, and the registration `)` only appears after the multi-line
// body). This regex anchors on the `func(` opener instead. Group 1 is the path;
// the .Methods(...) verb list — which on a multi-line inline handler appears
// after the body, out of regex reach — is recovered by scanning forward to the
// statement-terminating `)` (see verbsForGorillaInline). #4382.
var gorillaInlineRouteRe = regexp.MustCompile(
	`\b\w+\s*\.\s*HandleFunc\s*\(\s*` +
		"`" + `?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]` + "`" + `?\s*,\s*func\s*\(`,
)

// gorillaInlineMethodsRe finds the `.Methods("GET", http.MethodPost, …)` suffix
// that may trail an inline gorilla/mux handler after the func body's closing
// brace, within the forward window the caller passes.
var gorillaInlineMethodsRe = regexp.MustCompile(`\.\s*Methods\s*\(([^)]*)\)`)

// synthesizeGorillaMuxInline emits endpoints for gorilla/mux routes whose
// handler is an inline func literal. Each is signalled InlineHandler so makeEmit
// synthesizes a stable inline-handler node + bridge (#4382). The verb is taken
// from a trailing `.Methods(...)` when present (scanning past the func body),
// else ANY — matching gorilla's wildcard semantics for an un-Methods'd route.
func synthesizeGorillaMuxInline(content string, emit emitDefFn) {
	for _, m := range gorillaInlineRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		rawPath := content[m[2]:m[3]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, rawPath)
		regLine := lineOfOffset(content, m[0])

		verbs := verbsForGorillaInline(content, m[1])
		if len(verbs) == 0 {
			verbs = []string{"ANY"}
		}
		for _, verb := range verbs {
			emit(verb, canonical, "gorilla", inlineHandlerRefKind, "", regLine)
		}
	}
}

// verbsForGorillaInline recovers the verb list from a `.Methods(...)` suffix on
// an inline-handler registration. Starting at the `func(` opener offset, it
// finds the matching close of the func literal body and looks for a `.Methods(`
// immediately after; returns the parsed verbs, or nil when absent.
func verbsForGorillaInline(content string, funcOpenOffset int) []string {
	brace := strings.IndexByte(content[funcOpenOffset:], '{')
	if brace < 0 {
		return nil
	}
	bodyOpen := funcOpenOffset + brace
	bodyEnd := findMatchingBracket(content, bodyOpen)
	if bodyEnd < 0 {
		return nil
	}
	// The registration closes with `)` after the func body; the optional
	// `.Methods(...)` follows that. Scan a small forward window past the body.
	rest := content[bodyEnd:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		// Allow the `.Methods(...)` on the same line as the closing `})`.
		if line := rest[:nl]; gorillaInlineMethodsRe.MatchString(line) {
			mm := gorillaInlineMethodsRe.FindStringSubmatch(line)
			return parseGorillaVerbs(mm[1])
		}
	}
	if mm := gorillaInlineMethodsRe.FindStringSubmatch(rest); mm != nil {
		// Only honour a `.Methods(...)` that appears before the next route
		// registration so we don't borrow a sibling route's verbs.
		methodsIdx := strings.Index(rest, mm[0])
		nextReg := strings.Index(rest, "HandleFunc")
		if nextReg < 0 || methodsIdx < nextReg {
			return parseGorillaVerbs(mm[1])
		}
	}
	return nil
}

// parseGorillaVerbs extracts verb strings from a .Methods(...) argument
// list. Returns a deduplicated, upper-cased slice in the order each verb
// first appears. An empty input yields an empty slice (caller decides
// the default behavior).
func parseGorillaVerbs(arg string) []string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range gorillaVerbRe.FindAllStringSubmatch(arg, -1) {
		var v string
		switch {
		case m[1] != "":
			v = strings.ToUpper(m[1])
		case m[2] != "":
			v = strings.ToUpper(m[2])
		}
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// ---------------------------------------------------------------------------
// net/http stdlib — issue #2685
// ---------------------------------------------------------------------------

// stdlibHandleFuncRe matches net/http stdlib HandleFunc registrations:
//
//	http.HandleFunc("/x", handler)               // package-level mux
//	mux.HandleFunc("/x", handler)                // local *http.ServeMux
//	mux.HandleFunc("GET /users/{id}", handler)    // Go 1.22+ method prefix
//
// Group 1 is the pattern (which may contain a verb prefix in Go 1.22+),
// group 2 is the handler identifier.
//
// Note: this regex *also* matches `r.HandleFunc("/x", h)` on a
// gorilla/mux router. The synthesizer is gated on the absence of a
// gorilla/mux import so the two paths never run on the same file.
var stdlibHandleFuncRe = regexp.MustCompile(
	`\b\w+\s*\.\s*HandleFunc\s*\(\s*` +
		"`" + `?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]` + "`" + `?\s*,\s*([\w.]+)`,
)

// stdlibMethodPrefixRe parses a Go 1.22+ "VERB /path" pattern string
// into (verb, path). The verb prefix is optional in Go 1.22; when
// absent the pattern is the full path and the verb is ANY.
var stdlibMethodPrefixRe = regexp.MustCompile(
	`^\s*(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(.+)$`,
)

// goFileUsesNetHTTPStdlib reports whether the file looks like it
// registers handlers on the net/http stdlib mux. We accept either the
// canonical `"net/http"` import (which everyone has) plus at least one
// of the two registration entry points: the package-level
// `http.HandleFunc(...)` call OR a call to `http.NewServeMux()` whose
// result is then `HandleFunc`'d. Gating on these markers (rather than
// the import alone) avoids false-positives in every Go file that
// happens to import net/http for an unrelated reason.
func goFileUsesNetHTTPStdlib(content string) bool {
	if goFileUsesGorillaMux(content) {
		// gorilla/mux's HandleFunc shadows the stdlib one in the regex;
		// let synthesizeGorillaMux handle this file exclusively.
		return false
	}
	if !strings.Contains(content, `"net/http"`) {
		return false
	}
	return strings.Contains(content, "http.HandleFunc(") ||
		strings.Contains(content, "http.NewServeMux(") ||
		strings.Contains(content, "ServeMux")
}

// synthesizeNetHTTPStdlib scans a Go file for net/http stdlib route
// registrations and emits one synthetic http_endpoint per call. Handles
// the Go 1.22+ "VERB /path" method-prefix syntax when present;
// otherwise emits the endpoint with verb=ANY. The 1-based line number
// of the .HandleFunc call is stamped on each synthetic so the resolver
// rebind can stash it as `registration_start_line`.
func synthesizeNetHTTPStdlib(content string, emit emitDefFn) {
	if !goFileUsesNetHTTPStdlib(content) {
		return
	}
	for _, m := range stdlibHandleFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		pattern := content[m[2]:m[3]]
		handler := content[m[4]:m[5]]
		if i := strings.LastIndex(handler, "."); i >= 0 {
			handler = handler[i+1:]
		}
		regLine := lineOfOffset(content, m[0])

		verb := "ANY"
		path := pattern
		if mp := stdlibMethodPrefixRe.FindStringSubmatch(pattern); mp != nil {
			verb = strings.ToUpper(mp[1])
			path = strings.TrimSpace(mp[2])
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, path)
		// #4382 — `http.HandleFunc("/x", func(w, r) {...})`: the `([\w.]+)`
		// handler group captured the bare `func` literal keyword, not a real
		// handler symbol. Signal InlineHandler so makeEmit synthesizes a stable
		// inline-handler node + bridge instead of a dangling `Controller:func`
		// ref that leaves the endpoint a graph island.
		refKind := "Controller"
		if isGoInlineHandlerToken(handler) {
			handler = ""
			refKind = inlineHandlerRefKind
		}
		emit(verb, canonical, "nethttp", refKind, handler, regLine)
	}
}

// ---------------------------------------------------------------------------
// huma — issue #2686
// ---------------------------------------------------------------------------

// humaRegisterRe matches a huma.Register(...) call. The Operation
// struct literal is matched with a non-greedy body; we then extract
// Method and Path from the body in a second pass so field ordering
// inside the struct doesn't matter.
//
// Example:
//
//	huma.Register(api, huma.Operation{
//	    Method: http.MethodGet,
//	    Path:   "/users/{id}",
//	    Summary: "Get user",
//	}, handleGetUser)
//
// Group 1 is the Operation{...} body text, group 2 is the handler
// identifier (third argument to Register).
var humaRegisterRe = regexp.MustCompile(
	`\bhuma\s*\.\s*Register\s*\(\s*\w+\s*,\s*(?:&\s*)?huma\.Operation\s*\{([\s\S]*?)\}\s*,\s*([\w.]+)\s*\)`,
)

// humaMethodFieldRe extracts the Method field value from an Operation
// struct body. Accepts both `Method: "GET"` and `Method: http.MethodGet`.
var humaMethodFieldRe = regexp.MustCompile(
	`\bMethod\s*:\s*(?:["` + "`" + `](GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)["` + "`" + `]` +
		`|http\.Method(Get|Post|Put|Patch|Delete|Head|Options))`,
)

// humaPathFieldRe extracts the Path field value from an Operation body.
var humaPathFieldRe = regexp.MustCompile(
	`\bPath\s*:\s*` + "`" + `?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]` + "`" + `?`,
)

// goFileUsesHuma reports whether the file imports the huma library.
// Both v1 (danielgtaylor/huma) and v2 (danielgtaylor/huma/v2) ship the
// same huma.Register entry point with the Operation struct.
func goFileUsesHuma(content string) bool {
	return strings.Contains(content, "danielgtaylor/huma")
}

// synthesizeHuma scans a Go file for huma.Register(...) calls and emits
// one synthetic http_endpoint per call. Operations missing either
// Method or Path are skipped (they would fail at huma runtime too, so
// they're not real endpoints). The 1-based line number of the
// huma.Register(...) call is stamped on each synthetic so the resolver
// rebind can stash it as `registration_start_line`.
func synthesizeHuma(content string, emit emitDefFn) {
	if !goFileUsesHuma(content) {
		return
	}
	for _, m := range humaRegisterRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		opBody := content[m[2]:m[3]]
		handler := content[m[4]:m[5]]
		if i := strings.LastIndex(handler, "."); i >= 0 {
			handler = handler[i+1:]
		}
		regLine := lineOfOffset(content, m[0])

		verb := humaVerbFromBody(opBody)
		if verb == "" {
			continue
		}
		pathMatch := humaPathFieldRe.FindStringSubmatch(opBody)
		if pathMatch == nil {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, pathMatch[1])
		emit(verb, canonical, "huma", "Controller", handler, regLine)
	}
}

// humaVerbFromBody parses the Method field out of an Operation struct
// body. Returns "" when no recognizable Method field is present.
func humaVerbFromBody(body string) string {
	m := humaMethodFieldRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	switch {
	case m[1] != "":
		return strings.ToUpper(m[1])
	case m[2] != "":
		return strings.ToUpper(m[2])
	}
	return ""
}
