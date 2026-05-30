// Java consumer-side HTTP client synthesis (#721 wave 1, #796 Quarkus extension).
//
// Mirrors http_endpoint_python_client.go for Java consumer-side HTTP
// patterns. Emits a synthetic http_endpoint (consumer side) per detected
// client call site; the caller in applyHTTPEndpointSynthesis pairs the
// emission with a FETCHES edge from the enclosing method to the endpoint.
//
// Patterns covered (per the wave-1 brief):
//
//   - Java 11+ stdlib HttpClient:
//     HttpRequest.newBuilder().uri(URI.create("/api/users")).build()
//     httpClient.send(req, ...)
//   - Spring RestTemplate:
//     restTemplate.getForObject("/api/users", User.class)
//     restTemplate.postForEntity("/api/users", body, User.class)
//     restTemplate.exchange("/api/users/{id}", HttpMethod.PUT, ...)
//   - Spring WebClient:
//     webClient.get().uri("/api/users").retrieve()...
//     webClient.post().uri("/api/users").bodyValue(b).retrieve()...
//   - OkHttp:
//     client.newCall(new Request.Builder().url("/api/users").build()).execute()
//     new Request.Builder().url(...).method("POST", body)
//   - Apache HttpClient:
//     httpclient.execute(new HttpGet("/api/users"))
//     httpclient.execute(new HttpPost("/api/users"))
//   - Retrofit (interface methods):
//     @GET("/api/users") Call<List<User>> users();
//     @POST("/api/users") Call<User> create(@Body User u);
//
// #796 extensions:
//
//   - MicroProfile @RegisterRestClient (Quarkus):
//     @RegisterRestClient @Path("/customers") interface CustomerApiClient { @GET @Path("/{id}") ... }
//     @Inject @RestClient CustomerApiClient customerApi; → customerApi.getCustomer(id)
//   - @ClientHeaderParam and @ClientRequestFilter recording (auth/header pattern)
//   - @RegisterClientHeaders recording (auto-header propagator)
//   - Spring Cloud OpenFeign (@FeignClient):
//     @FeignClient(name="customer", url="${...}") interface CustomerClient { ... }
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
	"sync"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Cross-file DI registry (#845 — Option B)
//
// The per-file synthesis pass cannot see @RegisterRestClient interfaces
// declared in a DIFFERENT file. This global registry is populated with a
// pre-pass (ScanJavaDIRegistry) before the per-file synthesis runs. The
// Quarkus and Feign consumer passes then look up the cross-file registry
// when a local lookup fails.
//
// Lifecycle:
//   - ScanJavaDIRegistry(content) populates the registry from one file.
//   - synthesizeQuarkusRestClient / synthesizeFeignClient fall back to the
//     registry when the local (same-file) interface is not found.
//   - ClearJavaDIRegistry() resets the registry (test isolation / new index run).
// ---------------------------------------------------------------------------

// javaDIMethodMap maps method simple-name → (verb, canonicalPath).
type javaDIMethodMap = map[string]restClientMethodEntry

// JavaDIRegistry maps interface simple-name → javaDIMethodMap.
// Exported so callers outside the engine package can build and pass it.
type JavaDIRegistry map[string]javaDIMethodMap

// javaDIGlobal is the package-level cross-file registry, protected by a
// reader-writer mutex so parallel index passes can populate it safely.
var (
	javaDIGlobal   JavaDIRegistry = JavaDIRegistry{}
	javaDIGlobalMu sync.RWMutex
)

// ScanJavaDIRegistry extracts all @RegisterRestClient and @FeignClient
// interface definitions from `content` and merges them into the global
// cross-file registry. Safe for concurrent calls from parallel file walkers.
//
// Call once per Java source file BEFORE the per-file synthesis pass runs.
// The registry is additive — subsequent calls never overwrite earlier entries
// (first-declaration-wins, which is correct for a well-typed codebase where
// each interface name is unique within a repo).
func ScanJavaDIRegistry(content string) {
	if !javaHasRestClientMarker(content) {
		return
	}
	local := buildQuarkusDIEntries(content)
	local = mergeFeignDIEntries(content, local)
	if len(local) == 0 {
		return
	}
	javaDIGlobalMu.Lock()
	defer javaDIGlobalMu.Unlock()
	for ifaceName, methodMap := range local {
		if _, exists := javaDIGlobal[ifaceName]; !exists {
			javaDIGlobal[ifaceName] = methodMap
		}
	}
}

// ClearJavaDIRegistry resets the global cross-file DI registry.
// Call at the start of each index run and in test teardowns.
func ClearJavaDIRegistry() {
	javaDIGlobalMu.Lock()
	defer javaDIGlobalMu.Unlock()
	javaDIGlobal = JavaDIRegistry{}
}

// lookupDIRegistry returns the method map for `ifaceName` by checking first
// the provided local registry, then the global cross-file registry.
func lookupDIRegistry(ifaceName string, local JavaDIRegistry) (javaDIMethodMap, bool) {
	if mm, ok := local[ifaceName]; ok {
		return mm, true
	}
	javaDIGlobalMu.RLock()
	defer javaDIGlobalMu.RUnlock()
	mm, ok := javaDIGlobal[ifaceName]
	return mm, ok
}

// buildQuarkusDIEntries parses all @RegisterRestClient interfaces in content
// and returns their method maps (same logic as Pass 1 of synthesizeQuarkusRestClient).
func buildQuarkusDIEntries(content string) JavaDIRegistry {
	registry := JavaDIRegistry{}
	for _, annotMatch := range javaRegisterRestClientHeaderRe.FindAllStringIndex(content, -1) {
		searchEnd := annotMatch[1] + 512
		if searchEnd > len(content) {
			searchEnd = len(content)
		}
		header := content[annotMatch[1]:searchEnd]

		classPath := ""
		ifaceIdx := javaInterfaceDeclRe.FindStringIndex(header)
		if ifaceIdx == nil {
			continue
		}
		headerBefore := header[:ifaceIdx[0]]
		if pm := javaClassLevelPathRe.FindStringSubmatch(headerBefore); pm != nil {
			classPath = pm[1]
		}
		if classPath == "" {
			lookback := annotMatch[0] - 256
			if lookback < 0 {
				lookback = 0
			}
			priorSlice := content[lookback:annotMatch[0]]
			if pm := javaClassLevelPathRe.FindStringSubmatch(priorSlice); pm != nil {
				classPath = pm[1]
			}
		}

		ifaceNameMatch := javaInterfaceDeclRe.FindStringSubmatch(header)
		if len(ifaceNameMatch) < 2 {
			continue
		}
		ifaceName := ifaceNameMatch[1]

		bodyStartInFull := annotMatch[1] + ifaceIdx[1]
		body := javaFindInterfaceBody(content, bodyStartInFull)
		if body == "" {
			continue
		}
		methodMap := parseRestClientInterfaceMethods(body, classPath)
		registry[ifaceName] = methodMap
	}
	return registry
}

// mergeFeignDIEntries parses all @FeignClient interfaces in content and adds
// them to `into`, returning the merged result.
func mergeFeignDIEntries(content string, into JavaDIRegistry) JavaDIRegistry {
	if !strings.Contains(content, "FeignClient") {
		return into
	}
	for _, decl := range parseFeignClients(content) {
		body := javaFindInterfaceBody(content, decl.bodyStart)
		if body == "" {
			continue
		}
		methodMap := parseFeignInterfaceMethods(body, decl.url)
		into[decl.ifaceName] = methodMap
	}
	return into
}

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
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
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
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
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
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
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
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
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
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
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
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
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
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
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
		path, pathOK := normalizeRawClientPath(suffix) // #807
		if !pathOK {
			continue
		}
		verb := javaResolveBuilderVerb(content, m[1], javaBuilderVerbRe)
		caller := enclosingJavaMethodAt(methods, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "http_client", "Function", caller, true)
	}

	// ----- MicroProfile @RegisterRestClient (Quarkus) — #796 -----
	synthesizeQuarkusRestClient(content, emit)

	// ----- Spring Cloud OpenFeign (@FeignClient) — #796 beyond-minimum -----
	synthesizeFeignClient(content, emit)
}

// ---------------------------------------------------------------------------
// MicroProfile @RegisterRestClient (Quarkus) — #796
// ---------------------------------------------------------------------------
//
// Detection strategy (two-pass):
//
//  Pass 1 — Interface scanning. For each interface annotated with
//  @RegisterRestClient (and optionally @Path on the interface), we
//  extract the class-level path prefix and then scan each method
//  annotated with @GET/@POST/... plus @Path("...").
//
//  Pass 2 — Usage scanning. We look for @Inject @RestClient fields
//  that reference a recognised interface type, then find call sites of
//  methods on that field. The enclosing method of each call site becomes
//  the FETCHES edge source.
//
// The two passes share a registry: maps interface-simple-name →
// map[method-name](verb, fullPath).

// javaRegisterRestClientHeaderRe matches just the @RegisterRestClient
// annotation marker (short or FQ form), used to locate declaration start.
var javaRegisterRestClientHeaderRe = regexp.MustCompile(
	`@(?:org\.eclipse\.microprofile\.rest\.client\.inject\.)?RegisterRestClient` +
		`(?:\s*\([^)]*\))?`, // optional (...) e.g. baseUri="..."
)

// javaInterfaceDeclRe matches the interface keyword + name after annotations.
// Group 1: interface name.
var javaInterfaceDeclRe = regexp.MustCompile(
	`(?:public\s+)?interface\s+(\w+)`,
)

// javaClassLevelPathRe extracts a @Path annotation value.
// Group 1: path string.
var javaClassLevelPathRe = regexp.MustCompile(
	`@(?:javax\.ws\.rs\.|jakarta\.ws\.rs\.)?Path\s*\(\s*"([^"\n\r]*)"\s*\)`,
)

// javaVerbAnnotationRe matches a standalone verb annotation (no path in it).
// Group 1: verb name.
var javaVerbAnnotationRe = regexp.MustCompile(
	`@(?:javax\.ws\.rs\.|jakarta\.ws\.rs\.)?(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`,
)

// javaMethodNameRe matches a method declaration (return-type + name + paren
// ending with `;` for interface methods). The negative lookbehind excludes
// annotation values like `@Path("...")` — an annotation is preceded by `@`,
// so we require that the identifier is NOT directly preceded by `@`.
// Pattern: `<return-type-word> <name>(...)..;` where the name is a word NOT
// preceded by `@`.
// Group 1: method name.
var javaMethodNameRe = regexp.MustCompile(
	`(?:[\w<>,\[\]?\s.]+)\s+([a-z][A-Za-z0-9_]*)\s*\([^;{]*\)\s*(?:throws\s+[\w.,\s]+)?\s*;`,
)

// javaInjectRestClientFieldRe matches a field injection pattern:
//
//	@Inject @RestClient <InterfaceType> <fieldName>;
//	(annotations can appear in either order)
var javaInjectRestClientFieldRe = regexp.MustCompile(
	`@(?:javax\.inject\.|jakarta\.inject\.)?Inject[\s\n]+` +
		`@(?:org\.eclipse\.microprofile\.rest\.client\.inject\.)?RestClient[\s\n]+` +
		`(\w+)\s+(\w+)\s*;`, // group 1: interface type, group 2: field name
)

// javaInjectRestClientFieldRe2 accepts the other annotation order:
//
//	@RestClient @Inject <InterfaceType> <fieldName>;
var javaInjectRestClientFieldRe2 = regexp.MustCompile(
	`@(?:org\.eclipse\.microprofile\.rest\.client\.inject\.)?RestClient[\s\n]+` +
		`@(?:javax\.inject\.|jakarta\.inject\.)?Inject[\s\n]+` +
		`(\w+)\s+(\w+)\s*;`, // group 1: interface type, group 2: field name
)

// javaRestClientCallRe matches `<fieldName>.<methodName>(...)` call sites.
// Group 1: field name. Group 2: method name.
var javaRestClientCallRe = regexp.MustCompile(
	`\b(\w+)\s*\.\s*(\w+)\s*\(`,
)

// javaClientHeaderParamRe detects @ClientHeaderParam (beyond-minimum).
var javaClientHeaderParamRe = regexp.MustCompile(
	`@ClientHeaderParam\s*\(\s*name\s*=\s*"([^"\n\r]*)"\s*`,
)

// javaRegisterClientHeadersRe detects @RegisterClientHeaders (beyond-minimum).
var javaRegisterClientHeadersRe = regexp.MustCompile(
	`@RegisterClientHeaders\s*(?:\(\s*(\w+)\.class\s*\))?`,
)

// restClientMethodEntry holds the extracted verb and full composed path for
// one method on a @RegisterRestClient interface.
type restClientMethodEntry struct {
	verb     string
	fullPath string
}

// synthesizeQuarkusRestClient scans `content` for @RegisterRestClient
// interface definitions and @Inject @RestClient field usages, then emits
// FETCHES edges for each call site found in the consuming class.
//
// Pass 1 builds an interface registry (interface name → method name → entry).
// Pass 2 finds injected field names + types, then matches call sites.
func synthesizeQuarkusRestClient(content string, emit javaClientEmitFn) {
	if !javaHasRestClientMarker(content) {
		return
	}
	methods := indexJavaEnclosingMethods(content)

	// ---- Pass 1: parse @RegisterRestClient interfaces in the same file ----
	// registry: interface simple name → method name → (verb, fullPath)
	registry := map[string]map[string]restClientMethodEntry{}

	for _, annotMatch := range javaRegisterRestClientHeaderRe.FindAllStringIndex(content, -1) {
		// Scan forward up to 512 bytes to find the interface declaration.
		searchEnd := annotMatch[1] + 512
		if searchEnd > len(content) {
			searchEnd = len(content)
		}
		header := content[annotMatch[1]:searchEnd]

		// Extract class-level @Path if present before `interface`.
		classPath := ""
		ifaceIdx := javaInterfaceDeclRe.FindStringIndex(header)
		if ifaceIdx == nil {
			continue
		}
		// Look for @Path within the header slice before the interface keyword.
		headerBefore := header[:ifaceIdx[0]]
		if pm := javaClassLevelPathRe.FindStringSubmatch(headerBefore); pm != nil {
			classPath = pm[1]
		}
		// Also check the few bytes just before @RegisterRestClient (in case
		// @Path precedes @RegisterRestClient in source order).
		checkBefore := annotMatch[0]
		if classPath == "" {
			lookback := checkBefore - 256
			if lookback < 0 {
				lookback = 0
			}
			priorSlice := content[lookback:annotMatch[0]]
			if pm := javaClassLevelPathRe.FindStringSubmatch(priorSlice); pm != nil {
				classPath = pm[1]
			}
		}

		ifaceNameMatch := javaInterfaceDeclRe.FindStringSubmatch(header)
		if len(ifaceNameMatch) < 2 {
			continue
		}
		ifaceName := ifaceNameMatch[1]

		// Find the interface body starting from after the interface declaration.
		bodyStartInFull := annotMatch[1] + ifaceIdx[1]
		body := javaFindInterfaceBody(content, bodyStartInFull)
		if body == "" {
			continue
		}

		// Parse interface body procedurally: find verb annotations then
		// optional @Path then the method name on the declaration line.
		methodMap := parseRestClientInterfaceMethods(body, classPath)
		registry[ifaceName] = methodMap
		_ = checkBefore
	}

	// Always emit consumer endpoint entities for every method found in
	// @RegisterRestClient interfaces — even when no consumer call site is
	// visible in this file (cross-file injection scenario). This ensures the
	// http_endpoint consumer entity exists in the graph for cross-repo linker
	// matching, even if no FETCHES edge is emitted here.
	for _, methodMap := range registry {
		for _, entry := range methodMap {
			if entry.fullPath != "" {
				emit(entry.verb, entry.fullPath, "quarkus_rest_client", "Function", "", false)
			}
		}
	}

	if len(registry) == 0 {
		return
	}

	// ---- Pass 2: find @Inject @RestClient fields and call sites ----
	// fieldToIface: field variable name → interface name.
	// We match against both the local registry (same-file interfaces) AND the
	// global cross-file DI registry (#845 Option B) so consumers in file B can
	// resolve interfaces declared in file A.
	fieldToIface := map[string]string{}

	for _, mm := range javaInjectRestClientFieldRe.FindAllStringSubmatch(content, -1) {
		if len(mm) < 3 {
			continue
		}
		ifaceType, fieldName := mm[1], mm[2]
		if _, known := lookupDIRegistry(ifaceType, registry); known {
			fieldToIface[fieldName] = ifaceType
		}
	}
	for _, mm := range javaInjectRestClientFieldRe2.FindAllStringSubmatch(content, -1) {
		if len(mm) < 3 {
			continue
		}
		ifaceType, fieldName := mm[1], mm[2]
		if _, known := lookupDIRegistry(ifaceType, registry); known {
			fieldToIface[fieldName] = ifaceType
		}
	}

	if len(fieldToIface) == 0 {
		return
	}

	// Scan for call sites: <fieldName>.<methodName>(...)
	for _, callMatch := range javaRestClientCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(callMatch) < 6 {
			continue
		}
		fieldName := content[callMatch[2]:callMatch[3]]
		methodName := content[callMatch[4]:callMatch[5]]

		ifaceName, ok := fieldToIface[fieldName]
		if !ok {
			continue
		}
		// Look up method entry in local registry first, then cross-file registry.
		methodMap, ok := lookupDIRegistry(ifaceName, registry)
		if !ok {
			continue
		}
		entry, ok := methodMap[methodName]
		if !ok {
			continue
		}

		// Beyond-minimum: @ClientHeaderParam and @RegisterClientHeaders are
		// markers we note but route to the same framework label. A future
		// pass can inspect properties for header-propagation metadata.
		framework := "quarkus_rest_client"
		_ = javaClientHeaderParamRe
		_ = javaRegisterClientHeadersRe

		caller := enclosingJavaMethodAt(methods, callMatch[0])
		emit(entry.verb, entry.fullPath, framework, "Function", caller, false)
	}
}

// parseRestClientInterfaceMethods scans the body of a @RegisterRestClient
// interface and extracts (method-name → restClientMethodEntry) pairs.
//
// The scan works as a small state machine:
//  1. Collect all verb annotation positions (sorted by offset).
//  2. For each verb, the scan window is [verb_end .. next_verb_start).
//     This prevents cross-method @Path contamination.
//  3. Within that window: find optional @Path → method-level path;
//     find method name (identifier before `(`).
//  4. Compose classPath + methodPath → canonical full path.
func parseRestClientInterfaceMethods(body, classPath string) map[string]restClientMethodEntry {
	out := map[string]restClientMethodEntry{}

	allVerbs := javaVerbAnnotationRe.FindAllStringIndex(body, -1)
	for i, verbMatch := range allVerbs {
		vm := javaVerbAnnotationRe.FindStringSubmatch(body[verbMatch[0]:verbMatch[1]])
		if len(vm) < 2 {
			continue
		}
		verb := strings.ToUpper(vm[1])

		// Window ends at the start of the NEXT verb annotation (or +512 bytes).
		windowStart := verbMatch[1]
		windowEnd := windowStart + 512
		if i+1 < len(allVerbs) {
			nextVerbStart := allVerbs[i+1][0]
			if nextVerbStart > windowStart && nextVerbStart < windowEnd {
				windowEnd = nextVerbStart
			}
		}
		if windowEnd > len(body) {
			windowEnd = len(body)
		}
		window := body[windowStart:windowEnd]

		// Method-level @Path (optional) — first one found in window.
		methodPath := ""
		if pm := javaClassLevelPathRe.FindStringSubmatch(window); pm != nil {
			methodPath = pm[1]
		}

		// Method name: first `identifier(` in window that is a method declaration.
		methodName := javaNextMethodNameInWindow(window)
		if methodName == "" {
			continue
		}

		fullPath := composeBaseURL(classPath, methodPath)
		path, ok := normalizeRawClientPath(fullPath)
		if !ok {
			path = fullPath
		}
		if path == "" && classPath == "" {
			// No path at all — skip to avoid emitting http:VERB: junk.
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		out[methodName] = restClientMethodEntry{verb: verb, fullPath: canonical}
	}
	return out
}

// javaNextMethodNameInWindow finds the first method declaration (not an
// annotation, not a type) in `window` and returns the method name.
func javaNextMethodNameInWindow(window string) string {
	mm := javaMethodNameRe.FindStringSubmatch(window)
	if len(mm) < 2 {
		return ""
	}
	return mm[1]
}

// javaHasRestClientMarker is a fast pre-check to avoid the full scan on
// files that contain no MicroProfile or Feign markers.
func javaHasRestClientMarker(content string) bool {
	return strings.Contains(content, "RegisterRestClient") ||
		strings.Contains(content, "@RestClient") ||
		strings.Contains(content, "FeignClient")
}

// javaFindInterfaceBody returns the content between the first '{' at or
// after `start` (inclusive) and the matching '}', up to 8192 bytes. The
// naïve brace counter is sufficient for typical interface bodies which do
// not contain deeply-nested generics or string literals with braces.
func javaFindInterfaceBody(content string, start int) string {
	depth := 0
	began := -1
	limit := start + 8192
	if limit > len(content) {
		limit = len(content)
	}
	for i := start; i < limit; i++ {
		switch content[i] {
		case '{':
			if depth == 0 {
				began = i + 1
			}
			depth++
		case '}':
			depth--
			if depth == 0 && began >= 0 {
				return content[began:i]
			}
		}
	}
	if began >= 0 {
		return content[began:limit]
	}
	return ""
}

// ---------------------------------------------------------------------------
// Spring Cloud OpenFeign (@FeignClient) — #796 beyond-minimum
// ---------------------------------------------------------------------------
//
// @FeignClient(name="customer-service", url="${customer.url}")
// public interface CustomerClient {
//     @GetMapping("/customers/{id}")   // or @RequestMapping(method=GET)
//     Customer getCustomer(@PathVariable String id);
// }
//
// Detection mirrors @RegisterRestClient but uses Spring MVC verb annotations
// (@GetMapping / @PostMapping / @RequestMapping) on the interface methods
// instead of JAX-RS annotations.

// javaFeignClientHeadRe matches the head of a @FeignClient annotation up to its
// opening paren. The argument list (for the url attribute) is then read with a
// string-aware balanced scan, and the `interface NAME` declaration that follows
// is located by a parens-immune forward scan that skips any number of
// intervening annotations (e.g. @Validated, @Tag(name = "x (y)")). The previous
// single regex used a `(?:@[\w.]+(?:\s*\([^)]*\))?\s*)*` decorator-skip whose
// `[^)]*` stopped at the first ')' inside an intervening annotation's string,
// silently dropping the whole @FeignClient interface.
var javaFeignClientHeadRe = regexp.MustCompile(`@FeignClient\s*\(`)

// javaFeignURLRe extracts the url attribute from a @FeignClient argument string.
var javaFeignURLRe = regexp.MustCompile(`url\s*=\s*"([^"\n\r]*)"`)

// feignClientDecl is a parsed @FeignClient interface header.
type feignClientDecl struct {
	url       string // url attribute value (may be empty)
	ifaceName string // interface name
	bodyStart int    // byte offset at/after `interface NAME`, for body scan
}

// parseFeignClients finds every @FeignClient-annotated interface in `content`
// using a string-aware balanced scan for the annotation argument list and a
// parens-immune forward scan to the `interface NAME` declaration. This is the
// parens-in-string-immune replacement for javaFeignClientRe.
func parseFeignClients(content string) []feignClientDecl {
	heads := javaFeignClientHeadRe.FindAllStringIndex(content, -1)
	if len(heads) == 0 {
		return nil
	}
	var out []feignClientDecl
	for _, h := range heads {
		open := h[1] - 1 // index of '('
		closeAt := javaFindMatchingCloseString(content, open)
		if closeAt <= open {
			continue
		}
		args := content[open+1 : closeAt]
		url := ""
		if m := javaFeignURLRe.FindStringSubmatch(args); len(m) >= 2 {
			url = stripURLHost(m[1])
		}
		// Scan forward past intervening annotations to the interface decl.
		decl := javaSkipAnnotationsToDecl(content, closeAt+1)
		if decl < 0 {
			continue
		}
		m := javaInterfaceDeclRe.FindStringSubmatchIndex(content[decl:])
		if m == nil || m[0] != 0 {
			continue
		}
		out = append(out, feignClientDecl{
			url:       url,
			ifaceName: content[decl+m[2] : decl+m[3]],
			bodyStart: decl + m[1],
		})
	}
	return out
}

// javaFindMatchingCloseString walks forward from `open` (a '(') and returns the
// index of the matching ')', honouring single/double-quoted string literals so
// a ')' inside a string does not affect the depth count. Returns -1 when
// unbalanced.
func javaFindMatchingCloseString(s string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
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

// javaSkipAnnotationsToDecl returns the offset of the first non-whitespace,
// non-comment, non-annotation token at or after `from`. Annotations (including
// a balanced, string-aware `(...)` argument list) and whitespace/comments are
// skipped. Returns -1 if EOF is reached first.
func javaSkipAnnotationsToDecl(s string, from int) int {
	i := from
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			// line comment
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			// block comment
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
		case c == '@':
			// annotation name
			i++
			for i < len(s) && (isJavaIdentChar(s[i]) || s[i] == '.') {
				i++
			}
			// skip whitespace before optional arg list
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
				i++
			}
			if i < len(s) && s[i] == '(' {
				closeAt := javaFindMatchingCloseString(s, i)
				if closeAt < 0 {
					return -1
				}
				i = closeAt + 1
			}
		default:
			return i
		}
	}
	return -1
}

// isJavaIdentChar reports whether c is valid in a Java identifier.
func isJavaIdentChar(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// javaSpringMappingAnnotationRe matches Spring MVC shortcut mapping annotations
// used on Feign interface methods.
// Group 1: verb capitalization (Get/Post/Put/Delete/Patch).
// Group 2: path string.
var javaSpringMappingAnnotationRe = regexp.MustCompile(
	`@(Get|Post|Put|Delete|Patch)Mapping\s*\(\s*(?:value\s*=\s*)?"([^"\n\r]*)"\s*\)`,
)

// javaRequestMappingVerbRe matches @RequestMapping with method= and value=.
// Group 1: verb (e.g. GET). Group 2: path string (may be empty).
var javaRequestMappingVerbRe = regexp.MustCompile(
	`@RequestMapping\s*\([^)]*method\s*=\s*RequestMethod\.([A-Z]+)(?:[^)]*value\s*=\s*"([^"\n\r]*)")?[^)]*\)`,
)

// synthesizeFeignClient scans `content` for @FeignClient interfaces and
// emits FETCHES for each call site found in the consuming class.
// When the global cross-file DI registry (#845) has @FeignClient entries, the
// pass also runs for consumer files that don't define any @FeignClient interface
// themselves (they may reference one by type name alone).
func synthesizeFeignClient(content string, emit javaClientEmitFn) {
	if !strings.Contains(content, "FeignClient") {
		// Skip fast when neither a local interface nor a cross-file consumer
		// pattern is plausible. We still run when the global registry is
		// non-empty because the consumer may inject by type name only.
		javaDIGlobalMu.RLock()
		hasCrossFile := len(javaDIGlobal) > 0
		javaDIGlobalMu.RUnlock()
		if !hasCrossFile {
			return
		}
	}
	methods := indexJavaEnclosingMethods(content)

	// ---- Pass 1: parse @FeignClient interface definitions ----
	registry := map[string]map[string]restClientMethodEntry{}

	for _, decl := range parseFeignClients(content) {
		body := javaFindInterfaceBody(content, decl.bodyStart)
		if body == "" {
			continue
		}
		methodMap := parseFeignInterfaceMethods(body, decl.url)
		registry[decl.ifaceName] = methodMap
	}

	// ---- Pass 2: find Feign client field references and call sites ----
	// Feign clients are injected as plain Spring beans (@Autowired / constructor).
	// We scan for field declarations that reference a known interface type name,
	// checking both the local (same-file) registry and the global cross-file DI
	// registry (#845 Option B).
	fieldToIface := map[string]string{}

	// Combine local registry keys with cross-file registry keys so we can match
	// field declarations for interfaces defined in other files.
	allIfaceNames := make(map[string]struct{}, len(registry))
	for ifaceName := range registry {
		allIfaceNames[ifaceName] = struct{}{}
	}
	javaDIGlobalMu.RLock()
	for ifaceName := range javaDIGlobal {
		allIfaceNames[ifaceName] = struct{}{}
	}
	javaDIGlobalMu.RUnlock()

	for ifaceName := range allIfaceNames {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(ifaceName) + `\s+(\w+)\s*[;=({,]`)
		for _, mm := range re.FindAllStringSubmatch(content, -1) {
			if len(mm) < 2 {
				continue
			}
			fieldToIface[mm[1]] = ifaceName
		}
	}

	if len(fieldToIface) == 0 {
		return
	}

	for _, callMatch := range javaRestClientCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(callMatch) < 6 {
			continue
		}
		fieldName := content[callMatch[2]:callMatch[3]]
		methodName := content[callMatch[4]:callMatch[5]]

		ifaceName, ok := fieldToIface[fieldName]
		if !ok {
			continue
		}
		// Look up method entry in local registry first, then cross-file registry.
		methodMap, ok := lookupDIRegistry(ifaceName, registry)
		if !ok {
			continue
		}
		entry, ok := methodMap[methodName]
		if !ok {
			continue
		}

		caller := enclosingJavaMethodAt(methods, callMatch[0])
		emit(entry.verb, entry.fullPath, "feign_client", "Function", caller, false)
	}
}

// parseFeignInterfaceMethods extracts (method-name → restClientMethodEntry)
// from the body of a @FeignClient interface. Uses Spring MVC shortcut
// annotations (@GetMapping etc.) and @RequestMapping(method=...).
func parseFeignInterfaceMethods(body, baseURL string) map[string]restClientMethodEntry {
	out := map[string]restClientMethodEntry{}

	// Collect all mapping annotation positions.
	type verbPos struct {
		start int
		end   int
		verb  string
		path  string
	}
	var positions []verbPos

	for _, m := range javaSpringMappingAnnotationRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(body[m[2]:m[3]])
		path := body[m[4]:m[5]]
		positions = append(positions, verbPos{start: m[0], end: m[1], verb: verb, path: path})
	}
	for _, m := range javaRequestMappingVerbRe.FindAllStringSubmatchIndex(body, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(body[m[2]:m[3]])
		path := ""
		if m[4] >= 0 {
			path = body[m[4]:m[5]]
		}
		positions = append(positions, verbPos{start: m[0], end: m[1], verb: verb, path: path})
	}

	// For each annotation, find the method name in the window between this
	// annotation end and the next annotation start.
	for i, pos := range positions {
		windowStart := pos.end
		windowEnd := windowStart + 512
		if i+1 < len(positions) {
			if positions[i+1].start > windowStart && positions[i+1].start < windowEnd {
				windowEnd = positions[i+1].start
			}
		}
		if windowEnd > len(body) {
			windowEnd = len(body)
		}
		window := body[windowStart:windowEnd]

		methodName := javaNextMethodNameInWindow(window)
		if methodName == "" {
			continue
		}

		fullPath := composeBaseURL(baseURL, pos.path)
		path, ok := normalizeRawClientPath(fullPath)
		if !ok {
			path = fullPath
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		out[methodName] = restClientMethodEntry{verb: pos.verb, fullPath: canonical}
	}
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func javaHasAnyHTTPClient(content string) bool {
	if strings.Contains(content, "URI.create") ||
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
		strings.Contains(content, "@OPTIONS(") ||
		strings.Contains(content, "RegisterRestClient") ||
		strings.Contains(content, "@RestClient") ||
		strings.Contains(content, "FeignClient") {
		return true
	}
	// When the global cross-file DI registry (#845) is populated, a file that
	// injects a registered interface by type name alone (e.g. @Autowired
	// OrderServiceClient orderClient) may be a DI consumer even without any
	// explicit HTTP-client marker.
	javaDIGlobalMu.RLock()
	hasRegistry := len(javaDIGlobal) > 0
	javaDIGlobalMu.RUnlock()
	return hasRegistry
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
