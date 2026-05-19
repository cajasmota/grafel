// Client-side (consumer) synthetic http_endpoint emission for typed-HTTP
// cross-repo matching (issue #533, Phase 1 + template-literal Phase 2 +
// wrapper-recognition Phase 3 (#651)).
//
// Producer-side (#534 Phase 1/2) emits one synthetic `http:<METHOD>:<path>`
// entity per backend route. This file is the symmetric consumer pass: for
// every detectable HTTP client call (fetch, axios, requests, httpx,
// aiohttp), we emit the SAME synthetic-shaped entity from the caller's
// file with the caller recorded as a property. The cross-repo import
// linker (links/import_pass.go) already matches `http_endpoint` entities
// by Name across repos, so emitting the consumer side is sufficient to
// land HTTP cross-repo links — no new linker code is required.
//
// Phase 1 covers STATIC URL literals:
//   - JS/TS:   fetch("/users/123"), fetch("/users/123", {method:"POST"}),
//     axios.<verb>("/path", ...), httpClient.<verb>("/path", ...)
//   - Python:  requests.<verb>("/path"), httpx.<verb>("/path"),
//     aiohttp.ClientSession.<verb>("/path"), session.<verb>("/path")
//
// Phase 2 (this file) adds TEMPLATE-LITERAL URL extraction for JS/TS:
//   - fetch(`/users/${id}/checklists`) → http:GET:/users/{id}/checklists (#706)
//   - axios.post(`/api/v1/users/${userId}`, body) → http:POST:/api/v1/users/{userId} (#706)
//   - Simple constant folding: const API_BASE = "/api/v1"; fetch(`${API_BASE}/users`)
//     → resolves API_BASE to "/api/v1" → http:GET:/api/v1/users
//   - ${user.id} → {id} (last property segment); ${user?.id} → {id} (optional chain)
//   - ${userId as string} → {userId} (TypeScript cast stripped)
//   - Complex expressions (function calls, subscripts) → {param} fallback.
//
// Still deferred to later chain-fixes:
//   - URL builders: const u = new URL(...); fetch(u)
//   - Axios instance binding: const api = axios.create({baseURL}); api.get(p)
//   - React Query / SWR key arrays as URL surrogates
//   - SDK chain calls (typed clients)
//   - Curl / wget shell invocations
//   - Env-variable-only URLs
//
// Properties emitted on the synthetic:
//   - verb         — uppercase HTTP method
//   - path         — canonical path with `{name}` params
//   - framework    — "fetch" / "axios" / "http_client" / "requests" /
//     "httpx" / "aiohttp"
//   - pattern_type — "http_endpoint_client_synthesis"
//   - source_caller — present when the call sits inside a detectable
//     enclosing function. Format `Function:<name>`. The
//     existing resolver (`ResolveHTTPEndpointHandlers`)
//     ignores synthetics that lack `source_handler`, so
//     using a different property key keeps consumer-side
//     synthetics out of the producer-side resolver's
//     drop path; they fall into NoHandlerProp and pass
//     through untouched.
//
// No edges are emitted in this PR. CALLS-edge wiring from caller →
// synthetic is deferred to a later phase (it requires the AST-stamped
// EntityID of the enclosing function, which isn't available at this
// point in the pipeline).
//
// Refs #533 Phase 1.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// JS / TS: fetch + axios + generic <name>.<verb>(url) http clients
// ---------------------------------------------------------------------------

// fetchCallRe matches `fetch("path", ...)` and `fetch('path', ...)` and
// `fetch(\`path\`, ...)`. The path group captures the literal STRING
// content (no template substitution — those are Phase 2). The optional
// options group is captured separately so we can pick up an explicit
// `method: "POST"` setting.
//
// NB: we tolerate an arbitrary number of intervening chars (up to the
// closing `)` on the same statement) by matching non-greedy on the
// options blob. That blob may itself contain nested braces, so we do not
// try to balance them — we just look for a `method:` token inside.
var fetchCallRe = regexp.MustCompile(
	`(?:^|[^\w$.])fetch\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r$]+)['"` + "`" + `](\s*,\s*\{[^}]*\})?`,
)

// fetchMethodRe extracts the verb from a fetch options literal of the form
// `{ method: "POST", ... }`. Case-insensitive on the key; quoted value is
// canonicalised to upper-case by the caller.
var fetchMethodRe = regexp.MustCompile(
	`method\s*:\s*['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]`,
)

// axiosVerbCallRe matches `axios.<verb>("path", ...)` and any axios-like
// client instance whose call site looks like `<ident>.<verb>("path", ...)`
// where verb is one of the HTTP verbs. The leading identifier is captured
// so we can prefer the literal "axios" framework label when present.
//
// We deliberately do NOT trigger on the bare `<ident>.<verb>("...")`
// pattern unless the ident is `axios`, an `axios.create()` instance name
// hint, or a `*HttpClient` / `*Client` identifier. Otherwise this regex
// would collide with Express's `app.get("/p", handler)` route
// registrations, which are producer-side (#534).
//
// To avoid that collision cleanly we run TWO matchers:
//  1. axiosLiteralRe  — anchors on the literal `axios.`
//  2. axiosClientRe   — anchors on `<ident>Client.` / `<ident>HttpClient.`
//     / `httpClient.` / `apiClient.`
//
// Producer-side (Express) idiomatic forms (`app.get`, `router.get`,
// `<router>.get`) do not match either anchor.
var axiosLiteralRe = regexp.MustCompile(
	`\baxios\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r$]+)['"` + "`" + `]`,
)
var axiosClientRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*(?:HttpClient|Client|httpClient|apiClient))\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r$]+)['"` + "`" + `]`,
)

// enclosingJSFuncRe is a coarse heuristic to attribute a call site to the
// nearest preceding named function definition. JS/TS supports many
// function-declaration shapes; we recognise the four most common:
//   - function foo(
//   - const foo = (
//   - const foo = function(
//   - foo: function( (object-literal methods)
//   - async function foo(
//
// We scan the file once and build a sorted list of (offset, name) records,
// then a binary-search-free linear walk to find the nearest preceding
// definition. Good enough for Phase 1 attribution; a Phase 2 chain-fix
// can swap this for AST-derived spans.
var jsFuncDeclRe = regexp.MustCompile(
	`(?m)(?:^|[^\w$])(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(|(?m)(?:^|[^\w$])(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\(`,
)

// ---------------------------------------------------------------------------
// Phase 4 (#712) — bare const-variable path resolution
// ---------------------------------------------------------------------------
//
// Handles calls where the URL argument is a bare identifier (not a quoted
// string or template literal), e.g.:
//
//	const BASE_PATH = "/buildings/";
//	$http.get(BASE_PATH, { params: {...} })   // ← this case
//
// The identifier is resolved via the file-local const symbol table built
// by buildJSConstantSymbolTable. If it maps to a URL-path string, an
// endpoint is emitted. This covers the "plain const-path" miss class (#712).
//
// bareIdentCallRe matches `<receiver>.<verb>( <IDENT> [, ...] )` where:
//   - receiver is any JS identifier (we filter by instance table / $-prefix
//     / known HTTP-client naming in the loop)
//   - verb is one of the HTTP verbs
//   - the first argument is a bare identifier (no quotes, no backtick, no
//     parentheses — those are string literals, template literals, and calls)
//
// Capture groups:
//
//	1 = receiver identifier
//	2 = HTTP verb
//	3 = bare identifier (path variable name)
//
// The leading `(?:^|[^\w$.])` boundary avoids matching the trailing half
// of a dotted chain like `foo.bar.get(PATH)` (it fires on foo.bar but the
// `bar.get` portion still matches because `bar` itself is a word-char
// preceded by a dot boundary miss). We accept that minor over-match and
// rely on the symbol-table lookup to reject non-path identifiers.
var bareIdentCallRe = regexp.MustCompile(
	`(?:^|[^\w$.])(` +
		`\$?[A-Za-z_$][\w$]*` + // receiver (group 1)
		`)` +
		`\s*\.\s*(get|post|put|patch|delete|head|options)` + // verb (group 2)
		`(?:\s*<[^<>()]*>)?` + // optional TS generic
		`\s*\(\s*([A-Za-z_$][\w$]*)` + // bare identifier (group 3)
		`\s*(?:[,)])`, // end: comma (more args) or close-paren (only arg)
)

// ---------------------------------------------------------------------------
// JS / TS: template-literal URL extraction (Phase 2)
// ---------------------------------------------------------------------------

// fetchTemplateLiteralRe matches fetch(`...`) where the argument is a
// template literal containing at least one ${...} substitution.
//
// Capture groups:
//  1. the raw template body (content between the outermost backticks,
//     excluding the backticks themselves). We do a single-line scan and
//     stop at the first newline-free closing backtick after the opening
//     one. Multiline template literals whose path spans multiple lines are
//     uncommon in URL context and are left for a later phase.
//  2. optional options object (`,{...}`) to extract the HTTP method.
//
// The [^`\n\r]*\$\{[^`\n\r]* pattern requires at least one ${…} sequence so
// we only match actual template strings, not plain backtick strings (those
// are covered by fetchCallRe already).
var fetchTemplateLiteralRe = regexp.MustCompile(
	"(?:^|[^\\w$.])fetch\\s*\\(\\s*`([^`\\n\\r]*\\$\\{[^`\\n\\r]*)`(\\s*,\\s*\\{[^}]*\\})?",
)

// axiosLiteralTemplateLiteralRe matches axios.<verb>(`...${...}...`, ...).
var axiosLiteralTemplateLiteralRe = regexp.MustCompile(
	"\\baxios\\s*\\.\\s*(get|post|put|patch|delete|head|options)\\s*\\(\\s*`([^`\\n\\r]*\\$\\{[^`\\n\\r]*)`",
)

// axiosClientTemplateLiteralRe matches <ident>Client.<verb>(`...${...}...`).
var axiosClientTemplateLiteralRe = regexp.MustCompile(
	"\\b([A-Za-z_$][\\w$]*(?:HttpClient|Client|httpClient|apiClient))\\s*\\.\\s*(get|post|put|patch|delete|head|options)\\s*\\(\\s*`([^`\\n\\r]*\\$\\{[^`\\n\\r]*)`",
)

// jsConstStringRe matches simple string-literal const / let / var
// declarations: `const NAME = "/value"` or `const NAME = '/value'`.
// Used to build a lightweight constant-folding symbol table.
//
// Capture groups: 1=name, 2=value (without quotes).
var jsConstStringRe = regexp.MustCompile(
	`(?m)(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*['"]([^'"]{1,256})['"]`,
)

// buildJSConstantSymbolTable returns a map from identifier name → string
// value for every simple string-literal const declaration in the file.
// Used by canonicalizeTemplateLiteral for constant folding.
// Only single-line string assignments are captured; complex expressions
// and computed values are ignored (unknown variables fold to {param}).
func buildJSConstantSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range jsConstStringRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := content[m[2]:m[3]]
		value := content[m[4]:m[5]]
		if _, dup := syms[name]; !dup {
			syms[name] = value
		}
	}
	return syms
}

// templateSubstRe matches ${<expression>} inside a template literal.
// We capture the full expression inside ${...} for further analysis.
var templateSubstRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// jsIdentRe matches a valid JS/TS identifier (no dots, brackets, parens, etc.).
var jsIdentRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// tsCastRe strips a TypeScript `as <Type>` suffix from an expression.
// e.g. `userId as string` → `userId`, `user.id as unknown as string` → `user.id`.
var tsCastRe = regexp.MustCompile(`\s+as\s+\S+.*$`)

// extractParamName returns the best placeholder name for a template-literal
// interpolation expression, or "" if the expression is too complex to name
// (fall back to {param}).
//
// Rules (applied in order):
//  1. Strip TypeScript type casts:  `userId as string` → `userId`
//  2. Strip optional-chain markers: `user?.id` → `user.id`
//  3. Plain identifier:             `userId` → `userId`
//  4. Property access (dotted):     `user.id` / `obj.prop.sub` → last segment (`id`, `sub`)
//  5. Anything with `(` or `[`:     function call / subscript → return ""
func extractParamName(expr string) string {
	expr = strings.TrimSpace(expr)

	// Strip TypeScript type casts (e.g. `userId as string`, `id as unknown as number`).
	expr = tsCastRe.ReplaceAllString(expr, "")
	expr = strings.TrimSpace(expr)

	// Strip optional-chain markers (?.) — treat `user?.id` as `user.id`.
	expr = strings.ReplaceAll(expr, "?.", ".")

	// Reject complex expressions: function calls or array subscripts.
	if strings.ContainsAny(expr, "([") {
		return ""
	}

	// If expression contains a dot, take the last segment.
	if dot := strings.LastIndexByte(expr, '.'); dot >= 0 {
		last := expr[dot+1:]
		if jsIdentRe.MatchString(last) {
			return last
		}
		return ""
	}

	// Plain identifier.
	if jsIdentRe.MatchString(expr) {
		return expr
	}

	return ""
}

// canonicalizeTemplateLiteral converts a raw template-literal body (the
// content between backticks) into a canonical URL path suitable for
// cross-repo matching. Each `${expr}` substitution is either:
//   - Resolved to its constant string value from syms (constant folding), or
//   - Replaced with a `{name}` placeholder where `name` is derived from the
//     expression identifier (issue #706), or
//   - Replaced with `{param}` when the expression is too complex to name.
//
// Placeholder naming rules (constant folding has highest priority):
//  1. `${userId}` → `{userId}` (plain identifier)
//  2. `${user.id}` → `{id}` (last property segment)
//  3. `${user?.id}` → `{id}` (optional-chain, stripped)
//  4. `${userId as string}` → `{userId}` (TypeScript cast, stripped)
//  5. `${getUserId()}` → `{param}` (function call, fallback)
//  6. `${arr[0]}` → `{param}` (subscript, fallback)
//
// The resulting string is stripped of any host prefix (via stripURLHost) and
// validated by looksLikeURLPathOrParam before being returned. Returns ("", false)
// when the template does not look like a URL path.
func canonicalizeTemplateLiteral(tmpl string, syms map[string]string) (string, bool) {
	// Replace each ${expr} with its constant value or a named placeholder.
	result := templateSubstRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		// Extract the expression inside ${...}.
		inner := match[2 : len(match)-1]
		// Trim whitespace.
		inner = strings.TrimSpace(inner)

		// For simple identifiers: look up in the constant symbol table.
		// For member expressions (e.g. `obj.field`), try the full expr
		// first, then the leading identifier.
		if val, ok := syms[inner]; ok {
			return val
		}
		// Try just the leading identifier of a dotted expression.
		if dot := strings.IndexByte(inner, '.'); dot > 0 {
			if val, ok := syms[inner[:dot]]; ok {
				return val
			}
		}

		// Constant folding didn't apply — derive a semantic placeholder from
		// the expression identifier (#706).
		if name := extractParamName(inner); name != "" {
			return "{" + name + "}"
		}

		// Fallback for complex expressions (function calls, subscripts, etc.).
		return "{param}"
	})

	// Strip host prefix for absolute URLs.
	result = stripURLHost(result)

	// Validate that this looks like a URL path (absolute) or a
	// template-parameter-prefixed path (starts with {<name>}).
	if !looksLikeURLPathOrParam(result) {
		return "", false
	}

	return result, true
}

// looksLikeURLPathOrParam extends looksLikeURLPath to also accept paths
// that start with a {param} placeholder. These arise when the first segment
// of the template literal is a substitution whose value is unknown, e.g.:
//
//	fetch(`${BASE}/users/${id}`)  →  {param}/users/{param}
//
// The resulting path starts with `{param}` rather than `/` because the
// constant BASE was not resolvable. We still emit these so cross-repo
// matching has something to work with; the linker normalises leading slashes.
func looksLikeURLPathOrParam(s string) bool {
	if looksLikeURLPath(s) {
		return true
	}
	s = strings.TrimSpace(s)
	// Accept {param}/... or {param} alone.
	if strings.HasPrefix(s, "{") {
		return true
	}
	return false
}

// synthesizeFetchAxios scans a JS/TS file and emits one synthetic
// http_endpoint per detected client call. Handles both static string literals
// (Phase 1) and template literals with ${...} substitutions (Phase 2).
func synthesizeFetchAxios(content string, emit emitFn) {
	if !strings.Contains(content, "fetch(") &&
		!strings.Contains(content, "axios.") &&
		!strings.Contains(content, "axios(") &&
		!strings.Contains(content, "Client.") &&
		!strings.Contains(content, "httpClient.") &&
		!strings.Contains(content, "apiClient.") &&
		!strings.Contains(content, "endpoint:") &&
		!strings.Contains(content, "endpoint :") &&
		!strings.Contains(content, "axios.create") &&
		!strings.Contains(content, "$") {
		return
	}

	funcs := indexJSEnclosingFunctions(content)
	// Build constant symbol table once for the whole file (used by template
	// literal folding below).
	syms := buildJSConstantSymbolTable(content)

	// fetch(...) — static string literals
	for _, m := range fetchCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		// FindAllStringSubmatchIndex returns 2*(N+1) ints. m[0..1] is the
		// full match, m[2..3] is group 1 (path), m[4..5] is group 2 (opts).
		path := content[m[2]:m[3]]
		verb := "GET"
		if len(m) >= 6 && m[4] >= 0 {
			opts := content[m[4]:m[5]]
			if mv := fetchMethodRe.FindStringSubmatch(opts); len(mv) >= 2 {
				verb = strings.ToUpper(mv[1])
			}
		}
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, stripURLHost(path))
		emit(verb, canonical, "fetch", "Function", caller)
	}

	// fetch(`...${...}...`, ...) — template literal URLs
	for _, m := range fetchTemplateLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		tmpl := content[m[2]:m[3]]
		verb := "GET"
		if len(m) >= 6 && m[4] >= 0 {
			opts := content[m[4]:m[5]]
			if mv := fetchMethodRe.FindStringSubmatch(opts); len(mv) >= 2 {
				verb = strings.ToUpper(mv[1])
			}
		}
		path, ok := canonicalizeTemplateLiteral(tmpl, syms)
		if !ok {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "fetch", "Function", caller)
	}

	// axios.<verb>(...) — static string literals
	for _, m := range axiosLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		path := content[m[4]:m[5]]
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, stripURLHost(path))
		emit(verb, canonical, "axios", "Function", caller)
	}

	// axios.<verb>(`...${...}...`) — template literal URLs
	for _, m := range axiosLiteralTemplateLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		tmpl := content[m[4]:m[5]]
		path, ok := canonicalizeTemplateLiteral(tmpl, syms)
		if !ok {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "axios", "Function", caller)
	}

	// <ident>{HttpClient,Client,httpClient,apiClient}.<verb>(...) — static string literals
	for _, m := range axiosClientRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		path := content[m[6]:m[7]]
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, stripURLHost(path))
		emit(verb, canonical, "http_client", "Function", caller)
	}

	// <ident>{HttpClient,Client,httpClient,apiClient}.<verb>(`...${...}...`) — template literal URLs
	for _, m := range axiosClientTemplateLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		tmpl := content[m[6]:m[7]]
		path, ok := canonicalizeTemplateLiteral(tmpl, syms)
		if !ok {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "http_client", "Function", caller)
	}

	// -----------------------------------------------------------------
	// Phase 3 (#651): custom HTTP wrapper functions
	// -----------------------------------------------------------------
	//
	// Real frontends route HTTP calls through a project-specific wrapper
	// (e.g. `callApi({ endpoint: "/users/5", method: "POST" }, ...)`).
	// We do NOT hardcode the wrapper name — instead we detect by SHAPE:
	// any function call whose first argument is an object literal with an
	// `endpoint:` / `url:` / `path:` / `route:` key whose value is a
	// string literal or template literal.
	synthesizeWrapperCalls(content, funcs, syms, emit)

	// -----------------------------------------------------------------
	// Phase 3 (#651): named axios-instance method calls
	// -----------------------------------------------------------------
	//
	// 1. Build a per-file symbol table of `const X = axios.create({...})`
	//    declarations (with optional baseURL).
	// 2. Match `X.<verb>(url, ...)` for any X in the table.
	// 3. Also match `$<ident>.<verb>(url, ...)` (dollar-prefixed axios
	//    instances imported from elsewhere — common Angular/Vue/RN
	//    convention).
	//
	// When the instance has a known baseURL, prepend it to the path so
	// frontend↔backend cross-repo matching survives prefix differences.
	instances := buildAxiosInstanceTable(content)
	synthesizeAxiosInstanceCalls(content, funcs, syms, instances, emit)

	// -----------------------------------------------------------------
	// Phase 4 (#712): bare const-variable path resolution
	// -----------------------------------------------------------------
	//
	// Handles `$http.get(BASE_PATH)` and `instance.get(PATH_VAR)` where
	// the path is a file-local string constant (not a quoted literal).
	// The symbol table already exists from the template-literal phase.
	synthesizeBareIdentifierCalls(content, funcs, syms, instances, emit)
}

// ---------------------------------------------------------------------------
// Phase 3 (#651) — custom HTTP wrapper recognition
// ---------------------------------------------------------------------------

// wrapperCallStartRe locates the START of a `<ident>(<obj-literal>...`
// invocation. Capture group 1 is the wrapper function identifier; the
// regex consumes up to the opening `{` of the object literal. From there
// we walk the source manually to balance braces and parens — this lets us
// handle nested template-literal substitutions like
// `{ url: ` + "`" + `${BASE}/${id}` + "`" + ` }` whose value contains
// `${...}` (which itself contains `{}`).
//
// The leading `(?:^|[^\w$.])` boundary keeps us from matching the trailing
// half of a member-expression call like `foo.bar({...})` (we never want
// to pick up dotted receivers — those are method calls handled by other
// matchers).
var wrapperCallStartRe = regexp.MustCompile(
	`(?:^|[^\w$.])([A-Za-z_$][\w$]*)\s*\(\s*\{`,
)

// wrapperEndpointKeyRe extracts a URL-bearing key/value pair from a
// flat object-literal body. The key must be one of the canonical wrapper
// shapes: endpoint / url / path / route. The value must be a string
// literal or template literal (in single, double, or backtick quotes).
// Capture groups: 1 = key name; 2/3/4 = value (one of single/double/backtick).
var wrapperEndpointKeyRe = regexp.MustCompile(
	"(?:^|[,\\s{])(endpoint|url|path|route)\\s*:\\s*(?:'([^'\\n\\r]+)'|\"([^\"\\n\\r]+)\"|`([^`\\n\\r]+)`)",
)

// wrapperMethodKeyRe extracts a `method:` property value from the object
// literal. Accepts string literals OR dotted constants like
// `HTTP_METHODS.GET` (in which case the trailing identifier is the verb).
// Capture groups: 1 = quoted string verb; 2 = dotted-constant trailing
// identifier (e.g. GET from HTTP_METHODS.GET).
var wrapperMethodKeyRe = regexp.MustCompile(
	`(?:^|[,\s{])method\s*:\s*(?:['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]|[A-Za-z_$][\w$]*\.([A-Za-z]+))`,
)

// wrapperPositionalMethodRe extracts a method passed as a 2nd positional
// argument to the wrapper, after the object literal. Accepts a quoted
// string or a dotted constant. Used by the callApi-style 3-arg form:
//
//	callApi({endpoint: "..."}, HTTP_METHODS.POST, body)
//	callApi({endpoint: "..."}, "POST", body)
//
// Capture groups: 1 = quoted verb; 2 = dotted-constant trailing identifier.
var wrapperPositionalMethodRe = regexp.MustCompile(
	`^\s*,\s*(?:['"` + "`" + `]([A-Za-z]+)['"` + "`" + `]|[A-Za-z_$][\w$]*\.([A-Za-z]+))`,
)

// wrapperBlocklist contains identifier names that LOOK like wrapper
// invocations (they're called with an object-literal first arg) but are
// known not to be HTTP wrappers. We keep this list small and surgical;
// the obj-literal shape + URL-key requirement already filters >99% of
// non-HTTP callsites.
var wrapperBlocklist = map[string]bool{
	"if":         true,
	"for":        true,
	"while":      true,
	"switch":     true,
	"return":     true,
	"throw":      true,
	"new":        true,
	"typeof":     true,
	"instanceof": true,
	"await":      true,
	"async":      true,
	"function":   true,
	// Common non-HTTP fns that take an object first arg:
	"Object":   true,
	"assign":   true,
	"setState": true,
	"useState": true,
	"useMemo":  true,
}

// synthesizeWrapperCalls scans for custom HTTP wrapper invocations and
// emits one synthetic per call. Detection is shape-based (object-literal
// first arg with an `endpoint:`/`url:`/`path:`/`route:` URL key) so it
// works regardless of the project-specific wrapper name (`callApi`,
// `api`, `request`, `http`, `client`, etc.).
func synthesizeWrapperCalls(content string, funcs []jsFuncSpan, syms map[string]string, emit emitFn) {
	for _, m := range wrapperCallStartRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		wrapper := content[m[2]:m[3]]
		if wrapperBlocklist[wrapper] {
			continue
		}
		// m[1] is the position of the opening `{` of the object literal
		// (last byte consumed by the regex). Walk forward to find the
		// matching `}`, then continue to the next `)` to close the call.
		braceOpen := m[1] - 1
		if braceOpen < 0 || braceOpen >= len(content) || content[braceOpen] != '{' {
			continue
		}
		braceClose := findMatchingBrace(content, braceOpen)
		if braceClose < 0 {
			continue
		}
		objBody := content[braceOpen+1 : braceClose]

		// Find the closing `)` of the wrapper call. Bounded scan: up to
		// 256 bytes past the obj literal.
		rest := ""
		parenClose := findMatchingParenAfter(content, braceClose+1, 1024)
		if parenClose > braceClose+1 {
			rest = content[braceClose+1 : parenClose]
		}

		// Must have a URL-bearing key.
		urlMatch := wrapperEndpointKeyRe.FindStringSubmatch(objBody)
		if len(urlMatch) == 0 {
			continue
		}

		// Pull whichever quoting style was used.
		rawURL := ""
		isTemplate := false
		switch {
		case urlMatch[2] != "":
			rawURL = urlMatch[2]
		case urlMatch[3] != "":
			rawURL = urlMatch[3]
		case urlMatch[4] != "":
			rawURL = urlMatch[4]
			isTemplate = true
		}
		if rawURL == "" {
			continue
		}

		// Resolve URL to a canonical path.
		var path string
		var ok bool
		if isTemplate && strings.Contains(rawURL, "${") {
			path, ok = canonicalizeTemplateLiteral(rawURL, syms)
		} else {
			candidate := stripURLHost(rawURL)
			if looksLikeURLPath(candidate) {
				path = candidate
				ok = true
			}
		}
		if !ok {
			continue
		}

		// Determine the verb. Precedence:
		//   1. `method:` key inside the object literal
		//   2. 2nd positional argument after the object literal
		//   3. default GET
		verb := "GET"
		if mm := wrapperMethodKeyRe.FindStringSubmatch(objBody); len(mm) > 0 {
			if mm[1] != "" {
				verb = strings.ToUpper(mm[1])
			} else if mm[2] != "" {
				verb = strings.ToUpper(mm[2])
			}
		} else if rest != "" {
			if mm := wrapperPositionalMethodRe.FindStringSubmatch(rest); len(mm) > 0 {
				if mm[1] != "" {
					verb = strings.ToUpper(mm[1])
				} else if mm[2] != "" {
					verb = strings.ToUpper(mm[2])
				}
			}
		}

		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "http_wrapper", "Function", caller)
	}
}

// ---------------------------------------------------------------------------
// Phase 3 (#651) — named axios-instance method calls (incl. $-prefix)
// ---------------------------------------------------------------------------

// axiosCreateRe matches `const X = axios.create({...})` or `let`/`var`.
// Captures: 1 = instance name; 2 = options-object body (may be empty).
// We restrict the options body to a flat literal (no nested braces) since
// real-world axios.create configs are flat property bags. baseURL embedded
// inside a deeper struct will not be folded — those callsites still emit
// (with no baseURL prefix).
var axiosCreateRe = regexp.MustCompile(
	`(?m)(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*axios\s*\.\s*create\s*\(\s*\{([^{}]*)\}\s*\)`,
)

// axiosCreateBaseURLRe extracts `baseURL: "<value>"` from an axios.create
// options object. The value must be a static string literal — template
// literals with substitutions are NOT supported here (the baseURL becomes
// empty in that case).
var axiosCreateBaseURLRe = regexp.MustCompile(
	`(?:^|[,\s{])baseURL\s*:\s*['"` + "`" + `]([^'"` + "`" + `\n\r$]+)['"` + "`" + `]`,
)

// axiosInstance records the per-file metadata we keep for each
// axios.create() result. Used to (a) recognise `instance.<verb>(...)`
// calls regardless of identifier name, and (b) prepend baseURL when
// emitting endpoints so cross-repo matching survives a frontend that
// uses bare paths while the backend exposes a prefixed mount.
type axiosInstance struct {
	name    string
	baseURL string
}

// buildAxiosInstanceTable scans the file for axios.create() declarations
// and returns a map from instance-name → metadata.
func buildAxiosInstanceTable(content string) map[string]axiosInstance {
	out := make(map[string]axiosInstance)
	if !strings.Contains(content, "axios.create") {
		return out
	}
	for _, m := range axiosCreateRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := content[m[2]:m[3]]
		opts := content[m[4]:m[5]]
		base := ""
		if bm := axiosCreateBaseURLRe.FindStringSubmatch(opts); len(bm) >= 2 {
			base = stripURLHost(bm[1])
		}
		out[name] = axiosInstance{name: name, baseURL: base}
	}
	return out
}

// axiosInstanceCallRe matches `<ident>.<verb>(<arg>, ...)` for any
// identifier (we filter by instance table in the loop). The path argument
// may be a string literal OR a template literal. The leading `[^\w$.]`
// boundary keeps us from cross-firing on member-of-member expressions
// like `foo.bar.get(...)` (still allowed: leading boundary is matched on
// the first dot's left side).
//
// Capture groups:
//
//	1 = receiver identifier (may be `$`-prefixed)
//	2 = HTTP verb
//	3 = URL string literal (single/double quotes) OR empty if backtick
//	4 = URL template-literal body (backtick) OR empty if string
var axiosInstanceCallRe = regexp.MustCompile(
	"(?:^|[^\\w$.])(\\$?[A-Za-z_$][\\w$]*)\\s*\\.\\s*(get|post|put|patch|delete|head|options)\\s*(?:<[^<>()]*>)?\\s*\\(\\s*(?:['\"]([^'\"\\n\\r$]+)['\"]|`([^`\\n\\r]+)`)",
)

// dollarPrefixedHTTPRe is a narrowed view of axiosInstanceCallRe used to
// fall back on $-prefixed receivers when no in-file axios.create()
// declaration exists. This matches the gfleet/Angular/Vue pattern where
// `$http` is exported from a separate module.
//
// We require the dollar prefix specifically to avoid lighting up on
// ordinary local variables — the $-prefix is an idiomatic marker for
// "imported axios-like client" across Angular ($http), Vue 2 ($axios),
// and some React/RN projects.
var dollarPrefixedHTTPRe = regexp.MustCompile(
	"(?:^|[^\\w$.])(\\$[A-Za-z][\\w$]*)\\s*\\.\\s*(get|post|put|patch|delete|head|options)\\s*(?:<[^<>()]*>)?\\s*\\(\\s*(?:['\"]([^'\"\\n\\r$]+)['\"]|`([^`\\n\\r]+)`)",
)

// synthesizeAxiosInstanceCalls emits one synthetic per detected
// instance.<verb>(url) callsite. Behaviour:
//
//   - If the receiver is in the file-local axios.create() table, emit
//     and prepend the instance's baseURL (if any).
//   - Else if the receiver starts with `$`, treat it as an imported
//     axios-like instance and emit with no baseURL prefix.
//   - Else skip (handled by axiosClientRe / axiosLiteralRe already, or
//     intentionally ignored).
//
// Dedup against already-emitted axiosClientRe matches is enforced upstream
// by the per-ID dedup map in http_endpoint_synthesis.go.
func synthesizeAxiosInstanceCalls(
	content string,
	funcs []jsFuncSpan,
	syms map[string]string,
	instances map[string]axiosInstance,
	emit emitFn,
) {
	emitMatch := func(receiver, verb, path string, isTemplate bool, pos int) {
		var resolved string
		var ok bool
		if isTemplate {
			resolved, ok = canonicalizeTemplateLiteral(path, syms)
		} else {
			candidate := stripURLHost(path)
			if looksLikeURLPath(candidate) {
				resolved = candidate
				ok = true
			}
		}
		if !ok {
			return
		}

		// baseURL composition.
		if inst, found := instances[receiver]; found && inst.baseURL != "" {
			resolved = composeBaseURL(inst.baseURL, resolved)
		}

		caller := enclosingJSFuncAt(funcs, pos)
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, resolved)
		emit(strings.ToUpper(verb), canonical, "axios_instance", "Function", caller)
	}

	// Pass 1: any receiver present in the in-file axios.create() table.
	if len(instances) > 0 {
		for _, m := range axiosInstanceCallRe.FindAllStringSubmatchIndex(content, -1) {
			if len(m) < 10 {
				continue
			}
			receiver := content[m[2]:m[3]]
			if _, ok := instances[receiver]; !ok {
				continue
			}
			verb := content[m[4]:m[5]]
			var pathArg string
			var isTemplate bool
			if m[6] >= 0 {
				pathArg = content[m[6]:m[7]]
			} else if m[8] >= 0 {
				pathArg = content[m[8]:m[9]]
				isTemplate = true
			}
			if pathArg == "" {
				continue
			}
			emitMatch(receiver, verb, pathArg, isTemplate, m[0])
		}
	}

	// Pass 2: $-prefixed receivers (imported instances).
	for _, m := range dollarPrefixedHTTPRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		receiver := content[m[2]:m[3]]
		// If we already covered this receiver via the instance table,
		// pass-1 emitted; skip here to avoid duplicates.
		if _, ok := instances[receiver]; ok {
			continue
		}
		verb := content[m[4]:m[5]]
		var pathArg string
		var isTemplate bool
		if m[6] >= 0 {
			pathArg = content[m[6]:m[7]]
		} else if m[8] >= 0 {
			pathArg = content[m[8]:m[9]]
			isTemplate = true
		}
		if pathArg == "" {
			continue
		}
		emitMatch(receiver, verb, pathArg, isTemplate, m[0])
	}
}

// ---------------------------------------------------------------------------
// Phase 4 (#712) — bare const-variable path resolution
// ---------------------------------------------------------------------------

// synthesizeBareIdentifierCalls handles HTTP client calls where the URL
// argument is a bare identifier (not a quoted string literal or template
// literal), e.g.:
//
//	const BASE_PATH = "/buildings/";
//	$http.get(BASE_PATH, { params: {...} })
//	$http.delete(RECENTS_PATH, { params: {...} })
//
// The identifier is resolved via the file-local constant symbol table
// (syms) built by buildJSConstantSymbolTable. If it resolves to a URL
// path string, a synthetic http_endpoint entity is emitted.
//
// Receiver filtering (same logic as synthesizeAxiosInstanceCalls):
//   - Any receiver in the per-file axios.create() instance table
//   - Any $-prefixed receiver (imported axios-like instance)
//   - `axios` itself
//   - Any *Client / *HttpClient / httpClient / apiClient receiver
//     (mirrors axiosClientRe; avoids false-firing on non-HTTP calls like
//     `router.get(PATH)` which are producer-side Express routes)
//
// Beyond-minimum (#712): also handles `let X = "..."` assignments (covered
// by buildJSConstantSymbolTable which matches const|let|var) so no extra
// work is needed here.
func synthesizeBareIdentifierCalls(
	content string,
	funcs []jsFuncSpan,
	syms map[string]string,
	instances map[string]axiosInstance,
	emit emitFn,
) {
	if len(syms) == 0 {
		return
	}

	for _, m := range bareIdentCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		receiver := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		ident := content[m[6]:m[7]]

		// Filter: only known HTTP-client receivers. This prevents false
		// positives from Express producer-side `app.get(ROUTE, handler)`
		// and other framework calls that happen to pass a const first.
		if !isBareIdentHTTPReceiver(receiver, instances) {
			continue
		}

		// Resolve the identifier via the symbol table.
		raw, ok := syms[ident]
		if !ok {
			continue
		}

		// Must look like a URL path.
		candidate := stripURLHost(raw)
		if !looksLikeURLPath(candidate) {
			continue
		}

		// Prepend baseURL if the receiver is a known axios.create instance.
		if inst, found := instances[receiver]; found && inst.baseURL != "" {
			candidate = composeBaseURL(inst.baseURL, candidate)
		}

		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, candidate)
		framework := "axios_instance"
		if strings.HasPrefix(receiver, "$") {
			framework = "axios_instance"
		} else if strings.EqualFold(receiver, "axios") {
			framework = "axios"
		} else if strings.HasSuffix(strings.ToLower(receiver), "client") ||
			strings.Contains(strings.ToLower(receiver), "httpclient") {
			framework = "http_client"
		}
		emit(verb, canonical, framework, "Function", caller)
	}
}

// isBareIdentHTTPReceiver returns true when the receiver name is a known
// HTTP-client pattern — same classification logic used by axiosClientRe
// and dollarPrefixedHTTPRe, but applied to a resolved identifier name
// rather than a regex anchor.
func isBareIdentHTTPReceiver(receiver string, instances map[string]axiosInstance) bool {
	if _, ok := instances[receiver]; ok {
		return true
	}
	if strings.HasPrefix(receiver, "$") {
		return true
	}
	lower := strings.ToLower(receiver)
	if lower == "axios" {
		return true
	}
	// *Client / *HttpClient / httpClient / apiClient patterns.
	if strings.HasSuffix(lower, "client") ||
		strings.Contains(lower, "httpclient") ||
		lower == "api" ||
		lower == "http" {
		return true
	}
	return false
}

// composeBaseURL joins a baseURL prefix and a request path the way axios
// does: a trailing `/` on the base and a leading `/` on the path collapse
// to a single separator. Returns an absolute path beginning with `/`.
func composeBaseURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(base, "/") && base != "" {
		base = "/" + base
	}
	if path == "" {
		if base == "" {
			return "/"
		}
		return base
	}
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "{") {
		path = "/" + path
	}
	if strings.HasPrefix(path, "{") {
		// path starts with `{param}/...` — slot a `/` between base and {.
		return base + "/" + path
	}
	return base + path
}

// findMatchingBrace returns the index of the `}` matching the `{` at
// openIdx, accounting for nested braces inside `${...}` template-literal
// substitutions and inside nested object literals. Scans at most 4096
// bytes forward; returns -1 if no match found within the budget.
//
// We do NOT attempt to skip string-literal contents — wrapper-call obj
// literals in practice rarely contain `{` or `}` inside strings; the
// template-literal `${` case is the one that matters and that DOES
// balance correctly under naive counting.
func findMatchingBrace(content string, openIdx int) int {
	depth := 0
	limit := openIdx + 4096
	if limit > len(content) {
		limit = len(content)
	}
	for i := openIdx; i < limit; i++ {
		switch content[i] {
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

// findMatchingParenAfter returns the index of the next `)` at the outer
// paren level after `start`, scanning up to `limit` bytes. Used to find
// the close of the wrapper call after the object-literal arg. Returns -1
// if the close isn't found within budget.
//
// We assume the wrapper-call parens are already open at depth 1 when this
// is called (the regex consumed the opening `(`). We start at the byte
// after the obj-literal's `}` and decrement on each `)`.
func findMatchingParenAfter(content string, start, budget int) int {
	depth := 1
	end := start + budget
	if end > len(content) {
		end = len(content)
	}
	for i := start; i < end; i++ {
		switch content[i] {
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

// indexJSEnclosingFunctions returns a slice of (offset, name) records in
// file order, one per named function definition we recognise. Used to
// attribute downstream call sites to a `source_caller`.
type jsFuncSpan struct {
	offset int
	name   string
}

func indexJSEnclosingFunctions(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range jsFuncDeclRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := ""
		// Group 1 (function foo(...)) takes precedence over group 2 (const foo = ...)
		if m[2] >= 0 {
			name = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = content[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: name})
	}
	return out
}

// enclosingJSFuncAt returns the name of the nearest preceding function
// definition for a call site at `pos`. Returns "" if none found.
func enclosingJSFuncAt(funcs []jsFuncSpan, pos int) string {
	name := ""
	for _, f := range funcs {
		if f.offset > pos {
			break
		}
		name = f.name
	}
	return name
}

// ---------------------------------------------------------------------------
// Python: helpers shared with http_endpoint_python_client.go (#721 wave 1)
// ---------------------------------------------------------------------------
//
// The Python consumer-side synthesizer (synthesizePyClient + supporting
// regex/symbol-table helpers) now lives in http_endpoint_python_client.go.
// Only the shared enclosing-function indexer remains here, since it shares
// the jsFuncSpan layout with the JS/TS scanner above.

type pyFuncSpan = jsFuncSpan

func indexPyEnclosingFunctions(content string) []pyFuncSpan {
	var out []pyFuncSpan
	for _, m := range pyEnclosingFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, pyFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

func enclosingPyFuncAt(funcs []pyFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// looksLikeURLPath rejects strings that obviously aren't URL paths.
// Phase 1 accepts:
//   - Absolute paths starting with `/`
//   - Absolute URLs starting with `http://` or `https://`
//
// The absolute-URL case is folded back to its path component because the
// cross-repo linker matches by canonical path string, not by host. (A
// future phase can add host-aware matching for multi-tenant deployments.)
//
// Rejected:
//   - Empty / whitespace-only
//   - Identifiers (no `/`, no scheme)
//   - URLs containing template substitution markers (handled in Phase 2)
func looksLikeURLPath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.Contains(s, "${") || strings.Contains(s, "{{") {
		return false
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		// Has-a-path check: after scheme://host there must be a `/`.
		idx := strings.Index(s[8:], "/")
		return idx >= 0
	}
	return strings.HasPrefix(s, "/")
}

// stripURLHost returns the path component of an absolute URL, or the
// input unchanged for relative paths. Used by the client emitters before
// canonicalisation.
func stripURLHost(s string) string {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return s
	}
	rest := s
	if strings.HasPrefix(s, "https://") {
		rest = s[len("https://"):]
	} else {
		rest = s[len("http://"):]
	}
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return "/"
	}
	return rest[idx:]
}
