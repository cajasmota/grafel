// C# consumer-side HTTP client synthesis (#721 wave 2b).
//
// Mirrors http_endpoint_go_client.go / http_endpoint_kotlin_client.go for
// C# consumer-side HTTP patterns. Emits one synthetic `http_endpoint` entity
// (consumer side) per detected client call site, AND a FETCHES edge from
// the enclosing method to that endpoint.
//
// Patterns covered:
//
//   - HttpClient:
//     await client.GetAsync(url), await client.PostAsync(url, content)
//     await client.PutAsync(url, content), await client.DeleteAsync(url)
//     await client.PatchAsync(url, content), await client.SendAsync(request)
//     await client.GetStringAsync(url), await client.GetStreamAsync(url)
//     new HttpRequestMessage(HttpMethod.Get, url) — verb inferred from method arg
//
//   - RestSharp:
//     var client = new RestClient(url); var request = new RestRequest(path, Method.Get)
//     client.ExecuteAsync(request), client.GetAsync(request), client.PostAsync(request)
//
//   - Refit (interface annotations):
//     [Get("/users")], [Post("/users")], [Put("/users/{id}")],
//     [Delete("/users/{id}")], [Patch("/users/{id}")], [Head("/users")],
//     [Options("/users")] — on interface methods
//
//   - WebClient (legacy):
//     wc.DownloadString(url), wc.DownloadStringAsync(url)
//     wc.UploadString(url, data), wc.UploadData(url, data)
//
// Beyond-minimum behaviours:
//   - Async/await variants for all HttpClient methods
//   - Full Refit annotation suite: [Get], [Post], [Put], [Delete], [Patch], [Head]
//   - WebClient deprecated patterns
//   - Env-var concatenation: await client.GetAsync(Environment.GetEnvironmentVariable("API_URL") + "/users")
//     → emit with runtime_dynamic=true
//
// The enclosing method is identified by scanning for the nearest preceding
// `... <ReturnType> <Name>(` or `async Task ... <Name>(` declaration.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// HttpClient async verb methods
// ---------------------------------------------------------------------------

// csHttpClientVerbRe matches `await client.GetAsync(url)`, `await client.PostAsync(url, content)`.
// Also matches non-awaited forms like `client.GetAsync(url)` (fire-and-forget or .Result).
// Receiver allow-list: client, httpClient, _client, _httpClient, http, hc, httpClient.
var csHttpClientVerbRe = regexp.MustCompile(
	`\b(_?(?:client|httpClient|_client|http|hc|myClient))\s*\.\s*(GetAsync|PostAsync|PutAsync|DeleteAsync|PatchAsync|HeadAsync|OptionsAsync|GetStringAsync|GetStreamAsync|GetByteArrayAsync)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted url
		`|` +
		`@"([^"\n\r]+)"` + // group 4: verbatim string literal
		`|` +
		`([A-Za-z_][\w]*)` + // group 5: bare identifier
		`)`,
)

// csSendAsyncRe matches `await client.SendAsync(request)` where the request
// is an HttpRequestMessage. We look for a preceding
// `new HttpRequestMessage(HttpMethod.Get, url)` in a 512-byte window.
var csSendAsyncRe = regexp.MustCompile(
	`\b(_?(?:client|httpClient|_client|http|hc|myClient))\s*\.\s*SendAsync\s*\(`,
)

// csHttpRequestMessageRe captures `new HttpRequestMessage(HttpMethod.Get, url)`.
// Capture groups: 1 = method name (Get/Post/Put/Delete/Patch/Head/Options),
// 2 = double-quoted url, 3 = verbatim string url, 4 = identifier url.
var csHttpRequestMessageRe = regexp.MustCompile(
	`new\s+HttpRequestMessage\s*\(\s*HttpMethod\s*\.\s*([A-Za-z]+)\s*,\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`@"([^"\n\r]+)"` + // group 3: verbatim string url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// ---------------------------------------------------------------------------
// RestSharp
// ---------------------------------------------------------------------------

// csRestClientNewRe matches `new RestClient(url)` or `new RestClient("url")`.
// We use this to detect that RestSharp is in use in the file.
var csRestClientNewRe = regexp.MustCompile(
	`new\s+RestClient\s*\(\s*(?:"([^"\n\r]+)"|([A-Za-z_][\w]*))`,
)

// csRestRequestNewRe matches `new RestRequest(path, Method.Get)` or
// `new RestRequest("/path")`. Capture groups:
// 1 = double-quoted path, 2 = identifier path, 3 = Method enum value (optional).
var csRestRequestNewRe = regexp.MustCompile(
	`new\s+RestRequest\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 1: double-quoted path
		`|` +
		`([A-Za-z_][\w]*)` + // group 2: identifier path
		`)\s*(?:,\s*Method\s*\.\s*([A-Za-z]+))?`,
)

// csRestClientExecuteRe matches `client.ExecuteAsync(request)` and
// `client.GetAsync(request)` / `client.PostAsync(request)` in RestSharp context.
// We use this as a signal that a RestRequest is being executed.
var csRestClientExecuteRe = regexp.MustCompile(
	`\b\w+\s*\.\s*(ExecuteAsync|Execute|GetAsync|PostAsync|PutAsync|DeleteAsync|PatchAsync)\s*\(\s*\w+`,
)

// ---------------------------------------------------------------------------
// Refit interface annotations
// ---------------------------------------------------------------------------

// csRefitAnnotationRe captures Refit verb annotations on interface methods:
// [Get("/path")], [Post("/path")], [Put("/path/{id}")], etc.
// Both single and double brackets are valid in C# (single is standard).
// Capture groups: 1 = verb (Get/Post/Put/Delete/Patch/Head/Options),
// 2 = path string.
var csRefitAnnotationRe = regexp.MustCompile(
	`\[\s*(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*"([^"\n\r]+)"\s*\)\s*\]`,
)

// ---------------------------------------------------------------------------
// WebClient (legacy)
// ---------------------------------------------------------------------------

// csWebClientDownloadRe matches `wc.DownloadString(url)`, `wc.DownloadStringAsync(url)`,
// `wc.DownloadData(url)`, `new WebClient().DownloadString(url)`.
// Receiver allow-list: wc, webClient, _wc, client (when paired with WebClient).
var csWebClientDownloadRe = regexp.MustCompile(
	`\b(_?(?:wc|webClient|client|webClnt))\s*\.\s*(DownloadString(?:Async)?|DownloadData(?:Async)?|DownloadFile(?:Async)?)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// csWebClientUploadRe matches `wc.UploadString(url, data)`, `wc.UploadData(url, data)`.
// These are POST-equivalent operations.
var csWebClientUploadRe = regexp.MustCompile(
	`\b(_?(?:wc|webClient|client|webClnt))\s*\.\s*(UploadString(?:Async)?|UploadData(?:Async)?|UploadFile(?:Async)?)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// csEnvGetenvFrag is the fragment matching C# environment variable access:
// `Environment.GetEnvironmentVariable("NAME")`.
const csEnvGetenvFrag = `Environment\s*\.\s*GetEnvironmentVariable\s*\([^)]+\)`

// csHttpClientEnvVerbRe matches
// `client.GetAsync(Environment.GetEnvironmentVariable("X") + "/path")`.
// Capture groups: 1 = receiver, 2 = verb method name, 3 = path suffix.
var csHttpClientEnvVerbRe = regexp.MustCompile(
	`\b(_?(?:client|httpClient|_client|http|hc|myClient))\s*\.\s*(GetAsync|PostAsync|PutAsync|DeleteAsync|PatchAsync|HeadAsync|OptionsAsync)\s*\(\s*` +
		csEnvGetenvFrag + `\s*\+\s*"([^"\n\r]*)"`,
)

// ---------------------------------------------------------------------------
// String constant table
// ---------------------------------------------------------------------------

// csStringConstRe captures simple C# string constant declarations:
//
//	const string NAME = "/value";
//	private const string NAME = "/value";
//	var name = "/value";
//	string name = "/value";
var csStringConstRe = regexp.MustCompile(
	`(?:const\s+string|private\s+const\s+string|string|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"\n\r]{1,256})"`,
)

// ---------------------------------------------------------------------------
// Enclosing method index
// ---------------------------------------------------------------------------

// csEnclosingMethodRe captures C# method declarations:
//
//	public async Task<T> MethodName(
//	private void MethodName(
//	public static string MethodName(
//	protected override Task MethodName(
var csEnclosingMethodRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:public|private|protected|internal|static|virtual|override|abstract|async|\s)+\s+)[\w<>\[\],?\s]+\s+([A-Za-z_]\w*)\s*\(`,
)

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// csClientEmitFn is the runtime-aware emitter type for C# clients.
type csClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeCSharpClient is the package-level entry point referenced from
// applyHTTPEndpointSynthesis.
func synthesizeCSharpClient(content string, emit emitFn) {
	synthesizeCSharpClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizeCSharpClientWithRuntime runs the full C# client scan.
func synthesizeCSharpClientWithRuntime(content string, emit csClientEmitFn) {
	if !csHasAnyHTTPClient(content) {
		return
	}
	funcs := indexCsEnclosingMethods(content)
	syms := buildCsStringSymbolTable(content)

	// ----- HttpClient async verbs: client.GetAsync/PostAsync/... -----
	for _, m := range csHttpClientVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verbMethod := content[m[4]:m[5]]
		verb := csVerbFromMethodName(verbMethod)
		raw := csPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingCsMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "httpclient", "Function", caller, false)
	}

	// ----- HttpClient.SendAsync with HttpRequestMessage(method, url) -----
	for _, m := range csHttpRequestMessageRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := csPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingCsMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "httpclient", "Function", caller, false)
	}

	// ----- RestSharp: new RestRequest(path, Method.Verb) -----
	for _, m := range csRestRequestNewRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		// URL is group 1 (double-quoted) or group 2 (identifier).
		raw := ""
		if m[2] >= 0 {
			raw = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			ident := content[m[4]:m[5]]
			if val, ok := syms[ident]; ok {
				raw = val
			}
		}
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		// Verb: from Method enum group 3, defaulting to GET.
		verb := "GET"
		if m[6] >= 0 {
			verb = strings.ToUpper(content[m[6]:m[7]])
		}
		caller := enclosingCsMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "restsharp", "Function", caller, false)
	}

	// ----- Refit interface annotations: [Get("/path")], [Post("/path")] -----
	for _, m := range csRefitAnnotationRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := csNextInterfaceMethod(content, m[1])
		if caller == "" {
			caller = enclosingCsMethodAt(funcs, m[0])
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "refit", "Function", caller, false)
	}

	// ----- WebClient.DownloadString/DownloadData (legacy GET-equivalent) -----
	for _, m := range csWebClientDownloadRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		raw := csPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingCsMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit("GET", canonical, "webclient", "Function", caller, false)
	}

	// ----- WebClient.UploadString/UploadData (legacy POST-equivalent) -----
	for _, m := range csWebClientUploadRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		raw := csPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingCsMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit("POST", canonical, "webclient", "Function", caller, false)
	}

	// ----- Env-var concat: client.GetAsync(Environment.GetEnvironmentVariable("X") + "/path") -----
	for _, m := range csHttpClientEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verbMethod := content[m[4]:m[5]]
		verb := csVerbFromMethodName(verbMethod)
		suffix := ""
		if m[6] >= 0 {
			suffix = content[m[6]:m[7]]
		}
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingCsMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "httpclient", "Function", caller, true)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func csHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "HttpClient") ||
		strings.Contains(content, "HttpRequestMessage") ||
		strings.Contains(content, "RestClient") ||
		strings.Contains(content, "RestRequest") ||
		strings.Contains(content, "[Get(") || strings.Contains(content, "[Post(") ||
		strings.Contains(content, "[Put(") || strings.Contains(content, "[Delete(") ||
		strings.Contains(content, "[Patch(") || strings.Contains(content, "[Head(") ||
		strings.Contains(content, "[Options(") ||
		strings.Contains(content, "WebClient") ||
		strings.Contains(content, "GetAsync") || strings.Contains(content, "PostAsync") ||
		strings.Contains(content, "client.GetAsync") || strings.Contains(content, "client.PostAsync") ||
		strings.Contains(content, "Environment.GetEnvironmentVariable")
}

// buildCsStringSymbolTable returns a map from identifier → string value
// for simple constant/var string declarations in the C# file.
func buildCsStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range csStringConstRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		if _, dup := syms[m[1]]; !dup {
			syms[m[1]] = m[2]
		}
	}
	return syms
}

// csPickURLArg extracts the URL string from a match's double-quoted /
// verbatim string / identifier group triple. `litStart` is the index
// within `m` of the first literal group.
func csPickURLArg(content string, m []int, litStart int, syms map[string]string) string {
	// Double-quoted literal.
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	// Verbatim string @"..." (treated same as double-quoted for path extraction).
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

// csVerbFromMethodName maps an HttpClient async method name to its HTTP verb.
// e.g. GetAsync → GET, PostAsync → POST, GetStringAsync → GET.
func csVerbFromMethodName(methodName string) string {
	switch {
	case strings.HasPrefix(methodName, "Get"):
		return "GET"
	case strings.HasPrefix(methodName, "Post"):
		return "POST"
	case strings.HasPrefix(methodName, "Put"):
		return "PUT"
	case strings.HasPrefix(methodName, "Delete"):
		return "DELETE"
	case strings.HasPrefix(methodName, "Patch"):
		return "PATCH"
	case strings.HasPrefix(methodName, "Head"):
		return "HEAD"
	case strings.HasPrefix(methodName, "Options"):
		return "OPTIONS"
	default:
		return "GET"
	}
}

// csInterfaceMethodHeadRe captures C# interface method declarations
// following a Refit annotation. Handles both async Task<T> and return-type
// forms:
//
//	Task<List<User>> GetUsers();
//	Task CreateUser([Body] User user);
//	IObservable<User> GetUserById([AliasAs("id")] int id);
var csInterfaceMethodHeadRe = regexp.MustCompile(
	`(?:Task|IObservable|ValueTask)(?:<[^>]*>)?\s+([A-Za-z_]\w*)\s*\(`,
)

// csNextInterfaceMethod returns the method name on the line immediately
// following a Refit annotation match.
func csNextInterfaceMethod(content string, pos int) string {
	end := pos + 512
	if end > len(content) {
		end = len(content)
	}
	window := content[pos:end]
	mm := csInterfaceMethodHeadRe.FindStringSubmatch(window)
	if len(mm) < 2 {
		return ""
	}
	return mm[1]
}

// indexCsEnclosingMethods builds a sorted (offset, name) list for every
// C# method declaration in the file.
func indexCsEnclosingMethods(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range csEnclosingMethodRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingCsMethodAt returns the name of the nearest preceding method
// declaration for a call site at `pos`.
func enclosingCsMethodAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
