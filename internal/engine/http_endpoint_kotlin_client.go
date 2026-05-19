// Kotlin consumer-side HTTP client synthesis (#721 wave 2a).
//
// Mirrors http_endpoint_python_client.go / http_endpoint_java_client.go for
// Kotlin consumer-side HTTP patterns. Emits one synthetic `http_endpoint`
// entity (consumer side) per detected client call site, AND a FETCHES edge
// from the enclosing function to that endpoint.
//
// Patterns covered:
//
//   - Ktor HttpClient:
//     client.get("/users"), client.post("/users") { ... }
//     client.put("/users/{id}"), client.delete("/users/{id}")
//     HttpClient().get("/users") — inline construction + call
//     httpClient.request { url(...) } — request builder form
//     httpClient.use { it.get("/users") } — coroutine lambda (beyond-minimum)
//
//   - OkHttp (Kotlin usage):
//     OkHttpClient().newCall(Request.Builder().url("...").build()).execute()
//     Request.Builder().url("...").method("POST", body).build()
//
//   - Retrofit (Kotlin interface annotations):
//     @GET("/api/users") on suspend fun / fun interface method
//     @POST("/api/users"), @PUT, @DELETE, @PATCH, @HEAD, @OPTIONS
//     Retrofit.Builder().baseUrl("...") composition with @-annotations
//
// Beyond-minimum behaviours:
//   - Ktor coroutine lambda: httpClient.use { it.get("/users") } →
//     FETCHES from the enclosing function, framework=ktor
//   - Env-var concatenation: client.get(System.getenv("API_URL") + "/users")
//     → runtime_dynamic=true
//   - Retrofit baseUrl composition with annotation paths
//
// The enclosing function is identified by scanning for the nearest preceding
// `fun <name>(` declaration (handles both regular and suspend functions).
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Ktor HttpClient instance method calls
// ---------------------------------------------------------------------------

// ktKtorClientVerbRe matches `<ident>.get("/path")`, `.post("/path") { }`,
// `HttpClient().get("/path")` and similar direct-verb call forms.
// Receiver allow-list: httpClient, client, ktorClient, http, api, apiClient.
// Also matches `HttpClient().<verb>` (inline construction).
var ktKtorClientVerbRe = regexp.MustCompile(
	`\b(?:httpClient|client|ktorClient|http|api|apiClient|HttpClient\s*\(\s*\))\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier
		`)`,
)

// ktKtorRequestBuilderRe matches the builder form:
// `httpClient.request { url("https://...") }` or
// `httpClient.request { url = "..." }`.
// We capture the URL from inside the lambda.
var ktKtorRequestBuilderRe = regexp.MustCompile(
	`\b(?:httpClient|client|ktorClient|http)\s*\.\s*request\s*\{[^}]*(?:url\s*\(\s*"([^"\n\r]+)"\s*\)|url\s*=\s*"([^"\n\r]+)")`,
)

// ktKtorUseLambdaRe matches the coroutine `httpClient.use { it.get("/path") }`
// and `httpClient.use { client -> client.post("/path") { ... } }` patterns.
// Capture groups:
//
//	1 = verb
//	2 = double-quoted url
//	3 = identifier url
var ktKtorUseLambdaRe = regexp.MustCompile(
	`\b(?:httpClient|client|ktorClient)\s*\.\s*use\s*\{[^}]*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted
		`|` +
		`([A-Za-z_][\w]*)` + // group 3: identifier
		`)`,
)

// ---------------------------------------------------------------------------
// OkHttp (Kotlin)
// ---------------------------------------------------------------------------

// ktOkHttpRequestBuilderURLRe captures `Request.Builder().url("...")`.
// Mirrors the Java OkHttp pattern — the URL is set on the builder.
var ktOkHttpRequestBuilderURLRe = regexp.MustCompile(
	`Request\.Builder\s*\(\s*\)\s*\.\s*url\s*\(\s*(?:"([^"\n\r]+)"|([A-Za-z_][\w]*))`,
)

// ktOkHttpVerbBuilderRe matches the verb terminator on the OkHttp
// Request.Builder chain: `.get()` / `.post(body)` / `.method("VERB", body)`.
var ktOkHttpVerbBuilderRe = regexp.MustCompile(
	`\.\s*(?:(get|post|put|delete|head|patch)\s*\(` +
		`|method\s*\(\s*"([A-Za-z]+)"\s*,)`,
)

// ---------------------------------------------------------------------------
// Retrofit (Kotlin)
// ---------------------------------------------------------------------------

// ktRetrofitAnnotationRe captures Retrofit verb annotations on interface
// methods: @GET("/path"), @POST("/path"), etc.
// This covers both Java-style and Kotlin-style annotations.
var ktRetrofitAnnotationRe = regexp.MustCompile(
	`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"\n\r]+)"\s*\)`,
)

// ktRetrofitBaseURLRe captures `Retrofit.Builder().baseUrl("...")` for
// base URL composition.
var ktRetrofitBaseURLRe = regexp.MustCompile(
	`(?s)\bRetrofit\s*\.\s*Builder\s*\(\s*\)[^;]*?\.\s*baseUrl\s*\(\s*"([^"\n\r]+)"`,
)

// ktInterfaceMethodHeadRe captures the method signature following a Retrofit
// annotation. Handles both Kotlin and Java-style declarations:
//
//	suspend fun users(): List<User>
//	fun users(): Call<List<User>>
//	@JvmSuppressWildcards ... fun users(): ...
var ktInterfaceMethodHeadRe = regexp.MustCompile(
	`(?:suspend\s+)?fun\s+([A-Za-z_]\w*)\s*\(`,
)

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// ktEnvGetenvRe matches `System.getenv("NAME") + "/path"`.
// Used to detect runtime-dynamic Ktor / OkHttp URL arguments.
const ktEnvAccessFrag = `System\.getenv\s*\([^)]+\)`

// ktKtorClientEnvVerbRe matches `client.<verb>(System.getenv("X") + "/path")`.
//
// Capture groups: 1 = verb, 2 = path suffix
var ktKtorClientEnvVerbRe = regexp.MustCompile(
	`\b(?:httpClient|client|ktorClient|http|api|apiClient)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*` +
		ktEnvAccessFrag + `\s*\+\s*"([^"\n\r]*)"`,
)

// ---------------------------------------------------------------------------
// String constant table
// ---------------------------------------------------------------------------

// ktStringConstRe captures Kotlin string constant declarations:
//
//	val NAME = "/path"
//	const val NAME = "/path"
//	private val NAME = "/path"
var ktStringConstRe = regexp.MustCompile(
	`(?:const\s+)?val\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*String\s*)?=\s*"([^"\n\r]{1,256})"`,
)

// ---------------------------------------------------------------------------
// Enclosing function index
// ---------------------------------------------------------------------------

// ktEnclosingFuncRe captures Kotlin function declarations:
//
//	fun foo(...)
//	suspend fun foo(...)
//	private fun foo(...)
//	override suspend fun foo(...)
var ktEnclosingFuncRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(?:private|public|internal|protected|override|open|inline|suspend|\s)+\s+)?fun\s+([A-Za-z_]\w*)\s*\(`,
)

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// ktClientEmitFn is the runtime-aware emitter type for Kotlin clients.
type ktClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeKotlinClient is the package-level entry point referenced from
// applyHTTPEndpointSynthesis.
func synthesizeKotlinClient(content string, emit emitFn) {
	synthesizeKotlinClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizeKotlinClientWithRuntime runs the full Kotlin client scan.
func synthesizeKotlinClientWithRuntime(content string, emit ktClientEmitFn) {
	if !ktHasAnyHTTPClient(content) {
		return
	}
	funcs := indexKtEnclosingFunctions(content)
	syms := buildKtStringSymbolTable(content)

	// Retrofit base URL (file-scoped).
	var retrofitBase string
	if mm := ktRetrofitBaseURLRe.FindStringSubmatch(content); len(mm) >= 2 {
		retrofitBase = stripURLHost(mm[1])
	}

	// ----- Ktor direct verb calls: client.get("/path"), HttpClient().post(...) -----
	for _, m := range ktKtorClientVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := ktPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingKtFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "ktor", "Function", caller, false)
	}

	// ----- Ktor request builder: httpClient.request { url("...") } -----
	for _, m := range ktKtorRequestBuilderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		// Either group 2 (url("...")) or group 3 (url = "...").
		raw := ""
		if m[2] >= 0 {
			raw = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			raw = content[m[4]:m[5]]
		}
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingKtFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit("GET", canonical, "ktor", "Function", caller, false)
	}

	// ----- Ktor coroutine use lambda: httpClient.use { it.get("/path") } -----
	for _, m := range ktKtorUseLambdaRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := ""
		if m[4] >= 0 {
			raw = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			ident := content[m[6]:m[7]]
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
		caller := enclosingKtFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "ktor", "Function", caller, false)
	}

	// ----- OkHttp Request.Builder().url("...") -----
	for _, m := range ktOkHttpRequestBuilderURLRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
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
		// Resolve verb from the builder chain forward.
		verb := ktResolveOkHttpBuilderVerb(content, m[1])
		caller := enclosingKtFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "okhttp", "Function", caller, false)
	}

	// ----- Retrofit interface annotations -----
	for _, m := range ktRetrofitAnnotationRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		if retrofitBase != "" {
			path = composeBaseURL(retrofitBase, path)
		}
		caller := ktNextInterfaceMethod(content, m[1])
		if caller == "" {
			caller = enclosingKtFuncAt(funcs, m[0])
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "retrofit", "Function", caller, false)
	}

	// ----- Env-var concatenation: client.get(System.getenv("X") + "/path") -----
	for _, m := range ktKtorClientEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := content[m[4]:m[5]]
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingKtFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, suffix)
		emit(verb, canonical, "ktor", "Function", caller, true)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ktHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "HttpClient") ||
		strings.Contains(content, "httpClient") ||
		strings.Contains(content, "OkHttpClient") ||
		strings.Contains(content, "Request.Builder") ||
		strings.Contains(content, "@GET(") || strings.Contains(content, "@POST(") ||
		strings.Contains(content, "@PUT(") || strings.Contains(content, "@DELETE(") ||
		strings.Contains(content, "@PATCH(") || strings.Contains(content, "@HEAD(") ||
		strings.Contains(content, "@OPTIONS(") ||
		strings.Contains(content, "Retrofit") ||
		strings.Contains(content, ".R().") ||
		strings.Contains(content, "System.getenv") ||
		strings.Contains(content, "client.get") || strings.Contains(content, "client.post") ||
		strings.Contains(content, "httpClient.get") || strings.Contains(content, "httpClient.post")
}

// buildKtStringSymbolTable returns identifier → value map for Kotlin val/const val.
func buildKtStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range ktStringConstRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		if _, dup := syms[m[1]]; !dup {
			syms[m[1]] = m[2]
		}
	}
	return syms
}

// ktPickURLArg extracts the URL string from match groups at litStart
// (double-quoted), litStart+2 (single-quoted), litStart+4 (identifier).
func ktPickURLArg(content string, m []int, litStart int, syms map[string]string) string {
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

// ktResolveOkHttpBuilderVerb scans forward up to 512 bytes from `pos`
// for a verb terminator on the OkHttp builder chain.
func ktResolveOkHttpBuilderVerb(content string, pos int) string {
	end := pos + 512
	if end > len(content) {
		end = len(content)
	}
	window := content[pos:end]
	mm := ktOkHttpVerbBuilderRe.FindStringSubmatch(window)
	if len(mm) < 3 {
		return "GET"
	}
	if mm[1] != "" {
		return strings.ToUpper(mm[1])
	}
	if mm[2] != "" {
		return strings.ToUpper(mm[2])
	}
	return "GET"
}

// ktNextInterfaceMethod returns the fun name on the line immediately
// following a Retrofit annotation match.
func ktNextInterfaceMethod(content string, pos int) string {
	end := pos + 512
	if end > len(content) {
		end = len(content)
	}
	window := content[pos:end]
	mm := ktInterfaceMethodHeadRe.FindStringSubmatch(window)
	if len(mm) < 2 {
		return ""
	}
	return mm[1]
}

// indexKtEnclosingFunctions builds a sorted (offset, name) list for every
// Kotlin function declaration in the file.
func indexKtEnclosingFunctions(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range ktEnclosingFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingKtFuncAt returns the name of the nearest preceding function
// declaration for a call site at `pos`.
func enclosingKtFuncAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
