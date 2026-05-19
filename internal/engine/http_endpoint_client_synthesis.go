// Client-side (consumer) synthetic http_endpoint emission for typed-HTTP
// cross-repo matching (issue #533, Phase 1).
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
// Phase 1 covers STATIC URL literals only:
//   - JS/TS:   fetch("/users/123"), fetch("/users/123", {method:"POST"}),
//     axios.<verb>("/path", ...), httpClient.<verb>("/path", ...)
//   - Python:  requests.<verb>("/path"), httpx.<verb>("/path"),
//     aiohttp.ClientSession.<verb>("/path"), session.<verb>("/path")
//
// Deferred to Phase 2 chain-fixes (filed per #533 spec):
//   - Template literals: fetch(`/users/${id}/posts`)
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

// synthesizeFetchAxios scans a JS/TS file and emits one synthetic
// http_endpoint per detected client call. Static URL literals only.
func synthesizeFetchAxios(content string, emit emitFn) {
	if !strings.Contains(content, "fetch(") &&
		!strings.Contains(content, "axios.") &&
		!strings.Contains(content, "Client.") &&
		!strings.Contains(content, "httpClient.") &&
		!strings.Contains(content, "apiClient.") {
		return
	}

	funcs := indexJSEnclosingFunctions(content)

	// fetch(...)
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

	// axios.<verb>(...)
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

	// <ident>{HttpClient,Client,httpClient,apiClient}.<verb>(...)
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
// Python: requests / httpx / aiohttp
// ---------------------------------------------------------------------------

// pyRequestsLiteralRe matches `requests.<verb>("path", ...)` and the
// identical `httpx.<verb>(...)` form. Both libraries expose the same
// top-level verb functions, so a single regex handles them.
var pyRequestsLiteralRe = regexp.MustCompile(
	`\b(requests|httpx)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*['"]([^'"\n\r]+)['"]`,
)

// pySessionClientRe matches `<ident>.<verb>("path", ...)` where ident is
// a typical session/client variable name. This catches:
//   - requests.Session() instances: `session.get(url)`
//   - httpx.Client / AsyncClient instances: `client.get(url)`
//   - aiohttp.ClientSession instances: `session.get(url)`
//
// We deliberately restrict the leading identifier to a small allow-list of
// names to avoid colliding with framework producer patterns (Flask /
// FastAPI use `@app.get(...)` / `@router.get(...)` as DECORATORS — those
// are preceded by `@`, which this regex's `\b<ident>\s*\.` anchor does
// not match, since `@` is not a word boundary on its left and `\b` only
// matches between word/non-word; `@app` -> the `\b` is at `@|a`, so it
// DOES match. We exclude `app` and `router` from the allow-list to be
// safe; if a project uses `app.get(url)` as a true HTTP call it will be
// missed in Phase 1 — file a Phase 2 chain-fix.
var pySessionClientRe = regexp.MustCompile(
	`\b(session|client|http_client|api_client|http|api)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*['"]([^'"\n\r]+)['"]`,
)

// pyAiohttpRe matches `ClientSession().<verb>("path", ...)` inline form
// and `async with session.get("path") as ...` which the session re above
// also handles. This separate matcher catches the awaited inline form
// `await aiohttp.ClientSession().get("path")`.
var pyAiohttpRe = regexp.MustCompile(
	`aiohttp\.ClientSession\s*\(\s*\)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*['"]([^'"\n\r]+)['"]`,
)

// pyEnclosingFuncRe captures `def <name>(` and `async def <name>(`.
var pyEnclosingFuncRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`,
)

// synthesizePyClient scans a Python file for HTTP client call sites.
func synthesizePyClient(content string, emit emitFn) {
	if !strings.Contains(content, "requests.") &&
		!strings.Contains(content, "httpx.") &&
		!strings.Contains(content, "aiohttp.") &&
		!strings.Contains(content, "session.") &&
		!strings.Contains(content, "client.") &&
		!strings.Contains(content, "http_client.") &&
		!strings.Contains(content, "api_client.") {
		return
	}

	funcs := indexPyEnclosingFunctions(content)

	// requests / httpx
	for _, m := range pyRequestsLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		framework := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		path := content[m[6]:m[7]]
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, stripURLHost(path))
		emit(verb, canonical, framework, "Function", caller)
	}

	// Session / client instances
	for _, m := range pySessionClientRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		// Skip decorator forms: a true match preceded by `@` is a Flask /
		// FastAPI route decorator on the producer side. We can't easily
		// constrain that in the regex without breaking `\b`, so check the
		// preceding byte manually.
		if m[0] > 0 && content[m[0]-1] == '@' {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		path := content[m[6]:m[7]]
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, stripURLHost(path))
		emit(verb, canonical, "http_client", "Function", caller)
	}

	// aiohttp inline
	for _, m := range pyAiohttpRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		path := content[m[4]:m[5]]
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, stripURLHost(path))
		emit(verb, canonical, "aiohttp", "Function", caller)
	}
}

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
