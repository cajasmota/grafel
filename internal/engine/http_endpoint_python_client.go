// Python consumer-side HTTP client synthesis (#721 wave 1).
//
// Emits one synthetic `http_endpoint` entity (consumer side) per detected
// HTTP client call site, AND a FETCHES edge from the enclosing function
// to that endpoint. The FETCHES edge is the new primitive introduced by
// #721: previously the cross-repo HTTP matcher (`internal/links/http_pass.go`)
// reconstructed consumer→producer links via a post-hoc Name match, but
// downstream consumers (process-flow BFS from #724, MCP graph queries)
// could not traverse directly from a calling function to its endpoint.
// With FETCHES emitted at extraction time, the edge is first-class.
//
// Patterns covered (per the wave-1 brief):
//
//   - requests.<verb>(url, ...) / requests.request(method, url, ...)
//   - httpx.<verb>(url, ...) (sync) + httpx.AsyncClient().<verb>(url, ...) (async)
//   - aiohttp.ClientSession().<verb>(url, ...) (inline async)
//   - urllib.request.urlopen(url) / urllib.request.Request(url)
//   - Session-style instances: session/client/http_client/api_client/http/api
//     with optional base_url / base composition
//
// Beyond-minimum behaviours:
//   - File-local constant folding for string URLs:
//     BASE = "/api/v1"
//     requests.get(f"{BASE}/users") → /api/v1/users
//   - f-string templates with simple `{name}` interpolation
//   - String concatenation: `os.environ["API_URL"] + "/users"` →
//     `/users` with `runtime_dynamic=true` (the host comes from env)
//   - Session(base_url=...) declarations folded onto subsequent calls
//   - urlopen / Request with absolute URLs collapsed to their path
//
// Files where this is wired in:
//   - http_endpoint_synthesis.go: `case "python":` calls synthesizePyClient
//     here (the JS/TS-residing legacy variant is removed in the same change).
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Top-level module patterns: requests, httpx, urllib
// ---------------------------------------------------------------------------

// pyTopLevelVerbRe matches `requests.<verb>(url, ...)` and
// `httpx.<verb>(url, ...)`. The url group accepts:
//   - Plain string literal:        "..."  or  '...'
//   - f-string literal:            f"..." or  f'...'
//   - Bare identifier (constant):  PATH
var pyTopLevelVerbRe = regexp.MustCompile(
	`\b(requests|httpx)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`f?["']([^"'\n\r]+)["']` + // group 3: literal / f-string body
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare identifier
		`)`,
)

// pyRequestsRequestRe matches `requests.request("METHOD", url, ...)` and
// `httpx.request("METHOD", url, ...)`. The verb is positional.
var pyRequestsRequestRe = regexp.MustCompile(
	`\b(requests|httpx)\s*\.\s*request\s*\(\s*["']([A-Za-z]+)["']\s*,\s*(?:` +
		`f?["']([^"'\n\r]+)["']` +
		`|` +
		`([A-Za-z_][\w]*)` +
		`)`,
)

// pyUrllibUrlopenRe matches `urllib.request.urlopen("url")` and
// `urlopen("url")` (the latter for `from urllib.request import urlopen`).
// Verb is always GET.
var pyUrllibUrlopenRe = regexp.MustCompile(
	`(?:urllib\.request\.)?\burlopen\s*\(\s*(?:` +
		`f?["']([^"'\n\r]+)["']` +
		`|` +
		`([A-Za-z_][\w]*)` +
		`)`,
)

// pyUrllibRequestCtorRe matches `urllib.request.Request("url", ...)` and
// `Request("url", ...)`. Verb is GET unless a method= kwarg specifies
// otherwise.
//
// We pick the method off a `method="POST"` kwarg trailing inside the
// constructor call. The kwarg may be on the same line.
var pyUrllibRequestCtorRe = regexp.MustCompile(
	`(?:urllib\.request\.)?\bRequest\s*\(\s*(?:` +
		`f?["']([^"'\n\r]+)["']` +
		`|` +
		`([A-Za-z_][\w]*)` +
		`)([^)]*)`,
)

// pyMethodKwargRe extracts `method="POST"` from trailing kwargs.
var pyMethodKwargRe = regexp.MustCompile(`method\s*=\s*["']([A-Za-z]+)["']`)

// ---------------------------------------------------------------------------
// Session / client instance patterns
// ---------------------------------------------------------------------------

// pySessionClientRe matches `<ident>.<verb>("path", ...)` where ident is
// a typical session/client variable name. This catches:
//   - requests.Session() instances:    session.get(url)
//   - httpx.Client / AsyncClient:      client.get(url)
//   - aiohttp.ClientSession instances: session.get(url)
//   - generic api_client / http_client / api / http names
//
// `app` and `router` are deliberately excluded because in Flask/FastAPI
// those receivers are used in DECORATOR form (`@app.get(...)`) for
// producer-side route registration. The leading-`@` guard in the body
// catches the rare imperative-call collision.
//
// Accepts string-literal, f-string, and bare-identifier URL arguments.
var pySessionClientRe = regexp.MustCompile(
	`\b(session|client|http_client|api_client|http|api)\s*\.\s*(get|post|put|patch|delete|head|options|request)\s*\(\s*(?:` +
		`f?["']([^"'\n\r]+)["']` + // group 3: string/f-string
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare ident
		`)`,
)

// pyContextManagerAliasRe matches `with` / `async with` context-manager
// forms that bind an httpx or requests client to a user-chosen variable:
//
//	async with httpx.AsyncClient() as c:
//	async with httpx.AsyncClient(base_url="...") as svc:
//	with httpx.Client(base_url="...") as http:
//	with requests.Session() as sess:
//
// Capture groups:
//
//	1 = client type  (AsyncClient | Client | Session)
//	2 = optional base_url value (may be empty)
//	3 = alias identifier
var pyContextManagerAliasRe = regexp.MustCompile(
	`(?:async\s+)?with\s+(?:httpx\.(?:Async)?Client|requests\.Session)\s*\(` +
		`[^)]*?(?:base_url\s*=\s*["']([^"'\n\r]*)["'])?[^)]*\)\s+as\s+([A-Za-z_]\w*)`,
)

// pyLocalVarStringRe captures simple local string assignments inside a
// function body:
//
//	pricing_endpoint = "http://pricing/api/v1/price"
//	url = f"/items/{item_id}"
//
// We deliberately allow both lower and UPPER names (unlike the module-level
// pyStringConstRe which skips pure-uppercase to avoid re-capturing constants
// a second time — here all names are equally valid).
//
// The indentation prefix is required (at least one whitespace char) to avoid
// matching module-level constants again; those are already handled by
// buildPyStringSymbolTable.
var pyLocalVarStringRe = regexp.MustCompile(
	`(?m)^[ \t]+([a-zA-Z_][\w]*)\s*=\s*(f?)["']([^"'\n\r]{1,512})["']`,
)

// pyAiohttpInlineRe matches `aiohttp.ClientSession().<verb>("path", ...)`.
// Captures verb + path.
var pyAiohttpInlineRe = regexp.MustCompile(
	`aiohttp\.ClientSession\s*\(\s*\)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`f?["']([^"'\n\r]+)["']` +
		`|` +
		`([A-Za-z_][\w]*)` +
		`)`,
)

// pyHttpxAsyncRe matches `httpx.AsyncClient().<verb>("path", ...)`.
var pyHttpxAsyncRe = regexp.MustCompile(
	`httpx\.AsyncClient\s*\(\s*\)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`f?["']([^"'\n\r]+)["']` +
		`|` +
		`([A-Za-z_][\w]*)` +
		`)`,
)

// ---------------------------------------------------------------------------
// Symbol tables: string constants, session base URLs, env-var sites
// ---------------------------------------------------------------------------

// pyStringConstRe captures simple top-level string assignments:
//
//	NAME = "value"            (preferred form)
//	NAME = '/path'
//
// Both single- and double-quoted forms are accepted. We do not try to
// parse multi-line concatenations or function-local assignments — the
// dominant convention in real-world Python codebases is a module-level
// `BASE = "/api/v1"` followed by uses through `requests.get(f"{BASE}/x")`.
var pyStringConstRe = regexp.MustCompile(
	`(?m)^[ \t]*([A-Z_][A-Z0-9_]*|[a-zA-Z_][\w]*)\s*=\s*["']([^"'\n\r]{1,256})["']`,
)

// pySessionBaseURLRe captures `session.base_url = "..."` assignments.
// requests.Session does not natively support base_url but many wrappers
// (e.g. requests-toolbelt's BaseUrlSession) monkey-patch it; either way
// the pattern is widely used and we treat it as a hint.
var pySessionBaseURLRe = regexp.MustCompile(
	`\b([a-zA-Z_][\w]*)\.base_url\s*=\s*["']([^"'\n\r]+)["']`,
)

// pyHttpxClientCtorRe matches `<name> = httpx.Client(base_url="...")` and
// `<name> = httpx.AsyncClient(base_url="...")`. Folded into the per-file
// instance table so subsequent `<name>.get("/p")` calls receive the
// base_url prefix when emitted.
var pyHttpxClientCtorRe = regexp.MustCompile(
	`([a-zA-Z_][\w]*)\s*=\s*httpx\.(?:Async)?Client\s*\([^)]*base_url\s*=\s*["']([^"'\n\r]+)["']`,
)

// pyEnvLookupRe matches `os.environ["NAME"]`, `os.environ.get("NAME"[, ...])`,
// and `os.getenv("NAME"[, ...])`. Used to flag runtime-dynamic URLs.
var pyEnvLookupRe = regexp.MustCompile(
	`\bos\.(?:environ\.get|getenv|environ\s*\[)\s*["'][A-Z_][A-Z0-9_]*["']`,
)

// pyEnvAccessFrag is the non-capturing regex fragment that matches any of
// the three common Python env-variable access forms:
//
//	os.environ["NAME"]       (bracket subscript — closes with ])
//	os.environ.get("NAME")   (method call — closes with ))
//	os.getenv("NAME")        (module-level call — closes with ))
//
// The fragment is used inside the concatenation regexes below. Each form
// handles its own closing delimiter, so we list them as three explicit
// alternatives.
const pyEnvAccessFrag = `(?:os\.environ\s*\["[^"]+"\]|os\.environ\.get\s*\("[^"]+"\)|os\.getenv\s*\("[^"]+"\))`

// pyEnvConcatVerbTopRe matches HTTP client calls where the URL is an env-var
// concatenation, e.g.:
//
//	requests.get(os.environ["API_URL"] + "/users", ...)
//	httpx.post(os.getenv("BASE") + "/items", json=body)
//	session.get(os.environ.get("API") + "/health")
//
// Capture groups:
//
//	1 = framework (requests/httpx)
//	2 = http verb
//	3 = path suffix literal (the string being concatenated after the env var)
var pyEnvConcatVerbTopRe = regexp.MustCompile(
	`\b(requests|httpx)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*` +
		pyEnvAccessFrag + `\s*\+\s*["']([^"'\n\r]*)["']`,
)

// pyEnvConcatSessionRe is the same but for session/client/etc. receiver forms.
var pyEnvConcatSessionRe = regexp.MustCompile(
	`\b(session|client|http_client|api_client|http|api)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*` +
		pyEnvAccessFrag + `\s*\+\s*["']([^"'\n\r]*)["']`,
)

// pyEnclosingFuncRe captures `def <name>(` and `async def <name>(`. Same
// shape as the legacy regex in http_endpoint_client_synthesis.go.
var pyEnclosingFuncRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`,
)

// pyFStringSubstRe matches `{<expr>}` inside an f-string body. We capture
// the leading identifier of the expression so simple `{user_id}` /
// `{user.id}` interpolations can be canonicalised to `{user_id}` /
// `{id}` placeholders for cross-repo matching.
var pyFStringSubstRe = regexp.MustCompile(`\{([^{}!:]+)(?:[!:][^{}]*)?\}`)

// pyIdentRe validates a single Python identifier.
var pyIdentRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)

// pyURLConstSuffixRe matches identifier names that are conventional URL/host
// constant names: ALL_CAPS identifiers, or names ending with _URL, _HOST,
// _ADDR, _ENDPOINT, _BASE, _SVC, _SERVICE. When such an identifier appears
// as the FIRST substitution in an f-string and its value is not in the
// symbol table (e.g. it was imported from another module), we treat it as
// a host/base prefix and strip it rather than emitting it as a path
// parameter placeholder like `/{PRICING_URL}/quote`.
var pyURLConstSuffixRe = regexp.MustCompile(
	`(?i)_(url|host|addr|endpoint|base|svc|service)$`,
)

// pyIsURLConstName reports whether name looks like a URL/host constant
// (all-uppercase, or matches a conventional suffix).
func pyIsURLConstName(name string) bool {
	if name == "" {
		return false
	}
	// All-uppercase (e.g. BASE, PRICING_URL, API_HOST).
	allUpper := true
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			allUpper = false
			break
		}
	}
	if allUpper {
		return true
	}
	return pyURLConstSuffixRe.MatchString(name)
}

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// pyClientEmitFn is the consumer-side emitter used by this file. It is a
// superset of the engine-wide `emitFn` signature: in addition to the
// canonical (method, canonicalPath, framework, refKind, refName) tuple,
// it accepts a `runtimeDynamic` flag so the caller can request
// `runtime_dynamic=true` on the emitted entity.
type pyClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizePyClient scans a Python file for HTTP client call sites and
// invokes `emit` for each. It is the package-level entry point referenced
// from applyHTTPEndpointSynthesis. The `emit` parameter is the standard
// engine-wide `emitFn`; FETCHES edge emission is handled by the caller
// (it has access to the relationships slice).
func synthesizePyClient(content string, emit emitFn) {
	synthesizePyClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizePyClientWithRuntime is the runtime-aware variant. The wave-1
// caller in applyHTTPEndpointSynthesis uses this path so it can stamp
// `runtime_dynamic=true` on env-var-derived URLs.
func synthesizePyClientWithRuntime(content string, emit pyClientEmitFn) {
	if !pyHasAnyHTTPClient(content) {
		return
	}
	funcs := indexPyEnclosingFunctions(content)
	syms := buildPyStringSymbolTable(content)
	locals := buildPyLocalVarTable(content)
	instances := buildPySessionInstanceTable(content)
	// Merge local variables into syms for URL resolution (locals win on
	// collision, as they are more specific than module-level constants).
	mergedSyms := make(map[string]string, len(syms)+len(locals))
	for k, v := range syms {
		mergedSyms[k] = v
	}
	for k, v := range locals {
		mergedSyms[k] = v
	}

	// requests.<verb>(...) / httpx.<verb>(...)
	for _, m := range pyTopLevelVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		framework := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw, isFString, dynamic := pyResolveURLArg(content, m, 6, mergedSyms)
		if raw == "" {
			continue
		}
		path, ok := pyCanonicalize(raw, isFString, mergedSyms)
		if !ok {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, framework, "Function", caller, dynamic)
	}

	// requests.request("METHOD", url, ...)
	for _, m := range pyRequestsRequestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		framework := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw, isFString, dynamic := pyResolveURLArg(content, m, 6, mergedSyms)
		if raw == "" {
			continue
		}
		path, ok := pyCanonicalize(raw, isFString, mergedSyms)
		if !ok {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, framework, "Function", caller, dynamic)
	}

	// urllib.request.urlopen(url) / urlopen(url)
	for _, m := range pyUrllibUrlopenRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		raw, isFString, dynamic := pyResolveURLArg(content, m, 2, mergedSyms)
		if raw == "" {
			continue
		}
		path, ok := pyCanonicalize(raw, isFString, mergedSyms)
		if !ok {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit("GET", canonical, "urllib", "Function", caller, dynamic)
	}

	// urllib.request.Request(url, ..., method="POST") / Request(url, ...)
	for _, m := range pyUrllibRequestCtorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		raw, isFString, dynamic := pyResolveURLArg(content, m, 2, mergedSyms)
		if raw == "" {
			continue
		}
		path, ok := pyCanonicalize(raw, isFString, mergedSyms)
		if !ok {
			continue
		}
		verb := "GET"
		if m[6] >= 0 && m[7] > m[6] {
			rest := content[m[6]:m[7]]
			if mm := pyMethodKwargRe.FindStringSubmatch(rest); len(mm) >= 2 {
				verb = strings.ToUpper(mm[1])
			}
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, "urllib", "Function", caller, dynamic)
	}

	// Session / client instance calls — static allowlist.
	for _, m := range pySessionClientRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		// Skip @decorator forms (producer-side Flask/FastAPI registrations).
		if m[0] > 0 && content[m[0]-1] == '@' {
			continue
		}
		receiver := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		// `session.request("METHOD", url)` is not handled here — Phase 1
		// only covers verb-method calls. The trailing-method form is in
		// the requests/httpx top-level matcher above.
		if verb == "REQUEST" {
			continue
		}
		raw, isFString, dynamic := pyResolveURLArg(content, m, 6, mergedSyms)
		if raw == "" {
			continue
		}
		path, ok := pyCanonicalize(raw, isFString, mergedSyms)
		if !ok {
			continue
		}
		// Compose base URL when receiver has one in the symbol table.
		if base, ok := instances[receiver]; ok && base != "" {
			path = composeBaseURL(base, path)
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, "http_client", "Function", caller, dynamic)
	}

	// Session / client instance calls — dynamic aliases from context managers
	// and explicit constructor assignments (e.g. `c = httpx.AsyncClient()`).
	// Only fires for aliases NOT already covered by the static allowlist above
	// to avoid double-emitting.
	if dynRe := pyBuildDynamicAliasRe(instances); dynRe != nil {
		for _, m := range dynRe.FindAllStringSubmatchIndex(content, -1) {
			if len(m) < 10 {
				continue
			}
			if m[0] > 0 && content[m[0]-1] == '@' {
				continue
			}
			receiver := content[m[2]:m[3]]
			verb := strings.ToUpper(content[m[4]:m[5]])
			if verb == "REQUEST" {
				continue
			}
			raw, isFString, dynamic := pyResolveURLArg(content, m, 6, mergedSyms)
			if raw == "" {
				continue
			}
			path, ok := pyCanonicalize(raw, isFString, mergedSyms)
			if !ok {
				continue
			}
			if base, ok := instances[receiver]; ok && base != "" {
				path = composeBaseURL(base, path)
			}
			caller := enclosingPyFuncAt(funcs, m[0])
			canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
			emit(verb, canonical, "http_client", "Function", caller, dynamic)
		}
	}

	// aiohttp inline form.
	for _, m := range pyAiohttpInlineRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw, isFString, dynamic := pyResolveURLArg(content, m, 4, mergedSyms)
		if raw == "" {
			continue
		}
		path, ok := pyCanonicalize(raw, isFString, mergedSyms)
		if !ok {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, "aiohttp", "Function", caller, dynamic)
	}

	// httpx.AsyncClient() inline form.
	for _, m := range pyHttpxAsyncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw, isFString, dynamic := pyResolveURLArg(content, m, 4, mergedSyms)
		if raw == "" {
			continue
		}
		path, ok := pyCanonicalize(raw, isFString, mergedSyms)
		if !ok {
			continue
		}
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, "httpx", "Function", caller, dynamic)
	}

	// Env-var concatenation: requests.get(os.environ["X"] + "/path")
	// and session.get(os.environ["X"] + "/path"). These emit with
	// runtime_dynamic=true so the repair flow (#732) can annotate them.
	for _, m := range pyEnvConcatVerbTopRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		framework := content[m[2]:m[3]]
		verb := strings.ToUpper(content[m[4]:m[5]])
		suffix := content[m[6]:m[7]]
		if suffix == "" || !looksLikeURLPath(suffix) {
			continue
		}
		path := stripURLHost(suffix)
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, framework, "Function", caller, true)
	}

	for _, m := range pyEnvConcatSessionRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		if m[0] > 0 && content[m[0]-1] == '@' {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		suffix := content[m[6]:m[7]]
		if suffix == "" || !looksLikeURLPath(suffix) {
			continue
		}
		path := stripURLHost(suffix)
		caller := enclosingPyFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		emit(verb, canonical, "http_client", "Function", caller, true)
	}
}

// pyStaticAliasSet is the set of receiver names already matched by the
// compiled pySessionClientRe. Used by pyBuildDynamicAliasRe to avoid
// emitting duplicates.
var pyStaticAliasSet = map[string]bool{
	"session": true, "client": true, "http_client": true,
	"api_client": true, "http": true, "api": true,
}

// pyBuildDynamicAliasRe builds a regex that matches `<alias>.<verb>(url)`
// for every alias in `instances` that is NOT already covered by the static
// pySessionClientRe allowlist. Returns nil when there are no new aliases.
func pyBuildDynamicAliasRe(instances map[string]string) *regexp.Regexp {
	var extras []string
	for name := range instances {
		if !pyStaticAliasSet[name] {
			extras = append(extras, regexp.QuoteMeta(name))
		}
	}
	if len(extras) == 0 {
		return nil
	}
	pattern := fmt.Sprintf(
		`\b(%s)\s*\.\s*(get|post|put|patch|delete|head|options|request)\s*\(\s*(?:`+
			`f?["']([^"'\n\r]+)["']`+
			`|`+
			`([A-Za-z_][\w]*)`+
			`)`,
		strings.Join(extras, "|"),
	)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pyHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "requests.") ||
		strings.Contains(content, "httpx.") ||
		strings.Contains(content, "aiohttp.") ||
		strings.Contains(content, "urlopen(") ||
		strings.Contains(content, "Request(") ||
		strings.Contains(content, "os.environ") ||
		strings.Contains(content, "os.getenv") ||
		strings.Contains(content, "session.") ||
		strings.Contains(content, "client.") ||
		strings.Contains(content, "http_client.") ||
		strings.Contains(content, "api_client.") ||
		strings.Contains(content, "http.") ||
		strings.Contains(content, "api.")
}

// buildPyStringSymbolTable returns a map from identifier name → string
// value for every simple module-level string assignment in the file.
func buildPyStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range pyStringConstRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		name := m[1]
		val := m[2]
		if _, dup := syms[name]; !dup {
			syms[name] = val
		}
	}
	return syms
}

// buildPySessionInstanceTable returns a map from session/client variable
// name → base URL. Sources:
//   - `<n> = httpx.Client(base_url="...")` and AsyncClient
//   - `<n>.base_url = "..."` mutation
//   - `with/async with httpx.AsyncClient(...) as <alias>` context managers
//   - `with/async with requests.Session() as <alias>` context managers
//
// The empty-string value "" is used when no base_url is known; the
// caller in synthesizePyClientWithRuntime only composes a prefix when the
// base is non-empty.
func buildPySessionInstanceTable(content string) map[string]string {
	out := make(map[string]string)
	for _, m := range pyHttpxClientCtorRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			out[m[1]] = stripURLHost(m[2])
		}
	}
	for _, m := range pySessionBaseURLRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			out[m[1]] = stripURLHost(m[2])
		}
	}
	// Context-manager alias bindings: `with httpx.AsyncClient() as c` etc.
	// Register every alias so it is recognised as a client receiver even when
	// the name is not in the pySessionClientRe allowlist.
	for _, m := range pyContextManagerAliasRe.FindAllStringSubmatch(content, -1) {
		// m[1] = optional base_url, m[2] = alias
		if len(m) >= 3 {
			alias := m[2]
			base := ""
			if m[1] != "" {
				base = stripURLHost(m[1])
			}
			if _, exists := out[alias]; !exists {
				out[alias] = base
			}
		}
	}
	return out
}

// buildPyLocalVarTable returns a map from local-variable name → string value
// for every indented (function-body-level) string assignment in the file.
// It intentionally includes all names regardless of case so that patterns
// like `pricing_endpoint = "http://..."` are resolved when used as a URL
// argument later in the same function.
//
// Note: this is a file-wide scan — we do not attempt true per-function
// scoping. False-positive variable capture across function boundaries is
// extremely rare in practice and the worst outcome is resolving a URL from
// another function in the same file (which still produces a valid edge).
func buildPyLocalVarTable(content string) map[string]string {
	out := make(map[string]string)
	for _, m := range pyLocalVarStringRe.FindAllStringSubmatch(content, -1) {
		// m[1]=name, m[2]="f" if f-string, m[3]=body
		if len(m) < 4 {
			continue
		}
		name := m[1]
		val := m[3]
		if _, dup := out[name]; !dup {
			out[name] = val
		}
	}
	return out
}

// pyResolveURLArg picks the URL argument from a match's submatch slice.
// `m` is the result of FindAllStringSubmatchIndex; `litStart` is the byte
// offset within `m` of the literal/f-string group (so the bare-identifier
// group is at `litStart+2`).
//
// Returns (rawURL, isFString, runtimeDynamic). When the literal group is
// captured but starts with `f` (because the regex `f?` prefix consumed
// it), isFString is true. When the bare-identifier path is taken, the
// symbol table resolves the value; if the identifier is not in the
// table, runtimeDynamic is true (we still have the call site, just no
// URL to resolve).
func pyResolveURLArg(content string, m []int, litStart int, syms map[string]string) (string, bool, bool) {
	// Literal / f-string group.
	if litStart+1 < len(m) && m[litStart] >= 0 {
		raw := content[m[litStart]:m[litStart+1]]
		// The regex `f?["']...` consumes the `f` outside the group; we
		// need to peek at the character just before the opening quote
		// to detect f-strings. The match start is m[litStart]-1 if it's
		// a quote, so look back one more byte for `f`.
		isFString := false
		if m[litStart] >= 2 {
			// content[m[litStart]-1] is the opening quote; content[m[litStart]-2] would be `f` if f-string.
			if content[m[litStart]-1] == '"' || content[m[litStart]-1] == '\'' {
				if content[m[litStart]-2] == 'f' || content[m[litStart]-2] == 'F' {
					isFString = true
				}
			}
		}
		return raw, isFString, false
	}
	// Bare-identifier group is at litStart+2 / litStart+3.
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		ident := content[m[litStart+2]:m[litStart+3]]
		if val, ok := syms[ident]; ok {
			return val, false, false
		}
		// Unknown identifier — could be an env-var ref or any runtime
		// expression. We don't have a URL to canonicalise, so skip the
		// emission (no FETCHES target makes sense). Return empty.
		return "", false, true
	}
	return "", false, false
}

// pyCanonicalize converts a raw URL fragment to a canonical path. The
// caller has already split on literal vs f-string vs bare-identifier.
func pyCanonicalize(raw string, isFString bool, syms map[string]string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	// #807 — Apply env-var prefix normalization BEFORE f-string substitution.
	// Handles patterns like os.environ["API_URL"] + "/users" where the
	// raw argument has an env-var prefix rather than a pure path.
	normed := normalizePath(raw)
	raw = normed.Path

	if isFString {
		// Substitute {ident} with constant values from syms when known.
		// For the FIRST substitution only: if the identifier is not in syms
		// but its name looks like a URL/host constant (all-caps or conventional
		// _URL/_HOST/_ADDR suffix), strip it entirely so that
		// `f"{PRICING_URL}/quote"` → `/quote` rather than `/{PRICING_URL}/quote`
		// when PRICING_URL is imported from another module. Subsequent
		// substitutions (path params like {item_id}) keep their {name} form.
		firstSub := true
		replaced := pyFStringSubstRe.ReplaceAllStringFunc(raw, func(match string) string {
			mm := pyFStringSubstRe.FindStringSubmatch(match)
			if len(mm) < 2 {
				firstSub = false
				return "{param}"
			}
			expr := strings.TrimSpace(mm[1])
			isFirst := firstSub
			firstSub = false
			// Constant fold simple identifiers — works for both same-file
			// constants and any imported constant that was captured in mergedSyms.
			if pyIdentRe.MatchString(expr) {
				if val, ok := syms[expr]; ok {
					return val
				}
				// Not in syms. If this is the first (prefix) substitution and
				// the name looks like a URL/host constant, strip it so the
				// remaining path is still useful. This handles the cross-module
				// import case: `from config import PRICING_URL` where the value
				// is only known at the config module level.
				if isFirst && pyIsURLConstName(expr) {
					return ""
				}
				return "{" + expr + "}"
			}
			// Dotted: take the last segment for placeholder name.
			if dot := strings.LastIndexByte(expr, '.'); dot >= 0 {
				last := expr[dot+1:]
				if pyIdentRe.MatchString(last) {
					return "{" + last + "}"
				}
			}
			return "{param}"
		})
		raw = replaced
	}
	raw = stripURLHost(raw)
	if !looksLikeURLPathOrParam(raw) {
		return "", false
	}
	return raw, true
}
