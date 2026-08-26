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

	"github.com/cajasmota/grafel/internal/engine/httproutes"
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
// jsFuncDeclRe recognises named function definitions in JS/TS source.
// We intentionally cast a wide net to cover four common shapes:
//
//  1. `function foo(` / `async function foo(`
//
//  2. `const/let/var foo = (` / `const/let/var foo = async (`
//
//  3. Class property arrow: `foo = (` / `foo = async (` (without var/const/let).
//     This covers React component class methods and service-class patterns
//     common in Angular/Vue/RN frontends, e.g. `login = (email) => $http.post(...)`.
//
//  4. Method shorthand: `foo(...) {` inside an object literal or a class
//     body — `list(): Observable<T[]> { … }`, `async save(x) { … }`.
//     This shape used to be declined outright, on the grounds that a bare
//     `foo(` collides with arbitrary call expressions. That hazard is real,
//     but it is a property of the BARE form, not of the shape: an Angular /
//     NestJS / plain-TS service makes nearly all of its HTTP calls from
//     class methods, so declining it left those call sites with no caller
//     at all (#6447). The alternative below keeps the collision closed by
//     demanding four things at once that a call expression does not have:
//     line-start plus indentation (a call in an expression is preceded by
//     `=`, `.`, `(`, `return`, …), a PARAMETER group containing no nested
//     parens (which rules out `describe('x', () => {`), an optional TS
//     return annotation, and a following `{`. The `()`-free rule applies to
//     the parameter group only: the return annotation is `[^;{}\n]*`,
//     because a function type in return position carries its own parens
//     (`makeLoader(): (id: string) => Promise<void> {`) and excluding them
//     silently mis-attributed an ordinary TS shape.
//
//     The `\n` in that class is load-bearing and is NOT symmetry with the
//     other exclusions: in Go's regexp `[^;{}]` matches a newline, so an
//     annotation with no newline brake runs across lines until it finds a
//     `{` — turning a JSDoc continuation ` * Word (prose):` into a span
//     named `Word` that swallows the `export function` header below it, and
//     letting a semicolon-free abstract signature swallow the next method's
//     body. Measured on this repo's own webui-v2 frontend, the unbraked form
//     changed the spans of 10 of 233 files and destroyed the caller of a real
//     exported function. Every shape the annotation legitimately needs to
//     admit — a function type, a parenthesised union — is single-line, so the
//     brake costs nothing. Both crossings are pinned in
//     TestJSMethodShorthandDoesNotSwallowNonDeclarations_6447.
//
//     The residue is the
//     reserved words that are also written `(...) {` — `if`, `for`, `while`,
//     `switch`, `catch`, … — and those are rejected by name in
//     indexJSEnclosingFunctions rather than in the pattern, because RE2 has
//     no negative lookahead and spelling them into the alternation would
//     bury the shape being matched.
//
//     Unambiguous prefixes are allowed rather than declined: `*name(`,
//     `async *name(` (generators) and `#name(` (private methods) cannot be
//     call expressions, so there is no collision to trade against. The `#`
//     stays IN the captured name, and that is a consistency requirement
//     rather than a tie-break: handleMethodDefinition in
//     internal/extractors/javascript/extractor.go:1073 names a method entity
//     with `x.nodeText(nameNode)`, and for `#load` the name node is a
//     `private_property_identifier` whose text INCLUDES the `#` — a probe of
//     `class A { #load() {} load() {} }` emits entities named `#load` and
//     `load`, not `load` twice. Keeping the
//     `#` is what makes the `"Function:"+caller` reference resolvable at all;
//     stripping it would dangle. It also keeps a class that declares both
//     `#load` and `load` distinguishable.
//
// NOTE: #6500 arm A gave a span an `end` (jsFuncSpan.end, computed by
// jsSpanEnd; pyFuncSpan was split off the alias and gets an indentation-derived
// end from pySpanEnd), and enclosingJSFuncAt now prefers the innermost span
// CONTAINING a call site. What is NOT fixed, deliberately, is what happens when
// no span contains it: the walk still falls back to the nearest preceding name.
// That fallback is arm B of #6500 and is blocked on a per-consumer policy
// decision, because sse_edges.go and websocket_edges.go fold the caller into an
// entity IDENTITY rather than a property. enclosingJSFuncAt has 43 non-test
// call sites — including the C#, Kotlin, Swift, Rust, Ruby, Scala, Go, Java,
// Dart and PHP client passes, which reuse the walk over their own span lists —
// and indexJSEnclosingFunctions has 13.
//
// So DECLINING to match a shape is still not a safe no-op. A call site with no
// span containing it lands on the preceding declaration, so every shape this
// alternative turns away yields a wrong caller, not an empty one — that being
// the exact defect #6447 fixes, so these are places the fix does not reach
// rather than places it is careful. What #6500 changed is the SOURCE of the
// wrong caller: a declaration that is merely nearer no longer wins over the one
// the call site is actually inside.
// The known set is pinned in TestJSKnownMisattributionShapes_6447: column-0
// shorthand, computed keys (`['load']() {`), Allman braces, one-line class
// bodies, methods named after a rejected reserved word (`catch`, `return`),
// and the two shapes the type-parameter group `<[^<>()]*>` turns away —
// a generic with a function-typed default (`load<T = () => void>(x): T {`,
// rejected for the parens) and a nested generic (`load<Map<string, T>>(x) {`,
// rejected for the inner angle brackets). Both are ordinary TypeScript, not
// exotica; widening that group is a separate trade against the
// `describe('x', () => {` collision and is not attempted here.
// This is not confined to edge sourcing: sse_edges.go builds a Stream entity's
// ID as `"/" + caller`, so a wrong caller is a wrongly NAMED entity.
//
// The pattern is also blind to comments and template literals, as alternatives
// 1-3 already are; alternative 4 widens that surface materially, because a
// commented-out method body inside a live class is common where a
// commented-out `function foo(` is not. Accepted for now — masking dead text
// is a whole-pattern change, not a change to this alternative — and pinned in
// TestJSMethodShorthandInDeadTextPoisons_6447 so it stays a known cost.
var jsFuncDeclRe = regexp.MustCompile(
	`(?m)(?:^|[^\w$])(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(` +
		`|(?m)(?:^|[^\w$])(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\(` +
		`|(?m)(?:^|[\s{,;])([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\(` +
		`|(?m)^[ \t]+(?:(?:public|private|protected|static|readonly|async|get|set|override|abstract)[ \t]+)*` +
		`(?:\*[ \t]*)?(#?[A-Za-z_$][\w$]*)[ \t]*(?:<[^<>()]*>)?[ \t]*\([^()]*\)[ \t]*(?::[^;{}\n]*)?\{`,
)

// jsMethodShorthandReserved lists the identifiers that satisfy alternative 4
// of jsFuncDeclRe structurally — indentation, `(...)`, `{` — without being a
// method declaration. `if (ok) {` is indistinguishable from `probe(x) {` to
// RE2 without a negative lookahead, so the discrimination happens here.
//
// Being permissive here is the dangerous direction, not the strict one: a
// missing entry mints a span named `if`, and enclosingJSFuncAt would then
// attribute call sites to it — a WRONG caller, which is worse than the empty
// one #6447 started from. #6500's end bound narrows the reach of such a span to
// the block's own braces; it does not stop the span being minted, and inside
// that block the mis-attribution is unchanged.
//
// The reject is by NAME, so it also rejects a method legitimately called after
// one of these words. That is a cost, not a free win, and for the same reason
// as above it is paid in mis-attribution rather than in silence: the calls
// inside a real `catch(err) { … }` (thenable) or `return(v) { … }` (iterator
// protocol) are stamped with the PRECEDING method's name. Two rows in
// TestJSKnownMisattributionShapes_6447 hold that fact still. `function` and
// `with` are here for shapes alternative 1 does not cover — an ANONYMOUS
// `function (x) {` at line start, and sloppy-mode `with (o) {` — not for
// symmetry.
var jsMethodShorthandReserved = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true,
	"catch": true, "return": true, "do": true, "function": true,
	"else": true, "with": true, "try": true, "finally": true,
}

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

// jsConstTemplateLiteralRe matches template-literal variable assignments:
//
//	const url = `${process.env.API_URL}/users`;
//	const url = `${apiUrl}/${endpoint}`;
//
// These are NOT captured by jsConstStringRe (which requires plain string
// quotes) but ARE needed for #654: when fetch(url) is called with a
// variable that holds a template literal, we must trace back to the
// template literal to extract the URL pattern.
//
// Capture groups: 1=name, 2=template-literal body (between backticks).
var jsConstTemplateLiteralRe = regexp.MustCompile(
	"(?m)(?:const|let|var)\\s+([A-Za-z_$][\\w$]*)\\s*=\\s*`([^`\\n\\r]*\\$\\{[^`\\n\\r]*)`",
)

// fetchBareIdentRe matches `fetch(ident, ...)` where the argument is a bare
// identifier (not a quoted string, not a template literal). Used by #654
// to handle the pattern:
//
//	const url = `${process.env.API_URL}/users`;
//	fetch(url, { method: "POST" });
//
// Capture groups:
//
//	1 = bare identifier (the URL variable name)
//	2 = optional options object (for method extraction)
var fetchBareIdentRe = regexp.MustCompile(
	`(?:^|[^\w$.])fetch\s*\(\s*([A-Za-z_$][\w$]*)\s*(?:,(\s*\{[^}]*\}))?\)`,
)

// buildJSTemplateLiteralSymbolTable returns a map from identifier name →
// raw template-literal body (excluding backticks) for every
// const/let/var assignment of the form `const X = `...${...}...“.
// Used by #654 to resolve bare-identifier fetch arguments.
func buildJSTemplateLiteralSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range jsConstTemplateLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := content[m[2]:m[3]]
		tmplBody := content[m[4]:m[5]]
		if _, dup := syms[name]; !dup {
			syms[name] = tmplBody
		}
	}
	return syms
}

// ---------------------------------------------------------------------------
// Env-var URL patterns (#721 beyond-minimum)
// ---------------------------------------------------------------------------
//
// Handles fetch(process.env.X + "/path"), fetch(import.meta.env.VITE_X + "/path"),
// axios.get(process.env.NEXT_PUBLIC_X + "/path"), etc.
// Emits the path suffix with runtime_dynamic=true so the repair flow (#732)
// can annotate the resulting synthetic.

// jsEnvPrefixRe matches `process.env.<IDENT>`, `process.env["IDENT"]`,
// `import.meta.env.<IDENT>`, and `import.meta.env["IDENT"]`.
// Used as a building block in the compound env-fetch regexes below.
var jsEnvPrefixRe = regexp.MustCompile(
	`(?:process\.env(?:\.[A-Za-z_$][\w$]*|\["[^"]+"\])|import\.meta\.env(?:\.[A-Za-z_$][\w$]*|\["[^"]+"\]))`,
)

// fetchEnvConcatRe matches `fetch(process.env.X + "/path", ...)` and
// `fetch(import.meta.env.VITE_X + "/path", ...)`.
//
// Capture groups:
//
//	1 = path suffix literal
//	2 = optional options object (for method extraction)
var fetchEnvConcatRe = regexp.MustCompile(
	`(?:^|[^\w$.])fetch\s*\(\s*(?:process\.env|import\.meta\.env)[^\s+]+\s*\+\s*['"]([^'"\n\r]+)['"](\s*,\s*\{[^}]*\})?`,
)

// axiosEnvConcatRe matches `axios.<verb>(process.env.X + "/path", ...)`.
//
// Capture groups:
//
//	1 = verb
//	2 = path suffix literal
var axiosEnvConcatRe = regexp.MustCompile(
	`\baxios\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:process\.env|import\.meta\.env)[^\s+]+\s*\+\s*['"]([^'"\n\r]+)['"]`,
)

// clientEnvConcatRe matches `<ident>Client.<verb>(process.env.X + "/path")` and
// `$http.get(process.env.X + "/path")`.
//
// Capture groups:
//
//	1 = receiver identifier
//	2 = verb
//	3 = path suffix literal
var clientEnvConcatRe = regexp.MustCompile(
	`(?:^|[^\w$.])(\$?[A-Za-z_$][\w$]*)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:process\.env|import\.meta\.env)[^\s+]+\s*\+\s*['"]([^'"\n\r]+)['"]`,
)

// arrowFnTemplate captures a same-file arrow-function declaration whose
// body is a single template literal. Used by #2708 to inline factory
// callsites like `${base(companyType, branchId)}/contacts` into the
// canonical URL path.
//
// Example source: `const base = (companyType, companyId) =>
// `+"`"+`/${COMPANY_TYPE_MAPPING[companyType]}/${companyId}/branches`+"`"+`
//
//	→ Params: ["companyType", "companyId"]
//	→ Body:   "/${COMPANY_TYPE_MAPPING[companyType]}/${companyId}/branches"
type arrowFnTemplate struct {
	Params []string
	Body   string // raw template-literal content (between backticks)
}

// jsArrowFnTemplateRe matches an arrow-function declaration whose RHS is a
// single template literal. We allow both the parenthesised parameter list
// (`(a, b) => ...`) and the single-parameter bare form (`a => ...`).
//
// Capture groups:
//
//	1 = identifier name
//	2 = parameter list (parenthesised body), or empty if single-param form
//	3 = single-param identifier (when group 2 is empty)
//	4 = template-literal body (between backticks)
//
// Whitespace and a newline are tolerated between `=>` and the opening
// backtick (very common in real-world formatters — the acme-frontend
// branchService.js fixture splits the body across two lines).
//
// The body cannot contain backticks; multi-line templates and tagged
// templates are out of scope per the issue.
var jsArrowFnTemplateRe = regexp.MustCompile(
	"(?:const|let|var)\\s+([A-Za-z_$][\\w$]*)\\s*=\\s*(?:\\(([^)]*)\\)|([A-Za-z_$][\\w$]*))\\s*=>\\s*`([^`]*)`",
)

// buildJSArrowFnTemplateTable returns a map from identifier name →
// arrowFnTemplate for every same-file arrow-function declaration whose
// body is a template literal. Used by canonicalizeTemplateLiteralCore
// (issue #2708) to inline factory callsites.
//
// Only the simple shape `const NAME = (a, b) => `+"`"+`...`+"`"+“ is captured.
// Multi-statement bodies, conditional returns, async/await wrappers, and
// tagged templates are NOT captured — those callsites keep their existing
// `{param}` placeholder behaviour.
func buildJSArrowFnTemplateTable(content string) map[string]arrowFnTemplate {
	out := make(map[string]arrowFnTemplate)
	for _, m := range jsArrowFnTemplateRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		name := content[m[2]:m[3]]
		var params []string
		if m[4] >= 0 && m[5] > m[4] {
			// Parenthesised form: split on commas, trim whitespace.
			raw := content[m[4]:m[5]]
			for _, p := range strings.Split(raw, ",") {
				p = strings.TrimSpace(p)
				// Strip any default value: `a = 1` → `a`.
				if eq := strings.IndexByte(p, '='); eq >= 0 {
					p = strings.TrimSpace(p[:eq])
				}
				// Strip TypeScript type annotation: `a: string` → `a`.
				if colon := strings.IndexByte(p, ':'); colon >= 0 {
					p = strings.TrimSpace(p[:colon])
				}
				if jsIdentRe.MatchString(p) {
					params = append(params, p)
				}
			}
		} else if m[6] >= 0 && m[7] > m[6] {
			// Single-param bare form: `x => ...`.
			params = []string{content[m[6]:m[7]]}
		} else {
			// No-param parenthesised form `() => ...` — body has no
			// references to substitute; still capture it.
			params = nil
		}
		body := content[m[8]:m[9]]
		if _, dup := out[name]; !dup {
			out[name] = arrowFnTemplate{Params: params, Body: body}
		}
	}
	return out
}

// jsCallExprRe matches a function call shaped `ident(args)` where args is
// the raw argument list (no nested parens). Used by the arrow-fn inliner
// to detect `${base(companyType, branchId)}` interpolations.
//
// Capture groups: 1 = identifier; 2 = argument list (may be empty).
var jsCallExprRe = regexp.MustCompile(`^([A-Za-z_$][\w$]*)\s*\(([^()]*)\)\s*$`)

// splitTopLevelArgs splits a raw argument list on commas that are NOT
// nested inside brackets / parens / quotes / template literals. Returns
// the trimmed argument expressions.
func splitTopLevelArgs(s string) []string {
	var out []string
	depth := 0
	inSingle, inDouble, inBacktick := false, false, false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\\' {
				i++
				continue
			}
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
		case inBacktick:
			if c == '\\' {
				i++
				continue
			}
			if c == '`' {
				inBacktick = false
			}
		default:
			switch c {
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case '`':
				inBacktick = true
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			case ',':
				if depth == 0 {
					out = append(out, strings.TrimSpace(s[start:i]))
					start = i + 1
				}
			}
		}
	}
	if start < len(s) {
		tail := strings.TrimSpace(s[start:])
		if tail != "" || len(out) > 0 {
			out = append(out, tail)
		}
	}
	return out
}

// substituteArrowFnBody returns the arrow-fn body with each `${expr}`
// interpolation rewritten according to the call-site arguments.
//
// For each `${innerExpr}` in body:
//   - Determine which parameter is referenced. The parameter must appear
//     as a top-level identifier in the expression (either bare, dotted, or
//     as the subscript index of a member expression).
//   - If the call-site arg for that parameter is a quoted string literal,
//     substitute the literal value.
//   - Otherwise emit `${paramName}` (the arrow fn's parameter name) so the
//     outer canonicaliser produces a meaningful `{paramName}` placeholder.
//
// If an interpolation references zero or multiple parameters (e.g.
// `${a + b}`), it is preserved verbatim — the outer canonicaliser will
// fall back to `{param}` for it.
//
// Returns ("", false) if the body cannot be substituted (e.g. mismatched
// argument count).
func substituteArrowFnBody(fn arrowFnTemplate, args []string) (string, bool) {
	if len(args) < len(fn.Params) {
		// Missing trailing args → treat as `undefined`, expressed as the
		// param-name placeholder.
		for len(args) < len(fn.Params) {
			args = append(args, "")
		}
	}
	paramIdx := make(map[string]int, len(fn.Params))
	for i, p := range fn.Params {
		paramIdx[p] = i
	}
	out := templateSubstRe.ReplaceAllStringFunc(fn.Body, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-1])
		// Find which parameters are referenced in this expression.
		// We scan for identifier tokens and pick the first one that
		// matches a parameter name. Multiple distinct param refs in one
		// interpolation → too complex, keep verbatim.
		var referenced string
		multiple := false
		for ident := range identTokens(inner) {
			if _, ok := paramIdx[ident]; !ok {
				continue
			}
			if referenced != "" && referenced != ident {
				multiple = true
				break
			}
			referenced = ident
		}
		if referenced == "" || multiple {
			return match
		}
		arg := strings.TrimSpace(args[paramIdx[referenced]])
		// Literal string arg → substitute the literal value (no braces).
		if lit, ok := unquoteJSString(arg); ok {
			// If the original expr was the bare parameter, replace the
			// whole `${param}` with the literal. If it was a more complex
			// expression (subscript / dotted), the substitution is unsafe
			// — fall back to `${paramName}`.
			if inner == referenced {
				return lit
			}
			return "${" + referenced + "}"
		}
		// Non-literal call-site arg → emit `${paramName}` so the outer
		// canonicaliser turns it into `{paramName}`.
		return "${" + referenced + "}"
	})
	return out, true
}

// identTokens returns the set of bare-identifier tokens that appear in s.
// Used to detect which arrow-fn parameters an interpolation expression
// references. Properties on the RHS of a dot (e.g. `id` in `user.id`) are
// excluded because they cannot resolve to a parameter binding.
func identTokens(s string) map[string]struct{} {
	out := make(map[string]struct{})
	i := 0
	prevDot := false
	for i < len(s) {
		c := s[i]
		if isJSIdentStart(c) {
			j := i + 1
			for j < len(s) && isJSIdentPart(s[j]) {
				j++
			}
			if !prevDot {
				out[s[i:j]] = struct{}{}
			}
			i = j
			prevDot = false
			continue
		}
		prevDot = c == '.'
		i++
	}
	return out
}

func isJSIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isJSIdentPart(c byte) bool {
	return isJSIdentStart(c) || (c >= '0' && c <= '9')
}

// unquoteJSString returns the string body if s is a quoted JS string
// literal (single, double, or backtick with no `${}` interpolations).
// Returns ("", false) otherwise.
func unquoteJSString(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return "", false
	}
	first, last := s[0], s[len(s)-1]
	if first != last {
		return "", false
	}
	if first != '\'' && first != '"' && first != '`' {
		return "", false
	}
	body := s[1 : len(s)-1]
	// Reject template literals with interpolations.
	if first == '`' && strings.Contains(body, "${") {
		return "", false
	}
	// Reject any embedded unescaped quote of the same kind.
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' {
			i++
			continue
		}
		if body[i] == first {
			return "", false
		}
	}
	return body, true
}

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

// ---------------------------------------------------------------------------
// #2709 — object-literal const symbol table for template-literal subscripts
// ---------------------------------------------------------------------------
//
// Handles the pattern:
//
//	const COMPANY_TYPE_MAPPING = {
//	    1: "contracting-companies",
//	    2: "witnessing-companies",
//	};
//	apiClient.get(`/${COMPANY_TYPE_MAPPING[companyType]}/${id}`);
//
// We collect ident → (key → string-literal value) for every same-file
// `const NAME = { ... }` declaration whose body is a flat key/value list of
// string-literal values. Used by canonicalizeTemplateLiteralExpand to
// enumerate subscript interpolations.
//
// Limits (per the issue scope):
//   - const declaration only (not let/var, not assignment, not merge / spread)
//   - flat literal only — nested objects, computed keys, function values are
//     all ignored (the entire ident is skipped if any pair fails to parse)
//   - all values must be string literals (single, double, or backtick — no
//     substitutions inside the value)

// jsConstObjectStartRe matches the START of a `const NAME = { ... }`
// declaration. We capture the identifier and the position just past the
// opening `{` so the caller can walk forward to the matching close brace
// (handled by findMatchingBrace, already used by wrapper-call parsing).
//
// Capture group: 1 = identifier name.
var jsConstObjectStartRe = regexp.MustCompile(
	`(?m)\bconst\s+([A-Za-z_$][\w$]*)\s*=\s*\{`,
)

// jsConstObjectPairRe matches a single `key: "value"` pair inside a flat
// object literal body. Keys may be:
//   - bare identifier (`foo`)
//   - quoted string (`"foo"` / `'foo'`)
//   - numeric (`1`, `42`) — preserved as-is in the resulting key string
//
// Values must be string literals (single, double, or backtick — no
// substitutions). Other shapes (numbers, booleans, nested objects,
// function references) skip the pair.
//
// Capture groups: 1/2/3 = key variants (bare/numeric, double-quoted,
// single-quoted); 4/5/6 = value variants (double, single, backtick).
var jsConstObjectPairRe = regexp.MustCompile(
	"(?:^|[,{\\s])" +
		"(?:([A-Za-z_$][\\w$]*|\\d+)|\"([^\"\\n\\r]+)\"|'([^'\\n\\r]+)')" +
		"\\s*:\\s*" +
		"(?:\"([^\"\\n\\r]*)\"|'([^'\\n\\r]*)'|`([^`\\n\\r$]*)`)",
)

// buildJSConstantObjectTable returns a map from identifier name → (key →
// string value) for every `const NAME = { ... }` flat object literal in
// the file whose values are all string literals.
//
// An object literal is INCLUDED only when:
//   - the entire body parses as a flat key/value list (no nested braces)
//   - every value is a string literal
//
// On any failure (non-string value, nested object, unmatched brace, etc.)
// the ident is omitted entirely rather than half-populated.
func buildJSConstantObjectTable(content string) map[string]map[string]string {
	out := map[string]map[string]string{}
	if !strings.Contains(content, "const") || !strings.Contains(content, "{") {
		return out
	}
	for _, m := range jsConstObjectStartRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		// m[1] is the byte just past `{`; back up by one to the `{` itself.
		openIdx := m[1] - 1
		if openIdx < 0 || openIdx >= len(content) || content[openIdx] != '{' {
			continue
		}
		closeIdx := findMatchingBrace(content, openIdx)
		if closeIdx < 0 {
			continue
		}
		body := content[openIdx+1 : closeIdx]
		// Reject bodies that contain a nested object/function literal: any
		// `{` inside the body would mean a nested structure we don't model.
		if strings.ContainsAny(body, "{}") {
			continue
		}
		pairs := jsConstObjectPairRe.FindAllStringSubmatch(body, -1)
		if len(pairs) == 0 {
			continue
		}
		entries := map[string]string{}
		ok := true
		for _, p := range pairs {
			// Re-construct key.
			var key, val string
			switch {
			case p[1] != "":
				key = p[1]
			case p[2] != "":
				key = p[2]
			case p[3] != "":
				key = p[3]
			default:
				ok = false
			}
			switch {
			case p[4] != "":
				val = p[4]
			case p[5] != "":
				val = p[5]
			case p[6] != "":
				val = p[6]
			default:
				ok = false
			}
			if !ok {
				break
			}
			entries[key] = val
		}
		if !ok || len(entries) == 0 {
			continue
		}
		// Reject merge / spread / computed-key shapes: if the body contains
		// spread (`...`) we don't trust the static enumeration.
		if strings.Contains(body, "...") {
			continue
		}
		// Defensive: skip if a pair count mismatches non-whitespace commas.
		// A flat literal with N pairs should have at least N-1 commas (and
		// may have a trailing comma). If we matched fewer pairs than commas
		// suggest, some entry was non-string-literal and we should bail.
		commas := strings.Count(body, ",")
		// Allow trailing comma: pairs == commas OR pairs == commas+1.
		if len(pairs) < commas {
			// Some pair didn't match — half-populated, skip the whole ident.
			continue
		}
		if _, dup := out[name]; !dup {
			out[name] = entries
		}
	}
	return out
}

// clientSynthState carries per-extraction side-channel state from
// synthesizeFetchAxios (and its helpers) up to the runtime adapter.
//
// pendingPolySubscript, when non-empty just before an `emit(...)` call,
// signals that the just-canonicalised endpoint was produced by static
// enumeration of an `${ident[keyExpr]}` subscript expression (#2709). The
// adapter stamps the corresponding property on the emitted entity and
// then clears the field.
type clientSynthState struct {
	pendingPolySubscript string
	// pendingExtractionMethod, when non-empty just before an `emit(...)` call,
	// records HOW the about-to-be-emitted client synthetic was extracted. It is
	// set to "ast" by the tree-sitter pass (synthesizeFetchAxiosAST) and left
	// empty by the regex passes. The JS client adapter reads it and stamps the
	// `extraction_method` property so the endpoint-stats confidence classifier
	// (classifyDetector) can honestly mark AST-extracted client calls as
	// ast/exact instead of blanket regex/heuristic (#5527). Reset to "" after
	// each emit so a regex emit never inherits a stale stamp.
	pendingExtractionMethod string
}

// newClientSynthState returns an empty state ready to be threaded through
// the JS/TS client-synthesis call graph. Safe to pass nil to helpers that
// don't need poly support; helpers must nil-check before reading/writing.
func newClientSynthState() *clientSynthState {
	return &clientSynthState{}
}

// templateExpansion is one canonicalized path produced by expanding a
// template-literal URL. Most templates yield a single expansion with an
// empty PolySubscript. Subscript-into-map enumeration (#2709) yields one
// expansion per known map value, each tagged with the source subscript
// expression (e.g. `COMPANY_TYPE_MAPPING[companyType]`).
type templateExpansion struct {
	Path          string
	PolySubscript string // "" for non-polymorphic single expansions
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
// isEnvVarStyleExpr returns true when the expression inside ${...} looks like
// an explicit env-var accessor rather than a path parameter. Such expressions:
//   - Start with process.env. (e.g. process.env.API_URL)
//   - Start with import.meta.env. (e.g. import.meta.env.VITE_CORE_API)
//
// NOTE: We deliberately do NOT classify plain ALL_CAPS identifiers as env-var
// prefixes because they may be legitimate local constants (e.g. `${UNKNOWN_BASE}`
// in tests expecting `{UNKNOWN_BASE}` as a path placeholder, per #706). Only
// the explicit accessor forms are reliable env-var signals.
func isEnvVarStyleExpr(expr string) bool {
	return strings.HasPrefix(expr, "process.env.") || strings.HasPrefix(expr, "import.meta.env.")
}

// subscriptInterpRe matches `${IDENT[KEY_EXPR]}` interpolations. Capture
// groups: 1 = ident name; 2 = key expression (raw, may be a string literal
// or any identifier/expression — caller distinguishes).
//
// We require `[` + balanced single-pair `]` directly after the identifier
// (no chained property access like `ident.foo[key]`) to keep the rule tight
// and predictable. Whitespace around the brackets is tolerated.
var subscriptInterpRe = regexp.MustCompile(
	`^([A-Za-z_$][\w$]*)\s*\[\s*(.+?)\s*\]$`,
)

// stringLiteralRe matches a single, fully-enclosed string literal token
// (single or double quotes). Used to detect when a subscript key is a
// static literal rather than a dynamic identifier.
var stringLiteralRe = regexp.MustCompile(
	`^(?:"([^"\\]*)"|'([^'\\]*)')$`,
)

// canonicalizeTemplateLiteralExpand is the enumeration-aware extension of
// canonicalizeTemplateLiteral (#2709). When the template contains an
// `${ident[keyExpr]}` interpolation and `ident` resolves to a known
// flat const object literal:
//
//   - Static string-literal key: substitutes the matching value (or falls
//     back to `{param}` when the key is unknown). Single expansion.
//   - Dynamic identifier key (variable / parameter): enumerates ONE
//     expansion per known map value, each tagged with PolySubscript =
//     "<ident>[<keyExpr>]" so downstream consumers know this set is a
//     static discovery rather than a guaranteed runtime shape.
//
// When no subscript interpolation appears (or the ident is unknown), the
// function delegates to the existing scalar canonicalize path and returns
// a single expansion with PolySubscript = "" — preserving back-compat for
// every other call site.
//
// objSyms == nil disables subscript enumeration (back-compat for callers
// that don't carry the object-literal table).
//
// arrowFns may be nil — when it is, the function degrades to the
// pre-#2708 behaviour exactly (arrow-fn inlining is skipped).
func canonicalizeTemplateLiteralExpand(
	tmpl string,
	syms map[string]string,
	objSyms map[string]map[string]string,
	arrowFns map[string]arrowFnTemplate,
) []templateExpansion {
	// Fast path: no `[` in any interpolation means no subscripts to enumerate.
	// Fall back to the scalar canonicaliser.
	if len(objSyms) == 0 || !strings.Contains(tmpl, "[") {
		path, ok := canonicalizeTemplateLiteralCore(tmpl, syms, arrowFns, 0)
		if !ok {
			return nil
		}
		return []templateExpansion{{Path: path}}
	}

	// Find the FIRST subscript interpolation that targets a known object
	// literal. We enumerate one subscript at a time; templates with multiple
	// subscript interpolations are out of scope (and rare in practice).
	matches := templateSubstRe.FindAllStringSubmatchIndex(tmpl, -1)
	var (
		hitIdx     = -1 // index into matches[]
		hitIdent   string
		hitKeyExpr string
	)
	for i, m := range matches {
		inner := strings.TrimSpace(tmpl[m[2]:m[3]])
		sm := subscriptInterpRe.FindStringSubmatch(inner)
		if len(sm) < 3 {
			continue
		}
		ident := sm[1]
		if _, ok := objSyms[ident]; !ok {
			continue
		}
		hitIdx = i
		hitIdent = ident
		hitKeyExpr = strings.TrimSpace(sm[2])
		break
	}
	if hitIdx < 0 {
		path, ok := canonicalizeTemplateLiteralCore(tmpl, syms, arrowFns, 0)
		if !ok {
			return nil
		}
		return []templateExpansion{{Path: path}}
	}

	entries := objSyms[hitIdent]
	hitMatch := matches[hitIdx]
	matchStart, matchEnd := hitMatch[0], hitMatch[1]

	// Static-key case: subscript key is a string literal. Substitute the
	// matching value (or fall back to {param} when unknown).
	if lm := stringLiteralRe.FindStringSubmatch(hitKeyExpr); len(lm) > 0 {
		keyVal := lm[1]
		if keyVal == "" {
			keyVal = lm[2]
		}
		var replacement string
		if v, ok := entries[keyVal]; ok {
			replacement = v
		} else {
			replacement = "{param}"
		}
		rewritten := tmpl[:matchStart] + replacement + tmpl[matchEnd:]
		path, ok := canonicalizeTemplateLiteralCore(rewritten, syms, arrowFns, 0)
		if !ok {
			return nil
		}
		return []templateExpansion{{Path: path}}
	}

	// Dynamic-key case: enumerate one expansion per known value. The
	// keyExpr must look like a plain identifier (parameter / variable) —
	// anything more exotic falls back to the unknown-subscript `{param}`
	// shape so we don't over-fire on call-expressions or arithmetic.
	if !jsIdentRe.MatchString(hitKeyExpr) {
		path, ok := canonicalizeTemplateLiteralCore(tmpl, syms, arrowFns, 0)
		if !ok {
			return nil
		}
		return []templateExpansion{{Path: path}}
	}

	// Deterministic iteration order: keys are sorted so test output is
	// stable across runs (Go map iteration is randomised). We also dedup
	// by resolved path — different keys mapping to the same value should
	// not produce duplicate entries.
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	// Sort lexicographically for determinism.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}

	polyTag := hitIdent + "[" + hitKeyExpr + "]"
	expansions := make([]templateExpansion, 0, len(keys))
	seenPath := map[string]bool{}
	for _, k := range keys {
		v := entries[k]
		rewritten := tmpl[:matchStart] + v + tmpl[matchEnd:]
		path, ok := canonicalizeTemplateLiteralCore(rewritten, syms, arrowFns, 0)
		if !ok {
			continue
		}
		if seenPath[path] {
			continue
		}
		seenPath[path] = true
		expansions = append(expansions, templateExpansion{
			Path:          path,
			PolySubscript: polyTag,
		})
	}
	if len(expansions) == 0 {
		// All enumerated values failed canonicalisation — fall back to
		// the scalar path so we still emit something rather than nothing.
		path, ok := canonicalizeTemplateLiteralCore(tmpl, syms, arrowFns, 0)
		if !ok {
			return nil
		}
		return []templateExpansion{{Path: path}}
	}
	return expansions
}

func canonicalizeTemplateLiteral(tmpl string, syms map[string]string) (string, bool) {
	return canonicalizeTemplateLiteralCore(tmpl, syms, nil, 0)
}

// canonicalizeTemplateLiteralCore is the core implementation of
// canonicalizeTemplateLiteral. It additionally accepts an arrow-fn
// template-literal symbol table (issue #2708) used to inline factory
// callsites like `${base(companyType, branchId)}`. The depth argument
// guards against unbounded recursion when an inlined body itself calls
// another arrow-fn factory: depth >= 2 → skip the inlining step.
//
// arrowFns may be nil — when it is, the function degrades to the
// pre-#2708 behaviour exactly.
func canonicalizeTemplateLiteralCore(tmpl string, syms map[string]string, arrowFns map[string]arrowFnTemplate, depth int) (string, bool) {
	// Whether the FIRST substitution was an env-var prefix (to be stripped).
	firstSubst := true
	// #2704 — Track whether the template begins with a ${var} interpolation
	// that resolved to a same-file constant string (variable-binding resolution).
	// When it does and the resolved value lacks a leading slash, we prepend
	// one so the path validates as URL-absolute (e.g. `${path}/${id}` with
	// `const path = "companies"` → `/companies/{id}` instead of being rejected).
	leadingConstResolved := false
	// #2708 — Same logic for arrow-fn inlined values: when the FIRST
	// substitution resolved to an arrow-fn body whose first segment is a
	// path-shaped string, the resulting expansion may not start with `/`
	// even though it should. We track resolution separately so we don't
	// over-promote unresolved templates.
	leadingArrowFnResolved := false

	// Replace each ${expr} with its constant value or a named placeholder.
	result := templateSubstRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		// Extract the expression inside ${...}.
		inner := match[2 : len(match)-1]
		// Trim whitespace.
		inner = strings.TrimSpace(inner)

		isFirst := firstSubst
		firstSubst = false

		// For simple identifiers: look up in the constant symbol table.
		// For member expressions (e.g. `obj.field`), try the full expr
		// first, then the leading identifier.
		if val, ok := syms[inner]; ok {
			if isFirst {
				leadingConstResolved = true
			}
			return val
		}
		// Try just the leading identifier of a dotted expression.
		if dot := strings.IndexByte(inner, '.'); dot > 0 {
			if val, ok := syms[inner[:dot]]; ok {
				if isFirst {
					leadingConstResolved = true
				}
				return val
			}
		}

		// #2708 — Arrow-function template-literal factory inlining.
		// If the inner expression is a call `ident(args)` and `ident` is a
		// same-file arrow function whose body is a template literal, inline
		// the body (substituting parameters) and recursively canonicalise.
		// Depth-limited (max 2 levels) to prevent runaway expansion.
		if depth < 2 && len(arrowFns) > 0 {
			if cm := jsCallExprRe.FindStringSubmatch(inner); len(cm) == 3 {
				if fn, ok := arrowFns[cm[1]]; ok {
					args := splitTopLevelArgs(cm[2])
					// Resolve identifier-shaped args against the const
					// symbol table so `base(companyType, branchId)` where
					// `companyType` is itself a const string is folded.
					resolvedArgs := make([]string, len(args))
					for i, a := range args {
						a = strings.TrimSpace(a)
						if val, ok := syms[a]; ok {
							resolvedArgs[i] = "\"" + val + "\""
						} else {
							resolvedArgs[i] = a
						}
					}
					if expanded, ok := substituteArrowFnBody(fn, resolvedArgs); ok {
						// Recursively canonicalise the expanded body.
						// Strip the outer `${...}` wrapper from the
						// recursive call — we're returning the body that
						// will replace the interpolation.
						inlined, ok2 := canonicalizeTemplateLiteralCore(expanded, syms, arrowFns, depth+1)
						if ok2 {
							if isFirst {
								leadingArrowFnResolved = true
							}
							return inlined
						}
					}
				}
			}
		}

		// #807 — If this is the FIRST substitution and it looks like an env-var
		// (ALL_CAPS or process.env.X / import.meta.env.X), strip it (return "")
		// so it doesn't pollute the entity ID with a `{VITE_CORE_API}` prefix.
		// This produces the same path as the producer side which serves `/buildings`
		// without the base-URL prefix.
		if isFirst && isEnvVarStyleExpr(inner) {
			return ""
		}

		// Constant folding didn't apply — derive a semantic placeholder from
		// the expression identifier (#706).
		if name := extractParamName(inner); name != "" {
			return "{" + name + "}"
		}

		// Fallback for complex expressions (function calls, subscripts, etc.).
		return "{param}"
	})

	// Strip leading slash that remains after removing an env-var prefix.
	// When ${ENV}/path → ""/path → /path (leading slash already there).
	// When ${ENV}path (no leading slash) → ""path → "path" — add slash.
	// normaliseResult handles both cases.

	// Strip host prefix for absolute URLs.
	result = stripURLHost(result)

	// #2704 — When the leading interpolation resolved to a same-file const
	// string without a leading slash (e.g. `${path}/${id}` with `const path =
	// "companies"` → `companies/{id}`), prepend one so the path validates
	// as URL-absolute. Guarded by `leadingConstResolved` so we don't accept
	// bogus template literals like `not-a-path-${name}` whose leading literal
	// chunk was never a constant reference.
	if (leadingConstResolved || leadingArrowFnResolved) && len(result) > 0 && result[0] != '/' && result[0] != '{' {
		result = "/" + result
	}

	// #2710 — Strip query-string segment from the canonical path.
	// After template substitution, any remaining `?` is a genuine query string
	// separator (template params are already expanded above). Use only the
	// pre-`?` portion as the canonical path. This normalizes both sides of the
	// cross-repo match: backends rarely include query strings in route definitions,
	// and frontends should match on the path component only.
	if idx := strings.Index(result, "?"); idx >= 0 {
		result = result[:idx]
	}

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
//
// `state`, when non-nil, carries the side-channel used by #2709
// object-subscript enumeration to annotate emitted entities with a
// `polymorphic_subscript` property. May be nil for callers that don't need
// poly support.
func synthesizeFetchAxios(content string, emit emitFn, state *clientSynthState) {
	// Phase 5 (#806): React Query / RTK Query patterns can appear in files
	// that contain none of the standard HTTP-client markers below. Handle
	// them first so the early-exit guard doesn't drop them.
	// #2117: also check useMutation and useSuspenseQuery.
	if strings.Contains(content, "useQuery") || strings.Contains(content, "useMutation") ||
		strings.Contains(content, "builder.query") ||
		strings.Contains(content, "builder.mutation") || strings.Contains(content, "createApi") {
		funcsRQ := indexJSEnclosingFunctions(content)
		symsRQ := buildJSConstantSymbolTable(content)
		synthesizeReactQueryCalls(content, funcsRQ, symsRQ, emit)
	}

	// GraphQL client operations (Apollo / urql / graphql-request / raw gql
	// docs) emit operation-level http_endpoint_call entities keyed to the
	// server endpoint shape http:GRAPHQL:/graphql/<Root>/<field> (#3608).
	// Runs before the REST early-exit guard below since GraphQL client files
	// may contain none of the fetch/axios markers.
	if strings.Contains(content, "gql`") || strings.Contains(content, "graphql`") ||
		strings.Contains(content, "useQuery") || strings.Contains(content, "useMutation") ||
		strings.Contains(content, "useSubscription") || strings.Contains(content, "request(") {
		funcsGQL := indexJSEnclosingFunctions(content)
		synthesizeGraphQLClientCalls(content, funcsGQL, emit)
	}

	// WebSocket / Socket.IO client operations (socket.emit / socket.on on a
	// socket.io-client connection) emit event-level http_endpoint_call entities
	// keyed to the server endpoint shape http:WS:/<canonical event> (realtime
	// cross-link, epic #3628). Runs before the REST early-exit guard since a WS
	// client file may contain none of the fetch/axios markers.
	if strings.Contains(content, "io(") || strings.Contains(content, "io.connect(") ||
		strings.Contains(content, "socket.io-client") {
		funcsWS := indexJSEnclosingFunctions(content)
		synthesizeWSClientCalls(content, funcsWS, emit)
	}

	if !strings.Contains(content, "fetch(") &&
		!strings.Contains(content, "axios.") &&
		!strings.Contains(content, "axios(") &&
		!strings.Contains(content, "Client.") &&
		// #1418/#1422 — HTTP-client factory assignment (serviceClient(...),
		// httpClient(...), makeClient(...)). The resulting instance's
		// `.get/.post(...)` calls are recognised via the axios-instance table.
		!strings.Contains(content, "Client(") &&
		!strings.Contains(content, "client(") &&
		!strings.Contains(content, "httpClient.") &&
		!strings.Contains(content, "apiClient.") &&
		!strings.Contains(content, "endpoint:") &&
		!strings.Contains(content, "endpoint :") &&
		!strings.Contains(content, "axios.create") &&
		// #1483 — NestJS HttpService (RxJS) and Apollo Client URI.
		!strings.Contains(content, "httpService") &&
		// #6433 — Angular's injected HttpClient. An Angular service written as
		// `private http = inject(HttpClient)` + `this.http.get<T>('/api/x')`
		// contains none of the markers above, so this guard (not just the
		// synthesizeNestHttpService one) had to widen for it to be reached.
		// #6446 — evidence, not a substring: a migration TODO mentioning
		// HttpClient in a comment must not open this guard either.
		!hasInjectedHTTPClientToken(content) &&
		!strings.Contains(content, "ApolloClient") &&
		!strings.Contains(content, "$") {
		return
	}

	funcs := indexJSEnclosingFunctions(content)
	// Build constant symbol table once for the whole file (used by template
	// literal folding below).
	syms := buildJSConstantSymbolTable(content)
	// #2709 — build same-file const-object-literal table (used by subscript
	// enumeration in template-literal canonicalisation).
	objSyms := buildJSConstantObjectTable(content)
	// #2708 — Build the arrow-function template-literal table once. Used by
	// canonicalizeTemplateLiteralCore to inline factory callsites like
	// `${base(companyType, branchId)}/contacts`.
	arrowFns := buildJSArrowFnTemplateTable(content)

	// emitWithPoly stamps state.pendingPolySubscript for the next emit call
	// when the expansion is polymorphic, then calls emit and clears the
	// state. Safe with a nil state (no annotation; behaves like a direct
	// emit).
	emitWithPoly := func(verb, canonical, framework, refKind, refName, polySubscript string) {
		if state != nil {
			state.pendingPolySubscript = polySubscript
		}
		emit(verb, canonical, framework, refKind, refName)
		if state != nil {
			state.pendingPolySubscript = ""
		}
	}

	// fetch(...) — static string literals
	for _, m := range fetchCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		// FindAllStringSubmatchIndex returns 2*(N+1) ints. m[0..1] is the
		// full match, m[2..3] is group 1 (path), m[4..5] is group 2 (opts).
		raw := content[m[2]:m[3]]
		verb := "GET"
		if len(m) >= 6 && m[4] >= 0 {
			opts := content[m[4]:m[5]]
			if mv := fetchMethodRe.FindStringSubmatch(opts); len(mv) >= 2 {
				verb = strings.ToUpper(mv[1])
			}
		}
		// #807: normalize before URL-path check (strips query strings etc.)
		path, ok := normalizeRawClientPath(raw)
		if !ok {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
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
		caller := enclosingJSFuncAt(funcs, m[0])
		for _, exp := range canonicalizeTemplateLiteralExpand(tmpl, syms, objSyms, arrowFns) {
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, exp.Path)
			emitWithPoly(verb, canonical, "fetch", "Function", caller, exp.PolySubscript)
		}
	}

	// fetch(url, ...) — bare identifier whose value is a template literal (#654)
	// e.g.: const url = `${process.env.API_URL}/users`; fetch(url, { method: "POST" });
	// Build the template literal symbol table (template-body values, not plain strings).
	tmplSyms := buildJSTemplateLiteralSymbolTable(content)
	if len(tmplSyms) > 0 {
		for _, m := range fetchBareIdentRe.FindAllStringSubmatchIndex(content, -1) {
			if len(m) < 4 {
				continue
			}
			ident := content[m[2]:m[3]]
			// Skip if the identifier is a known string const (handled by fetchCallRe).
			if _, ok := syms[ident]; ok {
				continue
			}
			tmplBody, ok := tmplSyms[ident]
			if !ok {
				continue
			}
			verb := "GET"
			if len(m) >= 6 && m[4] >= 0 {
				opts := content[m[4]:m[5]]
				if mv := fetchMethodRe.FindStringSubmatch(opts); len(mv) >= 2 {
					verb = strings.ToUpper(mv[1])
				}
			}
			caller := enclosingJSFuncAt(funcs, m[0])
			for _, exp := range canonicalizeTemplateLiteralExpand(tmplBody, syms, objSyms, arrowFns) {
				canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, exp.Path)
				emitWithPoly(verb, canonical, "fetch", "Function", caller, exp.PolySubscript)
			}
		}
	}

	// axios.<verb>(...) — static string literals
	for _, m := range axiosLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		// #807: normalize before URL-path check
		path, ok := normalizeRawClientPath(raw)
		if !ok {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "axios", "Function", caller)
	}

	// axios.<verb>(`...${...}...`) — template literal URLs
	for _, m := range axiosLiteralTemplateLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		tmpl := content[m[4]:m[5]]
		caller := enclosingJSFuncAt(funcs, m[0])
		for _, exp := range canonicalizeTemplateLiteralExpand(tmpl, syms, objSyms, arrowFns) {
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, exp.Path)
			emitWithPoly(verb, canonical, "axios", "Function", caller, exp.PolySubscript)
		}
	}

	// <ident>{HttpClient,Client,httpClient,apiClient}.<verb>(...) — static string literals
	for _, m := range axiosClientRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := content[m[6]:m[7]]
		// #807: normalize before URL-path check
		path, ok := normalizeRawClientPath(raw)
		if !ok {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "http_client", "Function", caller)
	}

	// <ident>{HttpClient,Client,httpClient,apiClient}.<verb>(`...${...}...`) — template literal URLs
	for _, m := range axiosClientTemplateLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		tmpl := content[m[6]:m[7]]
		caller := enclosingJSFuncAt(funcs, m[0])
		for _, exp := range canonicalizeTemplateLiteralExpand(tmpl, syms, objSyms, arrowFns) {
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, exp.Path)
			emitWithPoly(verb, canonical, "http_client", "Function", caller, exp.PolySubscript)
		}
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
	//
	// Phase 5 (#806): also accepts bare resource names (no leading /)
	// when the wrapper name is recognized as HTTP-aware (Option A heuristic
	// or Option B per-repo config). Bare names are normalized to /name/.
	synthesizeWrapperCalls(content, funcs, syms, objSyms, nil, emit, state)

	// Note: React Query / RTK Query synthesis is handled in the early-exit
	// section at the top of this function (Phase 5 / #806). No second call needed.

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
	synthesizeAxiosInstanceCalls(content, funcs, syms, objSyms, instances, emit, state)

	// -----------------------------------------------------------------
	// Phase 4 (#712): bare const-variable path resolution
	// -----------------------------------------------------------------
	//
	// Handles `$http.get(BASE_PATH)` and `instance.get(PATH_VAR)` where
	// the path is a file-local string constant (not a quoted literal).
	// The symbol table already exists from the template-literal phase.
	synthesizeBareIdentifierCalls(content, funcs, syms, instances, emit)

	// -----------------------------------------------------------------
	// #1483 — NestJS HttpService (RxJS) + Apollo Client URI
	// -----------------------------------------------------------------
	synthesizeNestHttpService(content, funcs, syms, emit)
	synthesizeApolloClientURI(content, funcs, emit)
}

// jsRuntimeEmitFn is the runtime-dynamic-aware emitter type used by
// synthesizeFetchAxiosWithRuntime. Mirrors pyClientEmitFn / javaClientEmitFn.
//
// #2709 extension: the polySubscript argument carries an optional
// `<ident>[<keyExpr>]` tag identifying entries produced by static
// enumeration of an object-subscript template-literal interpolation. The
// downstream emit closure stamps this on the entity's Properties.
//
// #5527 extension: the trailing extractionMethod argument is "ast" for client
// synthetics extracted by the tree-sitter pass (synthesizeFetchAxiosAST) and
// "" for the regex passes. The downstream emit closure stamps it as the
// `extraction_method` property so endpoint-stats confidence is honest per
// entity rather than per framework name.
type jsRuntimeEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool, polySubscript, extractionMethod string)

// synthesizeFetchAxiosWithRuntime is the #721 entry point for JS/TS
// consumer-side synthesis. It calls the existing synthesizeFetchAxios for
// all static/template-literal patterns (delegating via an adapter) and
// additionally scans for env-var URL concatenations, emitting those with
// runtime_dynamic=true.
func synthesizeFetchAxiosWithRuntime(content, lang string, emit jsRuntimeEmitFn) {
	// Delegate all existing static/template/wrapper/axios-instance patterns
	// through the adapter. The adapter bridges emitFn → jsRuntimeEmitFn
	// with runtimeDynamic=false (these URLs are known at analysis time).
	//
	// #2709 — the shared clientSynthState carries the most-recent poly
	// subscript marker just-before each emit; the adapter forwards it and
	// then trusts synthesizeFetchAxios to clear / reset for the next call.
	state := newClientSynthState()
	adapter := func(method, canonicalPath, framework, refKind, refName string) {
		emit(method, canonicalPath, framework, refKind, refName, false, state.pendingPolySubscript, state.pendingExtractionMethod)
	}
	// #5527 — tree-sitter AST pass runs FIRST so its emits win the side-scoped
	// ID dedup in makeEmit and earn the `extraction_method=ast` stamp. The regex
	// passes below re-claim the same IDs (no-op via dedup) and pick up only the
	// calls the AST pass could not statically resolve, which stay heuristic.
	synthesizeFetchAxiosAST(content, lang, adapter, state)
	synthesizeFetchAxios(content, adapter, state)

	// #6433 — receiver-style client calls (`this.http.<verb><T>(<expr>)`) whose
	// URL argument is not statically resolvable. Runs on the real
	// jsRuntimeEmitFn (not the adapter) because it must stamp
	// runtime_dynamic=true, and BEFORE the process.env early-exit below because
	// an Angular service contains neither `process.env` nor `import.meta.env`.
	if hasInjectedHTTPClientToken(content) {
		// #6552 — the bare-identifier fold resolves through a scope-aware AST
		// table, built lazily so a file with no bare-identifier URL argument
		// never pays for a second parse. buildJSScopedConstTable returns nil on
		// a parse failure, and a nil table declines every lookup, so the fold
		// degrades to the honest /{dynamic} marker rather than guessing.
		var scoped *jsScopedConstTable
		scopedBuilt := false
		resolveConst := func(name string, pos uint32) (string, bool) {
			if !scopedBuilt {
				scoped, scopedBuilt = buildJSScopedConstTable(content, lang), true
			}
			return scoped.resolve(name, pos)
		}
		synthesizeReceiverClientResidualCalls(
			content,
			indexJSEnclosingFunctions(content),
			buildJSConstantSymbolTable(content),
			resolveConst,
			emit,
		)
	}

	// Env-var concatenation patterns — emit with runtimeDynamic=true.
	if !strings.Contains(content, "process.env") && !strings.Contains(content, "import.meta.env") {
		return
	}
	funcs := indexJSEnclosingFunctions(content)

	// fetch(process.env.X + "/path", ...) and fetch(import.meta.env.Y + "/path")
	for _, m := range fetchEnvConcatRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		suffix := content[m[2]:m[3]]
		if !looksLikeURLPath(suffix) {
			continue
		}
		verb := "GET"
		if len(m) >= 6 && m[4] >= 0 {
			opts := content[m[4]:m[5]]
			if mv := fetchMethodRe.FindStringSubmatch(opts); len(mv) >= 2 {
				verb = strings.ToUpper(mv[1])
			}
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, stripURLHost(suffix))
		caller := enclosingJSFuncAt(funcs, m[0])
		emit(verb, canonical, "fetch", "Function", caller, true, "", "")
	}

	// axios.<verb>(process.env.X + "/path")
	for _, m := range axiosEnvConcatRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := content[m[4]:m[5]]
		if !looksLikeURLPath(suffix) {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, stripURLHost(suffix))
		caller := enclosingJSFuncAt(funcs, m[0])
		emit(verb, canonical, "axios", "Function", caller, true, "", "")
	}

	// <ident>.<verb>(process.env.X + "/path") — $http, apiClient, etc.
	for _, m := range clientEnvConcatRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		receiver := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		suffix := content[m[6]:m[7]]
		// Filter: only known HTTP-client receivers (same guard as axiosClientRe).
		instances := buildAxiosInstanceTable(content)
		if !isBareIdentHTTPReceiver(receiver, instances) && receiver != "axios" {
			continue
		}
		if !looksLikeURLPath(suffix) {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, stripURLHost(suffix))
		caller := enclosingJSFuncAt(funcs, m[0])
		emit(verb, canonical, "http_client", "Function", caller, true, "", "")
	}
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
//
// Phase 5 (#806): when the wrapper name is recognized as HTTP-aware via
// Option A (heuristic) or Option B (per-repo wrappers.json config, passed
// in wrapperIdx), bare resource names (no leading /) are accepted and
// normalized to /name/.
//
// wrapperIdx may be nil — in that case only Option A heuristics apply.
func synthesizeWrapperCalls(content string, funcs []jsFuncSpan, syms map[string]string, objSyms map[string]map[string]string, wrapperIdx WrapperConfigIndex, emit emitFn, state *clientSynthState) {
	if wrapperIdx == nil {
		wrapperIdx = WrapperConfigIndex{}
	}
	// #2708 — Build the arrow-fn template table for factory inlining.
	arrowFns := buildJSArrowFnTemplateTable(content)
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

		// Resolve URL to a canonical path. Template literals may expand to
		// MULTIPLE paths (#2709: enumeration of object-subscript interps);
		// scalar shapes always yield a single expansion.
		var expansions []templateExpansion
		if isTemplate && strings.Contains(rawURL, "${") {
			expansions = canonicalizeTemplateLiteralExpand(rawURL, syms, objSyms, arrowFns)
		} else {
			candidate := stripURLHost(rawURL)
			if looksLikeURLPath(candidate) {
				expansions = []templateExpansion{{Path: candidate}}
			} else if IsHTTPWrapperHeuristic(wrapper, wrapperIdx) {
				// Bare resource name from a recognized HTTP wrapper:
				// normalize "checklists" → "/checklists/" and accept.
				normalized := normalizeBareName(candidate)
				if normalized != "" && normalized != "/" {
					expansions = []templateExpansion{{Path: normalized}}
				}
			}
		}
		if len(expansions) == 0 {
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
		for _, exp := range expansions {
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, exp.Path)
			if state != nil {
				state.pendingPolySubscript = exp.PolySubscript
			}
			emit(verb, canonical, "http_wrapper", "Function", caller)
			if state != nil {
				state.pendingPolySubscript = ""
			}
		}
	}
}

// synthesizeReactQueryCalls emits FETCHES edges for RTK Query createApi
// endpoint-builder patterns (#806 beyond-minimum).
//
// Recognized patterns:
//   - createApi builder.query({ query: () => 'resource' })
//     → FETCHES to /resource/
//
// IMPORTANT (#3171): React Query / SWR `queryKey` and `mutationKey` arrays are
// CACHE KEYS, not request URLs. Code such as
//
//	useQuery({ queryKey: ['scoped-permissions'], queryFn: () => api.get('permissions/123/scope_permissions') })
//
// names its cache entry 'scoped-permissions' — a logical label that very often
// does NOT match any backend path. Treating the queryKey first element as an
// endpoint path fabricated phantom calls (e.g. /scoped-permissions) that no
// backend exposes, inflating cross-repo orphan counts and producing confidently
// wrong MCP answers. The REAL endpoint is whatever the queryFn / mutationFn body
// invokes (fetch / axios / a service method), which is already extracted by
// synthesizeFetchAxios and the http-wrapper passes. We therefore do NOT derive
// any endpoint from queryKey / mutationKey arrays here.
//
// RTK Query's `builder.query({ query: () => 'users' })` is different: the arrow
// return value IS the literal request path, so it remains a valid endpoint
// source and is kept below.
//
// The enclosing function at the call site is used as source_caller.
func synthesizeReactQueryCalls(content string, funcs []jsFuncSpan, syms map[string]string, emit emitFn) {
	// Only RTK Query builder endpoints are synthesized here; useQuery /
	// useMutation key arrays are intentionally ignored (#3171).
	_ = syms
	if !strings.Contains(content, "createApi") && !strings.Contains(content, "builder.") {
		return
	}

	// RTK Query builder.query/mutation patterns.
	// rtkQueryEndpointRe group layout: [0]=full match, [1,2]=method, [3,4]=resource.
	for _, m := range rtkQueryEndpointRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		// Group 1: builder method ("query" or "mutation").
		builderMethod := content[m[2]:m[3]]
		// Group 2: resource name.
		resource := content[m[4]:m[5]]
		if resource == "" {
			continue
		}
		normalized := normalizeBareName(resource)
		if normalized == "" || normalized == "/" {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, normalized)
		caller := enclosingJSFuncAt(funcs, m[0])
		// Determine verb from captured builder method: query → GET, mutation → POST.
		verb := "GET"
		if builderMethod == "mutation" {
			verb = "POST"
		}
		emit(verb, canonical, "rtk_query", "Function", caller)
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

// httpClientFactoryRe matches assignments of an HTTP-client instance from a
// project-level factory function rather than a direct `axios.create()`, e.g.:
//
//	const orders = serviceClient(process.env.ORDERS_URL || "http://orders:8000");
//	const catalog = httpClient("http://catalog:3001");
//	const api = makeClient(baseURL);
//
// This is the ShipFast `serviceClient(...)` convention (#1418/#1422): a thin
// wrapper around `axios.create({ baseURL })` exported from a shared lib. The
// resulting variable is an axios instance whose `.get/.post/...` calls must be
// recognised as consumer-side HTTP calls. We detect by FACTORY NAME SHAPE —
// any function whose name contains "client" (case-insensitive) — to avoid
// hardcoding `serviceClient`. The first string-literal argument (or the
// literal inside a `X || "literal"` default) is used as the baseURL when it
// looks like a URL; otherwise the instance carries no static baseURL.
//
// Capture groups: 1 = instance name, 2 = factory name, 3 = whole arg list.
var httpClientFactoryRe = regexp.MustCompile(
	`(?m)(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*([A-Za-z_$][\w$]*[Cc]lient)\s*\(([^;\n\r]*)\)`,
)

// factoryBaseURLRe pulls the first http(s) URL string literal out of a
// factory call's argument list, e.g. from
// `process.env.ORDERS_URL || "http://orders:8000"` → `http://orders:8000`.
var factoryBaseURLRe = regexp.MustCompile(
	`['"]((?:https?://|/)[^'"\n\r]*)['"]`,
)

// buildAxiosInstanceTable scans the file for axios.create() declarations and
// HTTP-client factory assignments (#1418/#1422), returning a map from
// instance-name → metadata.
func buildAxiosInstanceTable(content string) map[string]axiosInstance {
	out := make(map[string]axiosInstance)
	if strings.Contains(content, "axios.create") {
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
	}
	// HTTP-client factory assignments (serviceClient/httpClient/makeClient/…).
	for _, m := range httpClientFactoryRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := m[1]
		// Don't clobber an axios.create() instance with the same name.
		if _, exists := out[name]; exists {
			continue
		}
		base := ""
		if bm := factoryBaseURLRe.FindStringSubmatch(m[3]); len(bm) >= 2 {
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
	objSyms map[string]map[string]string,
	instances map[string]axiosInstance,
	emit emitFn,
	state *clientSynthState,
) {
	// #2708 — Arrow-fn template-literal factory table for inlining
	// `${base(args)}` interpolations inside axios-instance call URLs.
	arrowFns := buildJSArrowFnTemplateTable(content)
	emitMatch := func(receiver, verb, path string, isTemplate bool, pos int) {
		var expansions []templateExpansion
		if isTemplate {
			expansions = canonicalizeTemplateLiteralExpand(path, syms, objSyms, arrowFns)
		} else {
			candidate := stripURLHost(path)
			if looksLikeURLPath(candidate) {
				expansions = []templateExpansion{{Path: candidate}}
			}
		}
		if len(expansions) == 0 {
			return
		}

		caller := enclosingJSFuncAt(funcs, pos)
		for _, exp := range expansions {
			resolved := exp.Path
			// baseURL composition.
			if inst, found := instances[receiver]; found && inst.baseURL != "" {
				resolved = composeBaseURL(inst.baseURL, resolved)
			}
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, resolved)
			if state != nil {
				state.pendingPolySubscript = exp.PolySubscript
			}
			emit(strings.ToUpper(verb), canonical, "axios_instance", "Function", caller)
			if state != nil {
				state.pendingPolySubscript = ""
			}
		}
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
	// end is the EXCLUSIVE byte offset just past the function body's closing
	// `}` (or, for an expression-bodied arrow, just past the terminating `;`).
	// The interval is half-open: a call site at `pos` is inside the span iff
	// `offset <= pos && pos < end`. The half-open-ness is load-bearing — see
	// the boundary case in js_py_span_end_6500_test.go, where a call match
	// begins on the newline that IS the sibling's end offset.
	//
	// end == offset means "no end could be established" (an unterminated body,
	// a TS overload signature with no body, a header longer than
	// jsSpanHeaderBudget). Such a span contains nothing, and the walk falls
	// back to its pre-#6500 answer.
	//
	// That is the BENIGN failure mode, and it is not the only one. The brace
	// scan is blind to strings, template literals and comments, so a stray
	// `{` in dead or quoted text can leave the scan unbalanced and carry the
	// end PAST the real body — over-extending the span across later
	// declarations. An over-long span does not degrade to the status quo: it
	// contains call sites it should not, out-ranks the fallback, and yields a
	// caller that differs from the pre-#6500 answer. Both directions are
	// pinned in TestJSSpanEnd_UnmaskedBraceOverExtends_6500. Masking dead and
	// quoted text is the fix, is owed from #6447, and is not attempted here.
	end  int
	name string
}

// jsSpanHeaderBudget bounds the scan between a declaration's `)` and its body
// `{` — the region a return-type annotation occupies.
//
// Its EFFECT is pinned, not its rationale. A header longer than this yields no
// end at all, so the declaration claims nothing and its own call sites fall
// through to the preceding declaration: a mis-attribution the budget causes
// rather than prevents. TestJSSpanEnd_HeaderBudgetCosts_6500 records that cost
// on a 652-byte generic return annotation. Raising the budget IMPROVES that
// case; the test says so and tells you to update it deliberately.
//
// The value is a defensive bound on an otherwise unbounded forward scan. No
// test observes it preventing anything, and the earlier claim here that it
// stopped a runaway scan from minting an overlapping span was never
// demonstrated, so it has been removed rather than left standing.
const jsSpanHeaderBudget = 512

// jsSpanBodyBudget bounds the two forward scans that have no natural stopping
// point: closing a declaration's parameter list (findMatchingParenAfter, in
// both jsSpanEnd and pySpanEnd) and walking an expression-bodied arrow to the
// end of its statement (jsStatementEnd).
const jsSpanBodyBudget = 4096

// jsSpanEnd computes the exclusive end offset of the function body for a
// jsFuncDeclRe match that ends at `matchEnd`.
//
// Alternative 4 of jsFuncDeclRe (method shorthand) ends ON the body `{`, so its
// caller passes bodyBraceKnown=true and the brace is at matchEnd-1.
// Alternatives 1-3 end just past the parameter list's opening `(`, so we close
// the parens first and then look for the body.
//
// Returns -1 when no end can be established.
//
// The scan is blind to string literals, template literals and comments,
// exactly as jsFuncDeclRe itself already is. That cuts BOTH ways and the
// permissive direction is the one that matters: a `}` in quoted text ends the
// span early (harmless — the call site falls out and lands on the fallback),
// but a `{` in quoted text leaves the scan a level down and carries the end
// past the real body, swallowing later declarations and their call sites.
// TestJSSpanEnd_UnmaskedBraceOverExtends_6500 pins a two-line example of the
// second. Masking is the fix and is owed from #6447.
func jsSpanEnd(content string, matchEnd int, bodyBraceKnown bool) int {
	open := -1
	if bodyBraceKnown {
		if matchEnd-1 < 0 || matchEnd-1 >= len(content) || content[matchEnd-1] != '{' {
			return -1
		}
		open = matchEnd - 1
	} else {
		closeParen := findMatchingParenAfter(content, matchEnd, jsSpanBodyBudget)
		if closeParen < 0 {
			return -1
		}
		i := skipJSSpace(content, closeParen+1)
		// An arrow function: `) => { ... }` or `) => expr;`.
		if i+1 < len(content) && content[i] == '=' && content[i+1] == '>' {
			i = skipJSSpace(content, i+2)
			if i < len(content) && content[i] == '{' {
				open = i
			} else {
				return jsStatementEnd(content, i)
			}
		} else {
			// A `function`/method header: whitespace and an optional return
			// annotation stand between `)` and `{`. Stop at `;` — that is a TS
			// overload or ambient declaration with no body at all.
			limit := i + jsSpanHeaderBudget
			if limit > len(content) {
				limit = len(content)
			}
			for ; i < limit; i++ {
				if content[i] == '{' {
					open = i
					break
				}
				if content[i] == ';' {
					return -1
				}
			}
			if open < 0 {
				return -1
			}
		}
	}
	depth := 0
	for i := open; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func skipJSSpace(content string, i int) int {
	for i < len(content) {
		switch content[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// jsStatementEnd returns the exclusive end of the expression starting at `i`:
// the first `;` or newline seen at bracket depth zero. Used for an
// expression-bodied arrow (`const base = (c) => BASE + c;`), whose "body" is
// the rest of the statement.
func jsStatementEnd(content string, i int) int {
	depth := 0
	limit := i + jsSpanBodyBudget
	if limit > len(content) {
		limit = len(content)
	}
	for ; i < limit; i++ {
		switch content[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				return i
			}
		case ';':
			if depth == 0 {
				return i + 1
			}
		case '\n':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func indexJSEnclosingFunctions(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range jsFuncDeclRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := ""
		// Group 1 (function foo(...)) takes precedence over group 2 (const foo = ...)
		// which takes precedence over group 3 (class property arrow: foo = (...) =>),
		// which takes precedence over group 4 (method shorthand: foo(...) { ).
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		case len(m) >= 8 && m[6] >= 0:
			name = content[m[6]:m[7]]
		case len(m) >= 10 && m[8] >= 0:
			name = content[m[8]:m[9]]
			// Only alternative 4 can capture a reserved word: shapes 1-3 each
			// require `function` / `const` / `let` / `var` / `=` beside the
			// name, which no control-flow keyword carries. See
			// jsMethodShorthandReserved for why a miss here is the costly
			// direction.
			if jsMethodShorthandReserved[name] {
				continue
			}
		}
		if name == "" {
			continue
		}
		// Alternative 4 is the only one whose match ends on the body `{`.
		bodyBraceKnown := len(m) >= 10 && m[8] >= 0
		end := jsSpanEnd(content, m[1], bodyBraceKnown)
		if end < m[0] {
			end = m[0] // degenerate: contains nothing, falls back to the old walk
		}
		out = append(out, jsFuncSpan{offset: m[0], end: end, name: name})
	}
	return out
}

// enclosingJSFuncAt returns the name of the function CONTAINING the call site
// at `pos` — the innermost such, when spans nest.
//
// #6500 arm A. Spans previously carried no end, so this was a nearest-PRECEDING
// walk: a call site attributed to the last declaration that STARTED before it,
// even one whose body had already closed. Now a span with a real end only
// claims a call site it actually contains, and since `funcs` is in ascending
// offset order the LAST containing span is the innermost one.
//
// Deliberately preserved: when NO span contains `pos`, this still returns the
// nearest preceding name — the pre-#6500 answer, wrong shapes included. Arm A
// does not change what any of the 43 consumers do for an unenclosed call site.
// That is arm B, and it is blocked on a policy decision rather than pending
// work: sse_edges.go builds a Stream entity's ID as `"/" + caller` and
// websocket_edges.go uses the caller as a WS_CONNECTS `FromID`, so returning ""
// there would rename graph nodes rather than blank a property. The fallback is
// pinned in TestSpanEnd_NoEnclosingSpanBehaviourIsPreserved_6500.
//
// The fallback absorbs one class of mis-computed end and not the other. A span
// that comes out too SMALL loses its call sites to the fallback, which is the
// pre-#6500 answer — no worse than before. A span that comes out too LARGE
// (an unbalanced brace scan; see jsSpanEnd) claims call sites outside the real
// body and out-ranks the fallback, producing a caller that pre-#6500 code would
// not have produced. Because sse_edges.go folds the caller into a Stream
// entity's ID, that direction renames a node rather than mis-labelling a
// property. Pinned in TestJSSpanEnd_UnmaskedBraceOverExtends_6500.
func enclosingJSFuncAt(funcs []jsFuncSpan, pos int) string {
	contained := ""
	found := false
	fallback := ""
	for _, f := range funcs {
		if f.offset > pos {
			break
		}
		fallback = f.name
		if pos < f.end {
			contained = f.name
			found = true
		}
	}
	if found {
		return contained
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Python: shared span types and helpers
// ---------------------------------------------------------------------------
//
// The Python consumer-side synthesizer moved to http_endpoint_python_client.go
// (#721). The span types and index helpers are retained here because they
// are part of the shared engine package used by tests and other passes.

// pyFuncSpan was a type ALIAS of jsFuncSpan until #6500. The alias was
// tenable only while a span carried nothing but (offset, name). Once a span
// carries an `end`, the two types stop being the same thing: a JS body is
// delimited by balanced braces and a Python body is delimited by INDENTATION,
// so sharing jsSpanEnd would compute a Python end from braces the language
// does not have. The types are split for that reason and not for tidiness —
// see TestPySpanEnd_ClosedSiblingDoesNotCapture_6500, which goes red if the
// brace logic is applied to the Python path.
type pyFuncSpan struct {
	offset int
	// end is exclusive, with the same half-open convention as jsFuncSpan.end,
	// and the same degenerate case: end == offset means no end was
	// established and the span contains nothing.
	end  int
	name string
}

// pySpanEnd returns the exclusive end of the `def` whose match starts at
// `defStart` (the first byte of the line's indentation, because
// pyEnclosingFuncRe anchors on `^[ \t]*`) and whose parameter list opens at
// `afterParen`.
//
// The body of a Python function runs to the first subsequent NON-BLANK line
// whose indentation is no deeper than the `def` line's own. Blank and
// whitespace-only lines carry no indentation to compare and are skipped; a
// comment line does not get that exemption, so a column-0 comment inside a
// body ends the span early — which drops call sites onto the preserved
// fallback rather than mis-attributing them.
//
// Indentation is counted in CHARACTERS, treating a tab as one. Python itself
// forbids mixing tabs and spaces for indentation in a single block, so within
// one body the comparison is consistent.
func pySpanEnd(content string, defStart, afterParen int) int {
	defIndent := 0
	for i := defStart; i < len(content) && (content[i] == ' ' || content[i] == '\t'); i++ {
		defIndent++
	}
	// The header may wrap across lines, so find the parameter list's `)`
	// before looking for the newline that ends the header.
	closeParen := findMatchingParenAfter(content, afterParen, jsSpanBodyBudget)
	if closeParen < 0 {
		return -1
	}
	nl := strings.IndexByte(content[closeParen:], '\n')
	if nl < 0 {
		return len(content)
	}
	i := closeParen + nl + 1
	for i < len(content) {
		lineEnd := len(content)
		if nl := strings.IndexByte(content[i:], '\n'); nl >= 0 {
			lineEnd = i + nl + 1
		}
		indent := 0
		for j := i; j < lineEnd && (content[j] == ' ' || content[j] == '\t'); j++ {
			indent++
		}
		blank := strings.TrimSpace(content[i:lineEnd]) == ""
		if !blank && indent <= defIndent {
			return i
		}
		i = lineEnd
	}
	return len(content)
}

// indexPyEnclosingFunctions builds a sorted (offset, name) list for every
// Python function definition recognisable by pyEnclosingFuncRe. Used by
// the Python consumer-side synthesizer (http_endpoint_python_client.go)
// and by tests.
func indexPyEnclosingFunctions(content string) []pyFuncSpan {
	var out []pyFuncSpan
	for _, m := range pyEnclosingFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		end := pySpanEnd(content, m[0], m[1])
		if end < m[0] {
			end = m[0]
		}
		out = append(out, pyFuncSpan{offset: m[0], end: end, name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingPyFuncAt mirrors enclosingJSFuncAt over Python spans: innermost
// CONTAINING span wins, and when none contains `pos` the pre-#6500
// nearest-preceding answer is preserved unchanged (see enclosingJSFuncAt for
// why that fallback is deliberate). It is a separate body rather than a
// delegation because pyFuncSpan is no longer an alias of jsFuncSpan.
func enclosingPyFuncAt(funcs []pyFuncSpan, pos int) string {
	contained := ""
	found := false
	fallback := ""
	for _, f := range funcs {
		if f.offset > pos {
			break
		}
		fallback = f.name
		if pos < f.end {
			contained = f.name
			found = true
		}
	}
	if found {
		return contained
	}
	return fallback
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

// normalizeRawClientPath is the entry-point normalizer for static-literal
// URL paths extracted by the client synthesizers (fetch, axios, requests,
// etc.). It applies normalizePath (#807) to strip query strings and
// env-var prefixes, then calls stripURLHost to remove scheme://host.
//
// Returns ("", false) when the result is not a usable URL path.
func normalizeRawClientPath(raw string) (string, bool) {
	normed := normalizePath(raw)
	path := stripURLHost(normed.Path)
	if !looksLikeURLPath(path) {
		return "", false
	}
	return path, true
}
