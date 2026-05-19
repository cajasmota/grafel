// Java consumer-side HTTP client synthesis (#721 wave 1).
//
// Mirrors http_endpoint_python_client.go for Java consumer-side HTTP
// patterns. Emits a synthetic http_endpoint (consumer side) per detected
// client call site; the caller in applyHTTPEndpointSynthesis pairs the
// emission with a FETCHES edge from the enclosing method to the endpoint.
//
// Patterns covered (per the wave-1 brief):
//
//   - Java 11+ stdlib HttpClient:
//	HttpRequest.newBuilder().uri(URI.create("/api/users")).build()
//	httpClient.send(req, ...)
//   - Spring RestTemplate:
//	restTemplate.getForObject("/api/users", User.class)
//	restTemplate.postForEntity("/api/users", body, User.class)
//	restTemplate.exchange("/api/users/{id}", HttpMethod.PUT, ...)
//   - Spring WebClient:
//	webClient.get().uri("/api/users").retrieve()...
//	webClient.post().uri("/api/users").bodyValue(b).retrieve()...
//   - OkHttp:
//	client.newCall(new Request.Builder().url("/api/users").build()).execute()
//	new Request.Builder().url(...).method("POST", body)
//   - Apache HttpClient:
//	httpclient.execute(new HttpGet("/api/users"))
//	httpclient.execute(new HttpPost("/api/users"))
//   - Retrofit (interface methods):
//	@GET("/api/users") Call<List<User>> users();
//	@POST("/api/users") Call<User> create(@Body User u);
//
// Beyond-minimum behaviours:
//   - Base URL composition from `HttpClient.Builder`, `RestTemplate.setRootUri`,
//     `WebClient.baseUrl(...)`, `OkHttpClient` (no native base) — composed via
//     `Retrofit.Builder().baseUrl(...)` for Retrofit interfaces.
//   - Constant folding of file-local `private static final String BASE = "...";`
//   - String concatenation collapsed when literal segments are recognisable.
//   - Runtime-dynamic URLs (env-var or System.getenv() in the argument) are
//     skipped — there is no path to canonicalise; the wave-2 brief picks
//     those up via the env-var symbol table.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Stdlib HttpClient (Java 11+)
// ---------------------------------------------------------------------------

// javaURICreateRe matches `URI.create("path")` — the canonical idiom for
// stdlib HttpClient. Captures the literal/identifier URL argument.
var javaURICreateRe = regexp.MustCompile(
	`\bURI\s*\.\s*create\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 1: string literal
		`|` +
		`([A-Za-z_][\w]*)` + // group 2: bare identifier
		`)\s*\)`,
)

// javaHttpRequestBuilderURIRe matches
// `HttpRequest.newBuilder().uri(URI.create("..."))...` and the eventual
// `.method("VERB", ...)` or `.GET()/.POST(...)/.PUT(...)/.DELETE()`
// terminator. We match the chain in two passes:
//   1. javaURICreateRe identifies each call site
//   2. for each URI we look back/forward up to 512 bytes for the verb
//      builder method that completes the request.
//
// The two-pass approach keeps the regex tractable across multi-line
// builder chains common in real Java code.

// javaBuilderVerbRe matches the verb terminator method on a HttpRequest
// builder chain. We accept the four explicit verb shorthands plus the
// generic `method("VERB", ...)` form.
var javaBuilderVerbRe = regexp.MustCompile(
	`\.\s*(?:` +
		`(GET|POST|PUT|DELETE)\s*\(` + // group 1: shorthand
		`|` +
		`method\s*\(\s*"([A-Za-z]+)"` + // group 2: explicit method("VERB", ...)
		`)`,
)

// ---------------------------------------------------------------------------
// Spring RestTemplate
// ---------------------------------------------------------------------------

// javaRestTemplateRe matches `<receiver>.<verbForXxx>("path", ...)` where
// receiver is a RestTemplate-shaped identifier (restTemplate / rest / template
// / restClient) and the suffix is the canonical Spring helper name.
//
// Map of suffix → verb:
//
//	getForObject / getForEntity → GET
//	postForObject / postForEntity / postForLocation → POST
//	put → PUT
//	delete → DELETE
//	patchForObject → PATCH
//	headForHeaders → HEAD
//	optionsForAllow → OPTIONS
var javaRestTemplateRe = regexp.MustCompile(
	`\b(restTemplate|rest|template|restClient)\s*\.\s*` +
		`(getForObject|getForEntity|postForObject|postForEntity|postForLocation|` +
		`put|delete|patchForObject|headForHeaders|optionsForAllow)\s*\(\s*` +
		`(?:"([^"\n\r]+)"|([A-Za-z_][\w]*))`,
)

// javaRestTemplateExchangeRe matches `restTemplate.exchange("path",
// HttpMethod.<VERB>, ...)`. The HttpMethod identifier carries the verb.
var javaRestTemplateExchangeRe = regexp.MustCompile(
	`\b(restTemplate|rest|template|restClient)\s*\.\s*exchange\s*\(\s*` +
		`(?:"([^"\n\r]+)"|([A-Za-z_][\w]*))\s*,\s*HttpMethod\.([A-Z]+)`,
)

// javaRestTemplateSetRootURIRe captures `restTemplate.setRootUri("...")`
// declarations for base URL composition.
var javaRestTemplateSetRootURIRe = regexp.MustCompile(
	`\b(restTemplate|rest|template|restClient)\.setRootUri\s*\(\s*"([^"\n\r]+)"`,
)

// ---------------------------------------------------------------------------
// Spring WebClient
// ---------------------------------------------------------------------------

// javaWebClientVerbURIRe matches `<webclient>.<verb>().uri("/path"[, ...])`.
// The receiver allow-list mirrors RestTemplate's; verb is captured from
// the leading `.<verb>()` call.
var javaWebClientVerbURIRe = regexp.MustCompile(
	`\b(webClient|client|httpClient)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*\)\s*\.\s*uri\s*\(\s*` +
		`(?:"([^"\n\r]+)"|([A-Za-z_][\w]*))`,
)

// javaWebClientBuilderBaseURLRe captures
// `WebClient.builder().baseUrl("...")` for base URL composition. Tolerates
// whitespace / newlines between the chained calls, which is the dominant
// real-world formatting for WebClient setup.
var javaWebClientBuilderBaseURLRe = regexp.MustCompile(
	`(?s)\bWebClient\s*\.\s*builder\s*\(\s*\)\s*\.\s*baseUrl\s*\(\s*"([^"\n\r]+)"`,
)

// ---------------------------------------------------------------------------
// OkHttp
// ---------------------------------------------------------------------------

// javaOkHttpRequestBuilderURLRe captures `new Request.Builder().url("...")`
// and its method-chain terminator. As with HttpRequest, we pair the URL
// hit with a nearby `.method("VERB", ...)` or `.get()/.post(...)/.put(...)/.delete()`
// call.
var javaOkHttpRequestBuilderURLRe = regexp.MustCompile(
	`new\s+Request\.Builder\s*\(\s*\)\s*\.\s*url\s*\(\s*(?:"([^"\n\r]+)"|([A-Za-z_][\w]*))`,
)

// javaOkHttpVerbBuilderRe matches the verb terminator on a Request.Builder
// chain. Shorthands: .get() / .post(body) / .put(body) / .delete([body]) /
// .head() / .patch(body); generic: .method("VERB", body).
var javaOkHttpVerbBuilderRe = regexp.MustCompile(
	`\.\s*(?:(get|post|put|delete|head|patch)\s*\(` +
		`|` +
		`method\s*\(\s*"([A-Za-z]+)"\s*,)`,
)

// ---------------------------------------------------------------------------
// Apache HttpClient
// ---------------------------------------------------------------------------

// javaApacheHttpMethodCtorRe matches the per-verb method-object constructors
// HttpGet / HttpPost / HttpPut / HttpDelete / HttpPatch / HttpHead / HttpOptions
// taking a URL string. Apache HttpClient encodes the verb in the class name.
var javaApacheHttpMethodCtorRe = regexp.MustCompile(
	`new\s+Http(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*(?:"([^"\n\r]+)"|([A-Za-z_][\w]*))`,
)

// ---------------------------------------------------------------------------
// Retrofit
// ---------------------------------------------------------------------------

// javaRetrofitAnnotationRe captures Retrofit per-method annotations on
// interface methods: @GET("/path") / @POST("/path") / @PUT / @DELETE /
// @PATCH / @HEAD / @OPTIONS. We capture the verb and path; the enclosing
// method name comes from the function index.
var javaRetrofitAnnotationRe = regexp.MustCompile(
	`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"\n\r]+)"\s*\)`,
)

// javaRetrofitBaseURLRe captures `Retrofit.Builder().baseUrl("...")` from
// retrofit setup, used to compose Retrofit interface paths with the base
// URL when the same file declares both. The (?s) flag lets `.` span
// newlines so multi-line builder chains are recognised.
var javaRetrofitBaseURLRe = regexp.MustCompile(
	`(?s)\bRetrofit\s*\.\s*Builder\s*\(\s*\)[^;]*?\.\s*baseUrl\s*\(\s*"([^"\n\r]+)"`,
)

// ---------------------------------------------------------------------------
// Symbol table helpers (constants, enclosing methods)
// ---------------------------------------------------------------------------

// javaStringConstRe captures simple `[private|public|protected]
// [static] [final] String NAME = "value";` declarations.
var javaStringConstRe = regexp.MustCompile(
	`(?:private|public|protected|static|final|\s)+\s+String\s+([A-Za-z_][\w]*)\s*=\s*"([^"\n\r]+)"\s*;`,
)

// javaEnvGetenvRe matches `System.getenv("NAME")` or
// `System.getenv("NAME") + "/path"`. Used to detect runtime-dynamic URLs.
var javaEnvGetenvRe = regexp.MustCompile(
	`System\.getenv\s*\(\s*"[^"]+"\s*\)\s*\+\s*"([^"\n\r]*)"`,
)

// javaURICreateEnvRe matches `URI.create(System.getenv("X") + "/path")`.
// These are env-var-derived URLs; we emit the path suffix with
// runtime_dynamic=true so the repair flow can annotate them.
var javaURICreateEnvRe = regexp.MustCompile(
	`\bURI\s*\.\s*create\s*\(\s*System\.getenv\s*\(\s*"[^"]+"\s*\)\s*\+\s*"([^"\n\r]*)"`,
)

// javaEnclosingMethodRe captures `<modifiers> <return-type> <name>(...)` at
// the start of a method declaration. Heuristic — we accept any line that
// looks plausibly like a method header, including `void`, primitive, and
// generic return types. The enclosing-class name is not threaded through;
// the method name alone is sufficient for the source_caller property.
var javaEnclosingMethodRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:public|private|protected|static|final|abstract|synchronized|default|\s)+` +
		`[\w<>,\[\]\s.?]+\s+([A-Za-z_]\w*)\s*\([^;]*?\)\s*(?:throws\s+[\w.,\s]+)?\s*\{`,
)

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

// javaClientEmitFn mirrors pyClientEmitFn — a runtime-dynamic-aware
// emitter so the caller can stamp `runtime_dynamic=true` on
// env-resolved URLs.
type javaClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeJavaClient is the package-level entry point referenced from
// applyHTTPEndpointSynthesis. The standard engine `emitFn` is adapted to
// the runtime-aware emitter below.
func synthesizeJavaClient(content string, emit emitFn) {
	synthesizeJavaClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizeJavaClientWithRuntime runs the full per-framework scan.
func synthesizeJavaClientWithRuntime(content string, emit javaClientEmitFn) {
	if !javaHasAnyHTTPClient(content) {
		return
	}
	methods := indexJavaEnclosingMethods(content)
	syms := buildJavaStringSymbolTable(content)

	// Base URL inference, file-scoped. We pick the FIRST declaration we
	// find for each framework type; mixed-framework files are uncommon.
	var restTemplateBase, webClientBase, retrofitBase string
	if mm := javaRestTemplateSetRootURIRe.FindStringSubmatch(content); len(mm) >= 3 {
		restTemplateBase = stripURLHost(mm[2])
	}
	if mm := javaWebClientBuilderBaseURLRe.FindStringSubmatch(content); len(mm) >= 2 {
		webClientBase = stripURLHost(mm[1])
	}
	if mm := javaRetrofitBaseURLRe.FindStringSubmatch(content); len(mm) >= 2 {
		retrofitBase = stripURLHost(mm[1])
	}

	// ----- stdlib HttpClient via URI.create + builder verb -----
	for _, m := range javaURICreateRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		raw := javaPickURLArg(content, m, 2, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		// Look forward up to 512 bytes (across newlines) for a verb
		// terminator. If none, default to GET — the stdlib client uses
		// GET when no method is specified on the builder.
		verb := javaResolveBuilderVerb(content, m[1], javaBuilderVerbRe)
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "http_client", "Function", caller, false)
	}

	// ----- Spring RestTemplate helper methods -----
	for _, m := range javaRestTemplateRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		suffix := content[m[4]:m[5]]
		verb := javaRestTemplateSuffixVerb(suffix)
		raw := javaPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		if restTemplateBase != "" {
			path = composeBaseURL(restTemplateBase, path)
		}
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "rest_template", "Function", caller, false)
	}

	// ----- RestTemplate.exchange("path", HttpMethod.VERB, ...) -----
	for _, m := range javaRestTemplateExchangeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		raw := javaPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		verb := strings.ToUpper(content[m[8]:m[9]])
		if restTemplateBase != "" {
			path = composeBaseURL(restTemplateBase, path)
		}
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "rest_template", "Function", caller, false)
	}

	// ----- Spring WebClient -----
	for _, m := range javaWebClientVerbURIRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := javaPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		if webClientBase != "" {
			path = composeBaseURL(webClientBase, path)
		}
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "web_client", "Function", caller, false)
	}

	// ----- OkHttp Request.Builder -----
	for _, m := range javaOkHttpRequestBuilderURLRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		raw := javaPickURLArg(content, m, 2, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		verb := javaResolveBuilderVerb(content, m[1], javaOkHttpVerbBuilderRe)
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "okhttp", "Function", caller, false)
	}

	// ----- Apache HttpClient method-object constructors -----
	for _, m := range javaApacheHttpMethodCtorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := javaPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "apache_httpclient", "Function", caller, false)
	}

	// ----- Retrofit interface annotations -----
	for _, m := range javaRetrofitAnnotationRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		if retrofitBase != "" {
			path = composeBaseURL(retrofitBase, path)
		}
		// Pull the interface method name from the next non-annotation
		// line following the @VERB annotation. Falls back to the
		// enclosing method index if the parse fails.
		caller := javaNextInterfaceMethod(content, m[1])
		if caller == "" {
			caller = enclosingJavaMethodAt(methods, m[0])
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "retrofit", "Function", caller, false)
	}

	// ----- Env-var URL concatenation: URI.create(System.getenv("X") + "/path") -----
	// Emits the path suffix with runtime_dynamic=true so the repair flow
	// (#732) can annotate the resulting synthetic. The verb is resolved the
	// same way as standard HttpClient URIs — look forward for a builder
	// verb terminator.
	for _, m := range javaURICreateEnvRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		suffix := content[m[2]:m[3]]
		if suffix == "" || !looksLikeURLPath(suffix) {
			continue
		}
		path := stripURLHost(suffix)
		verb := javaResolveBuilderVerb(content, m[1], javaBuilderVerbRe)
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "http_client", "Function", caller, true)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func javaHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "URI.create") ||
		strings.Contains(content, "System.getenv") ||
		strings.Contains(content, "restTemplate") ||
		strings.Contains(content, "RestTemplate") ||
		strings.Contains(content, "webClient") ||
		strings.Contains(content, "WebClient") ||
		strings.Contains(content, "Request.Builder") ||
		strings.Contains(content, "newCall") ||
		strings.Contains(content, "HttpGet") || strings.Contains(content, "HttpPost") ||
		strings.Contains(content, "HttpPut") || strings.Contains(content, "HttpDelete") ||
		strings.Contains(content, "HttpPatch") || strings.Contains(content, "HttpHead") ||
		strings.Contains(content, "HttpOptions") ||
		strings.Contains(content, "@GET(") || strings.Contains(content, "@POST(") ||
		strings.Contains(content, "@PUT(") || strings.Contains(content, "@DELETE(") ||
		strings.Contains(content, "@PATCH(") || strings.Contains(content, "@HEAD(") ||
		strings.Contains(content, "@OPTIONS(")
}

func buildJavaStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range javaStringConstRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		if _, dup := syms[m[1]]; !dup {
			syms[m[1]] = m[2]
		}
	}
	return syms
}

// javaPickURLArg extracts the URL string from a match's literal/identifier
// group pair. `litStart` is the index within `m` of the literal group;
// `litStart+2` is the identifier group.
func javaPickURLArg(content string, m []int, litStart int, syms map[string]string) string {
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		ident := content[m[litStart+2]:m[litStart+3]]
		if v, ok := syms[ident]; ok {
			return v
		}
	}
	return ""
}

// javaRestTemplateSuffixVerb maps a RestTemplate helper-method suffix to
// its HTTP verb.
func javaRestTemplateSuffixVerb(suffix string) string {
	switch suffix {
	case "getForObject", "getForEntity":
		return "GET"
	case "postForObject", "postForEntity", "postForLocation":
		return "POST"
	case "put":
		return "PUT"
	case "delete":
		return "DELETE"
	case "patchForObject":
		return "PATCH"
	case "headForHeaders":
		return "HEAD"
	case "optionsForAllow":
		return "OPTIONS"
	}
	return "GET"
}

// javaResolveBuilderVerb scans forward up to 512 bytes from `pos` looking
// for a verb terminator matched by `verbRe`. Returns "GET" if no
// terminator is found within budget (the stdlib HttpClient default).
//
// verbRe captures TWO alternative verb groups (group 1 = shorthand,
// group 2 = explicit method("VERB")). We pick whichever is non-empty.
func javaResolveBuilderVerb(content string, pos int, verbRe *regexp.Regexp) string {
	end := pos + 512
	if end > len(content) {
		end = len(content)
	}
	window := content[pos:end]
	mm := verbRe.FindStringSubmatch(window)
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

// javaInterfaceMethodHeadRe captures the next `<return-type> <name>(...)`
// declaration after a Retrofit annotation. We accept declarations ending
// in either `;` (interface form) or `{` (class form).
var javaInterfaceMethodHeadRe = regexp.MustCompile(
	`[\w<>,\[\]?\s.]*\s+([A-Za-z_]\w*)\s*\([^;{]*\)\s*(?:throws\s+[\w.,\s]+)?\s*[;{]`,
)

// javaNextInterfaceMethod returns the method name on the line immediately
// following a Retrofit annotation match. Returns "" when no method
// declaration is found within 512 bytes.
func javaNextInterfaceMethod(content string, pos int) string {
	end := pos + 512
	if end > len(content) {
		end = len(content)
	}
	window := content[pos:end]
	// Strip any intervening annotations on subsequent lines (e.g.
	// @Headers, @Streaming) — we only care about the eventual method
	// declaration.
	mm := javaInterfaceMethodHeadRe.FindStringSubmatch(window)
	if len(mm) < 2 {
		return ""
	}
	return mm[1]
}

// indexJavaEnclosingMethods builds a sorted (offset, name) list for every
// method header in the file. Used to attribute non-Retrofit emissions to
// the enclosing function.
func indexJavaEnclosingMethods(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range javaEnclosingMethodRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

func enclosingJavaMethodAt(methods []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(methods, pos)
}
