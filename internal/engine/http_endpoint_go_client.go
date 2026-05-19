// Go consumer-side HTTP client synthesis (#721 wave 2a).
//
// Mirrors http_endpoint_python_client.go / http_endpoint_java_client.go for
// Go consumer-side HTTP patterns. Emits one synthetic `http_endpoint` entity
// (consumer side) per detected client call site, AND a FETCHES edge from the
// enclosing function to that endpoint.
//
// Patterns covered:
//
//   - net/http standard library:
//     http.Get(url), http.Post(url, ...)
//     http.Head(url), http.PostForm(url, ...)
//     http.NewRequest(method, url, body)
//     client.Get(url), client.Post(url, ...), client.Head(url)
//     client.Do(req) with a preceding http.NewRequest (verb inferred from
//     the nearest NewRequest in a 512-byte backward window)
//
//   - resty (github.com/go-resty/resty):
//     resty.New().R().Get(url), .Post(url), .Put(url), .Patch(url),
//     .Delete(url), .Head(url), .Options(url)
//
//   - fasthttp (github.com/valyala/fasthttp):
//     fasthttp.Get(dst, url), fasthttp.Post(dst, url, ...)
//     client.Do(req) with req.SetRequestURI(url) (verb inferred from
//     req.Header.SetMethod("VERB") in the same block)
//
// Beyond-minimum behaviours:
//   - http.Client instance method calls:
//     c.Get(url), c.Post(url, ...), c.Head(url), c.Delete(url)  etc.
//     where the receiver is a typical HTTP client variable name
//     (client/c/httpClient/hc/httpC/cl)
//   - Env-var concatenation: http.Get(os.Getenv("API_URL") + "/users")
//     → emit with runtime_dynamic=true
//   - fmt.Sprintf URL composition:
//     fmt.Sprintf("%s/users", base) → /users (static suffix extracted)
//
// The enclosing function is identified by scanning for the nearest preceding
// `func <name>(` declaration.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// net/http package-level verbs
// ---------------------------------------------------------------------------

// goHTTPPkgVerbRe matches `http.Get(url)`, `http.Post(url, ...)`,
// `http.Head(url)`, `http.PostForm(url, ...)`.
// The url group accepts:
//   - String literal:  "..." or `...`
//   - Bare identifier: PATH
var goHTTPPkgVerbRe = regexp.MustCompile(
	`\bhttp\s*\.\s*(Get|Post|Head|PostForm)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted literal
		`|` +
		"`([^`\n\r]+)`" + // group 3: backtick literal
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare identifier
		`)`,
)

// goHTTPNewRequestRe matches `http.NewRequest(method, url, body)`.
// Capture groups:
//
//	1 = method literal (e.g. "GET") — may be a string literal or identifier
//	2 = url double-quoted literal
//	3 = url backtick literal
//	4 = url bare identifier
//	5 = method identifier (when method is not a string literal)
var goHTTPNewRequestRe = regexp.MustCompile(
	`\bhttp\s*\.\s*NewRequest(?:WithContext)?\s*\(\s*(?:` +
		`"([A-Za-z]+)"` + // group 1: method as string literal
		`|` +
		`([A-Za-z_][\w]*)` + // group 2: method as identifier (e.g. http.MethodGet)
		`)\s*,\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: url double-quoted
		`|` +
		"`([^`\n\r]+)`" + // group 4: url backtick
		`|` +
		`([A-Za-z_][\w]*)` + // group 5: url identifier
		`)`,
)

// ---------------------------------------------------------------------------
// net/http client instance verbs
// ---------------------------------------------------------------------------

// goHTTPClientVerbRe matches `<client>.<verb>(url, ...)` where client is a
// typical net/http client variable name.
// Receiver allow-list: client, c, httpClient, hc, httpC, cl, httpc, myClient.
var goHTTPClientVerbRe = regexp.MustCompile(
	`\b(client|c|httpClient|hc|httpC|cl|httpc|myClient)\s*\.\s*(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted url
		`|` +
		"`([^`\n\r]+)`" + // group 4: backtick url
		`|` +
		`([A-Za-z_][\w]*)` + // group 5: identifier url
		`)`,
)

// ---------------------------------------------------------------------------
// resty
// ---------------------------------------------------------------------------

// goRestyVerbRe matches resty chain calls ending in a verb:
// `resty.New().R().<verb>(url)` or just `<restyClient>.R().<verb>(url)`
// or even the short form `client.R().<verb>(url)`.
// We match any `.R().<verb>(url)` suffix since the leading receiver doesn't
// matter — only the `.R()` prefix identifies the resty request object.
var goRestyVerbRe = regexp.MustCompile(
	`\.R\s*\(\s*\)\s*\.\s*(Get|Post|Put|Patch|Delete|Head|Options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		"`([^`\n\r]+)`" + // group 3: backtick url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// ---------------------------------------------------------------------------
// fasthttp
// ---------------------------------------------------------------------------

// goFasthttpPkgVerbRe matches `fasthttp.Get(dst, url)` and
// `fasthttp.Post(dst, url, ...)` at the package level.
// The dst argument is always the first arg; url is the second.
// We skip the dst argument and capture the url arg.
var goFasthttpPkgVerbRe = regexp.MustCompile(
	`\bfasthttp\s*\.\s*(Get|Post)\s*\(\s*[^,]+,\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		"`([^`\n\r]+)`" + // group 3: backtick url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// goFasthttpSetRequestURIRe matches `req.SetRequestURI(url)` which is
// the canonical way to set the URL on a fasthttp request object.
// We use this to detect fasthttp client.Do(req) calls — pair the URL
// from SetRequestURI with the verb from SetMethod.
var goFasthttpSetRequestURIRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*SetRequestURI\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		"`([^`\n\r]+)`" + // group 3: backtick url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// goFasthttpSetMethodRe matches `req.Header.SetMethod("VERB")` in a fasthttp
// block. Used alongside goFasthttpSetRequestURIRe to infer the verb.
var goFasthttpSetMethodRe = regexp.MustCompile(
	`\w+\s*\.\s*Header\s*\.\s*SetMethod\s*\(\s*"([A-Za-z]+)"`,
)

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// goEnvConcatRe matches `os.Getenv("X") + "/path"` and
// `os.LookupEnv("X")` (first return value) + "/path" (we only handle
// os.Getenv for simplicity) as the prefix of an HTTP URL argument.
// Used to detect runtime-dynamic URLs in http.Get / http.Post / http.NewRequest.
//
// Capture groups:
//
//	1 = path suffix (the string being concatenated after the env var)
var goEnvConcatRe = regexp.MustCompile(
	`os\.Getenv\s*\([^)]+\)\s*\+\s*"([^"\n\r]*)"`,
)

// goHTTPPkgEnvVerbRe matches http.<Verb>(os.Getenv("X") + "/path", ...).
//
// Capture groups:
//
//	1 = verb
//	2 = path suffix
var goHTTPPkgEnvVerbRe = regexp.MustCompile(
	`\bhttp\s*\.\s*(Get|Post|Head|PostForm)\s*\(\s*os\.Getenv\s*\([^)]+\)\s*\+\s*"([^"\n\r]*)"`,
)

// goHTTPClientEnvVerbRe matches `<client>.<Verb>(os.Getenv("X") + "/path")`.
//
// Capture groups:
//
//	1 = receiver
//	2 = verb
//	3 = path suffix
var goHTTPClientEnvVerbRe = regexp.MustCompile(
	`\b(client|c|httpClient|hc|httpC|cl|httpc|myClient)\s*\.\s*(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*os\.Getenv\s*\([^)]+\)\s*\+\s*"([^"\n\r]*)"`,
)

// goRestyEnvVerbRe matches `.R().<Verb>(os.Getenv("X") + "/path")`.
//
// Capture groups:
//
//	1 = verb
//	2 = path suffix
var goRestyEnvVerbRe = regexp.MustCompile(
	`\.R\s*\(\s*\)\s*\.\s*(Get|Post|Put|Patch|Delete|Head|Options)\s*\(\s*os\.Getenv\s*\([^)]+\)\s*\+\s*"([^"\n\r]*)"`,
)

// ---------------------------------------------------------------------------
// fmt.Sprintf URL composition
// ---------------------------------------------------------------------------

// goFmtSprintfHTTPRe matches `http.Get(fmt.Sprintf("%s/path", base), ...)`.
// We extract the static `/path` suffix from the format string.
// The regex requires a `%s` placeholder followed by a `/`-prefixed path segment
// so that we only capture the static URL suffix, not the `%s` itself.
// Capture groups:
//
//	1 = verb
//	2 = static path suffix (starts with /)
var goFmtSprintfHTTPRe = regexp.MustCompile(
	`\bhttp\s*\.\s*(Get|Post|Head|PostForm)\s*\(\s*fmt\.Sprintf\s*\(\s*"[^"]*?%s(\/[A-Za-z0-9_/{}.-]*)?"`,
)

// ---------------------------------------------------------------------------
// String constant table
// ---------------------------------------------------------------------------

// goStringConstRe captures simple Go constant/var declarations:
//
//	const NAME = "/value"
//	var NAME = "/value"
//	NAME := "/value"
var goStringConstRe = regexp.MustCompile(
	`(?m)(?:const|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:string\s*)?=\s*"([^"\n\r]{1,256})"` +
		`|([A-Za-z_][A-Za-z0-9_]*)\s*:=\s*"([^"\n\r]{1,256})"`,
)

// ---------------------------------------------------------------------------
// Enclosing function index
// ---------------------------------------------------------------------------

// goEnclosingFuncRe captures Go function and method declarations:
//
//	func foo(...) {...}
//	func (r *Receiver) foo(...) {...}
var goEnclosingFuncRe = regexp.MustCompile(
	`(?m)^func\s+(?:\([^)]+\)\s+)?([A-Za-z_]\w*)\s*\(`,
)

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// goClientEmitFn is the runtime-aware emitter type for Go clients.
type goClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeGoClient is the package-level entry point referenced from
// applyHTTPEndpointSynthesis. Adapts the standard emitFn to goClientEmitFn.
func synthesizeGoClient(content string, emit emitFn) {
	synthesizeGoClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizeGoClientWithRuntime runs the full per-framework Go client scan.
func synthesizeGoClientWithRuntime(content string, emit goClientEmitFn) {
	if !goHasAnyHTTPClient(content) {
		return
	}
	funcs := indexGoEnclosingFunctions(content)
	syms := buildGoStringSymbolTable(content)

	// ----- net/http package-level verbs: http.Get/Post/Head/PostForm -----
	for _, m := range goHTTPPkgVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		// PostForm is always POST.
		if verb == "POSTFORM" {
			verb = "POST"
		}
		raw := goPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, path)
		emit(verb, canonical, "net_http", "Function", caller, false)
	}

	// ----- net/http NewRequest / NewRequestWithContext -----
	for _, m := range goHTTPNewRequestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		// Pick verb: string literal (group 1) takes priority over identifier (group 2).
		verb := "GET"
		if m[2] >= 0 {
			verb = strings.ToUpper(content[m[2]:m[3]])
		} else if m[4] >= 0 {
			// e.g. http.MethodPost → "POST" (strip leading "http.Method" prefix)
			raw := content[m[4]:m[5]]
			verb = goParseHTTPMethodConst(raw)
		}
		// Pick URL: groups 5, 7, 9 for double-quoted, backtick, identifier.
		raw := goPickURLArgAt(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, path)
		emit(verb, canonical, "net_http", "Function", caller, false)
	}

	// ----- net/http client instance: client.Get/Post/... -----
	for _, m := range goHTTPClientVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := goPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, path)
		emit(verb, canonical, "net_http", "Function", caller, false)
	}

	// ----- resty: .R().<Verb>(url) -----
	for _, m := range goRestyVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := goPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, path)
		emit(verb, canonical, "resty", "Function", caller, false)
	}

	// ----- fasthttp package-level: fasthttp.Get / fasthttp.Post -----
	for _, m := range goFasthttpPkgVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := goPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, path)
		emit(verb, canonical, "fasthttp", "Function", caller, false)
	}

	// ----- fasthttp client.Do with req.SetRequestURI -----
	for _, m := range goFasthttpSetRequestURIRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		raw := goPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		// Resolve verb from nearby SetMethod call in a 256-byte window.
		verb := goResolveFasthttpVerb(content, m[0])
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, path)
		emit(verb, canonical, "fasthttp", "Function", caller, false)
	}

	// ----- fmt.Sprintf URL composition: http.Get(fmt.Sprintf("%s/path", base)) -----
	for _, m := range goFmtSprintfHTTPRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		if verb == "POSTFORM" {
			verb = "POST"
		}
		// Group 2 is optional (the static suffix after %s). Skip if absent.
		if m[4] < 0 {
			continue
		}
		suffix := content[m[4]:m[5]]
		if suffix == "" {
			continue
		}
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, suffix)
		emit(verb, canonical, "net_http", "Function", caller, false)
	}

	// ----- Env-var concatenation: http.Get(os.Getenv("X") + "/path") -----
	for _, m := range goHTTPPkgEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		if verb == "POSTFORM" {
			verb = "POST"
		}
		suffix := content[m[4]:m[5]]
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, suffix)
		emit(verb, canonical, "net_http", "Function", caller, true)
	}

	// ----- client.Get(os.Getenv("X") + "/path") -----
	for _, m := range goHTTPClientEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		suffix := content[m[6]:m[7]]
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, suffix)
		emit(verb, canonical, "net_http", "Function", caller, true)
	}

	// ----- resty .R().<Verb>(os.Getenv("X") + "/path") -----
	for _, m := range goRestyEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := content[m[4]:m[5]]
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingGoFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkGin, suffix)
		emit(verb, canonical, "resty", "Function", caller, true)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func goHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "http.Get") ||
		strings.Contains(content, "http.Post") ||
		strings.Contains(content, "http.Head") ||
		strings.Contains(content, "http.NewRequest") ||
		strings.Contains(content, "resty.") ||
		strings.Contains(content, ".R().") ||
		strings.Contains(content, "fasthttp.") ||
		strings.Contains(content, "SetRequestURI") ||
		strings.Contains(content, "os.Getenv") ||
		strings.Contains(content, "fmt.Sprintf") ||
		strings.Contains(content, "client.Get") ||
		strings.Contains(content, "client.Post") ||
		strings.Contains(content, "hc.Get") ||
		strings.Contains(content, "hc.Post")
}

// buildGoStringSymbolTable returns a map from identifier → string value
// for simple const/var/short-assignment declarations in the file.
func buildGoStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range goStringConstRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		var name, val string
		if m[2] >= 0 && m[4] >= 0 {
			name = content[m[2]:m[3]]
			val = content[m[4]:m[5]]
		} else if m[6] >= 0 && m[8] >= 0 {
			name = content[m[6]:m[7]]
			val = content[m[8]:m[9]]
		}
		if name != "" {
			if _, dup := syms[name]; !dup {
				syms[name] = val
			}
		}
	}
	return syms
}

// goPickURLArg extracts the URL string from a match's literal/backtick/
// identifier group triple. `litStart` is the index within `m` of the
// first literal group; litStart+2 is backtick; litStart+4 is identifier.
func goPickURLArg(content string, m []int, litStart int, syms map[string]string) string {
	// Double-quoted literal.
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	// Backtick literal.
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		return content[m[litStart+2]:m[litStart+3]]
	}
	// Bare identifier — resolve via symbol table.
	if litStart+5 < len(m) && m[litStart+4] >= 0 {
		ident := content[m[litStart+4]:m[litStart+5]]
		if val, ok := syms[ident]; ok {
			return val
		}
	}
	return ""
}

// goPickURLArgAt is identical to goPickURLArg but uses `litStart` directly
// without the 2-index skip (used by NewRequest where the method groups come
// first and the url groups are numbered differently).
func goPickURLArgAt(content string, m []int, litStart int, syms map[string]string) string {
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		return content[m[litStart+2]:m[litStart+3]]
	}
	if litStart+5 < len(m) && m[litStart+4] >= 0 {
		ident := content[m[litStart+4]:m[litStart+5]]
		if val, ok := syms[ident]; ok {
			return val
		}
	}
	return ""
}

// goParseHTTPMethodConst converts `http.MethodPost` → "POST" etc.
// If the identifier doesn't start with `http.Method` we return it
// upper-cased as-is (it might be a local variable containing the method string).
func goParseHTTPMethodConst(s string) string {
	const prefix = "http.Method"
	if strings.HasPrefix(s, prefix) {
		return strings.ToUpper(s[len(prefix):])
	}
	return strings.ToUpper(s)
}

// goResolveFasthttpVerb searches backward up to 512 bytes from `pos`
// for a `req.Header.SetMethod("VERB")` call in the same block.
// Returns "GET" if none is found.
func goResolveFasthttpVerb(content string, pos int) string {
	start := pos - 512
	if start < 0 {
		start = 0
	}
	window := content[start:pos]
	if mm := goFasthttpSetMethodRe.FindStringSubmatch(window); len(mm) >= 2 {
		return strings.ToUpper(mm[1])
	}
	return "GET"
}

// indexGoEnclosingFunctions builds a sorted (offset, name) list for every
// Go function/method declaration in the file.
func indexGoEnclosingFunctions(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range goEnclosingFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingGoFuncAt returns the name of the nearest preceding function
// declaration for a call site at `pos`.
func enclosingGoFuncAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
