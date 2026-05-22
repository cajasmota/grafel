// PHP consumer-side HTTP client synthesis (#721 wave 2c).
//
// Mirrors http_endpoint_ruby_client.go / http_endpoint_csharp_client.go for
// PHP consumer-side HTTP patterns. Emits one synthetic `http_endpoint` entity
// (consumer side) per detected client call site, AND a FETCHES edge from the
// enclosing function/method to that endpoint.
//
// Patterns covered:
//
//   - Guzzle (most popular PHP HTTP client):
//     $client = new Client(); $client->get($url), $client->post($url)
//     $client->request('POST', $url, ['json' => $body])
//     $client->request('GET', '/path')
//
//   - Symfony HttpClient:
//     HttpClient::create()->request('GET', $url)
//     $client->request('POST', $url, ['body' => $body])
//     $response = $client->request('GET', 'https://example.com/api/users')
//
//   - cURL wrappers:
//     curl_init($url); ... curl_setopt($ch, CURLOPT_POST, true); ... curl_exec($ch)
//     We detect curl_init("url") as a GET and promote to POST if CURLOPT_POST
//     or CURLOPT_CUSTOMREQUEST is set within a 1024-byte window.
//
//   - file_get_contents with HTTP URL:
//     file_get_contents("https://api.example.com/path")
//     file_get_contents($url) where $url looks HTTP-like
//
// Beyond-minimum behaviours:
//   - WordPress HTTP API: wp_remote_get($url), wp_remote_post($url, $args)
//   - Laravel HTTP facade: Http::get($url), Http::post($url, $body)
//   - Env-var concat: $client->get(getenv('API_URL') . '/users')
//     → emit with runtime_dynamic=true
//   - All standard HTTP verbs on Guzzle/Symfony: GET, POST, PUT, PATCH, DELETE, HEAD
//
// The enclosing function is identified by scanning for the nearest preceding
// `function <name>(` or `public function <name>(` declaration.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Guzzle: $client->get($url), $client->request('POST', $url, [...])
// ---------------------------------------------------------------------------

// phpGuzzleVerbRe matches `$client->get("url")`, `$client->post("url")`, etc.
// Receiver allow-list: $client, $http, $guzzle, $httpClient.
var phpGuzzleVerbRe = regexp.MustCompile(
	`\$(?:client|http|guzzle|httpClient)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 4: variable url ($url, $endpoint)
		`)`,
)

// phpGuzzleRequestRe matches `$client->request('POST', '/path', [...])` and
// `$client->request("GET", "/path")`.
// Capture groups: 1 = verb (single-quoted), 2 = verb (double-quoted),
// 3 = path (double-quoted), 4 = path (single-quoted), 5 = path (variable).
var phpGuzzleRequestRe = regexp.MustCompile(
	`\$(?:client|http|guzzle|httpClient)\s*->\s*request\s*\(\s*(?:` +
		`'([A-Za-z]+)'` + // group 1: single-quoted verb
		`|` +
		`"([A-Za-z]+)"` + // group 2: double-quoted verb
		`)\s*,\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted path
		`|` +
		`'([^'\n\r]+)'` + // group 4: single-quoted path
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 5: variable path
		`)`,
)

// ---------------------------------------------------------------------------
// Symfony HttpClient: HttpClient::create()->request('GET', $url)
// ---------------------------------------------------------------------------

// phpSymfonyRequestRe matches `HttpClient::create()->request('GET', $url)` and
// the stored-client form `$client->request('POST', $url, [...])`.
// Capture groups: 1 = verb (single-quoted), 2 = verb (double-quoted),
// 3 = path (double-quoted), 4 = path (single-quoted), 5 = path (variable).
var phpSymfonyRequestRe = regexp.MustCompile(
	`(?:HttpClient\s*::\s*create\s*\(\s*\)|(?:\$(?:client|httpClient|symfonyClient)))\s*->\s*request\s*\(\s*(?:` +
		`'([A-Za-z]+)'` + // group 1: single-quoted verb
		`|` +
		`"([A-Za-z]+)"` + // group 2: double-quoted verb
		`)\s*,\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted path
		`|` +
		`'([^'\n\r]+)'` + // group 4: single-quoted path
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 5: variable path
		`)`,
)

// ---------------------------------------------------------------------------
// cURL: curl_init($url) ... curl_exec($ch)
// ---------------------------------------------------------------------------

// phpCurlInitRe matches `curl_init("url")` or `curl_init($url)`.
// Capture groups: 1 = double-quoted url, 2 = single-quoted url, 3 = variable url.
var phpCurlInitRe = regexp.MustCompile(
	`\bcurl_init\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 1: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 2: single-quoted url
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 3: variable url
		`)\s*\)`,
)

// phpCurlPostRe detects if a curl session is configured as POST via
// curl_setopt($ch, CURLOPT_POST, true) or curl_setopt($ch, CURLOPT_CUSTOMREQUEST, "POST").
// Used as a 1024-byte lookahead after curl_init.
var phpCurlPostRe = regexp.MustCompile(
	`\bcurl_setopt\s*\([^,]+,\s*CURLOPT_(?:POST|CUSTOMREQUEST)\s*,\s*(?:true|1|"([A-Za-z]+)"|'([A-Za-z]+)')`,
)

// ---------------------------------------------------------------------------
// file_get_contents with HTTP URL
// ---------------------------------------------------------------------------

// phpFileGetContentsRe matches `file_get_contents("https://...")`.
// Only triggers when the URL starts with http:// or https://.
// Capture groups: 1 = double-quoted url, 2 = single-quoted url.
var phpFileGetContentsRe = regexp.MustCompile(
	`\bfile_get_contents\s*\(\s*(?:` +
		`"(https?://[^"\n\r]+)"` + // group 1: double-quoted http(s) url
		`|` +
		`'(https?://[^'\n\r]+)'` + // group 2: single-quoted http(s) url
		`)`,
)

// ---------------------------------------------------------------------------
// WordPress HTTP API
// ---------------------------------------------------------------------------

// phpWPRemoteRe matches `wp_remote_get($url)`, `wp_remote_post($url, $args)`,
// `wp_remote_request($url, ['method' => 'PUT', ...])`.
// Capture groups: 1 = wp verb (get/post/request), 2 = double-quoted url,
// 3 = single-quoted url, 4 = variable url.
var phpWPRemoteRe = regexp.MustCompile(
	`\bwp_remote_(get|post|request|put|delete|patch|head)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 4: variable url
		`)`,
)

// ---------------------------------------------------------------------------
// Laravel HTTP facade: Http::get($url), Http::post($url, $body)
// ---------------------------------------------------------------------------

// phpLaravelHttpRe matches `Http::get("url")`, `Http::post("url", $data)`,
// and the chained form `Http::withHeaders([...])->get("url")`.
// Capture groups: 1 = verb, 2 = double-quoted url, 3 = single-quoted url, 4 = variable url.
//
// The modifier argument allows one level of nested parentheses
// (e.g. Http::withToken(config('services.erp.token'))->post(...)) via
// (?:[^)(]|\([^)]*\))* — without this, a `config(...)` call inside the
// modifier breaks the `[^)]*` anchor (issue #1466).
var phpLaravelHttpRe = regexp.MustCompile(
	`\bHttp\s*(?:::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\((?:[^)(]|\([^)]*\))*\)\s*->\s*)?\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 4: variable url
		`)`,
)

// phpLaravelHttpChainedRe matches the chained form:
// Http::withHeaders([...])->get("/path"), Http::withToken($t)->post("/path"),
// Http::withToken(config('x'))->post("/path").
// Capture groups: 1 = verb, 2 = double-quoted url, 3 = single-quoted url, 4 = variable url.
//
// (?:[^)(]|\([^)]*\))* allows one level of nested parens in the modifier
// argument so that helpers like config('key') or env('VAR') work (issue #1466).
var phpLaravelHttpChainedRe = regexp.MustCompile(
	`\bHttp\s*::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\((?:[^)(]|\([^)]*\))*\)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 4: variable url
		`)`,
)

// ---------------------------------------------------------------------------
// Env-var concatenation: getenv('API_URL') . '/path'
// ---------------------------------------------------------------------------

// phpGetenvFrag is the fragment matching PHP getenv() or $_ENV[] access.
const phpGetenvFrag = `(?:getenv\s*\([^)]+\)|\$_ENV\s*\[[^\]]+\]|\$_SERVER\s*\[[^\]]+\])`

// phpGuzzleEnvVerbRe matches `$client->get(getenv('X') . '/path')`.
// Capture groups: 1 = verb, 2 = path suffix (double-quoted), 3 = path suffix (single-quoted).
var phpGuzzleEnvVerbRe = regexp.MustCompile(
	`\$(?:client|http|guzzle|httpClient)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*` +
		phpGetenvFrag + `\s*\.\s*(?:"([^"\n\r]*)"|'([^'\n\r]*)')`,
)

// phpLaravelEnvVerbRe matches `Http::get(getenv('X') . '/path')`.
// Capture groups: 1 = verb, 2 = path suffix (double-quoted), 3 = path suffix (single-quoted).
var phpLaravelEnvVerbRe = regexp.MustCompile(
	`\bHttp\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*` +
		phpGetenvFrag + `\s*\.\s*(?:"([^"\n\r]*)"|'([^'\n\r]*)')`,
)

// ---------------------------------------------------------------------------
// PHP variable string table: $url = "/path"; $base = "https://...";
// Also captures runtime-dynamic base URLs: $x = config(...) / env(...)
// ---------------------------------------------------------------------------

// phpStringVarRe captures PHP variable string assignments:
//
//	$url = "/path";
//	$base_url = "https://api.example.com";
//	$endpoint = '/api/v1/users';
var phpStringVarRe = regexp.MustCompile(
	`(?m)(\$[A-Za-z_][\w]*)\s*=\s*(?:"([^"\n\r]{1,256})"|'([^'\n\r]{1,256})')`,
)

// phpConfigVarRe captures PHP variable assignments whose value is a runtime
// config/env call:
//
//	$ordersUrl  = config('services.orders.url');
//	$baseUrl    = env('ORDERS_BASE');
//	$apiBaseUrl = config("services.notifications.url");
//	$erpUrl     = env('ERP_URL', 'http://legacy-erp:8090');   // two-arg form (#1490)
//
// The two-argument env() form is the most common in Laravel (key + fallback
// default). We match everything from the opening `(` up to the closing `)`
// using a non-greedy `[^)]*` so that simple nested parens in the fallback
// value (rare) don't misfire. The matching is permissive — we only care
// whether the call is present (to mark the variable as runtime-dynamic),
// not about extracting the key or default value.
//
// Group 1 = variable name; group 2+ = config key (unused by caller — just
// the presence of the assignment is needed to mark the var as runtime-dynamic).
var phpConfigVarRe = regexp.MustCompile(
	`(?m)(\$[A-Za-z_][\w]*)\s*=\s*(?:config|env)\s*\([^)]{1,512}\)`,
)

// phpInterpLeadingVarRe matches a PHP double-quoted string whose FIRST
// interpolation is a variable: "{$ordersUrl}/orders/{$orderId}".
// It captures:
//   - group 1: the leading variable name (without `${}` delimiters), e.g. "ordersUrl"
//   - group 2: the static path suffix following the first interpolation,
//     e.g. "/orders/" — further `{$x}` segments inside it are converted to
//     OpenAPI {x} placeholders by resolvePHPInterpSuffix.
var phpInterpLeadingVarRe = regexp.MustCompile(
	`^\{\$([A-Za-z_][\w]*)\}([^"'\n\r]*)$`,
)

// phpInnerInterpRe replaces `{$varName}` and `{$obj->prop}` segments within a
// path suffix with the OpenAPI placeholder `{varName}` or `{prop}`.
// Matches:
//   - {$orderId}        → {orderId}
//   - {$this->orderId}  → {orderId}  (last identifier after ->)
var phpInnerInterpRe = regexp.MustCompile(`\{\$([A-Za-z_][\w]*(?:->[A-Za-z_][\w]*)*)\}`)

// phpConcatLeadingVarRe matches PHP concatenation where a variable prefixes
// a string literal, as captured by the URL variable group in client regexes
// AFTER the literal groups have been exhausted. It is applied to the raw
// URL value when it looks like `$varName . "/path"` — this form is
// pre-folded into the symbol table (literal part already extracted) so this
// regex handles the separate concat-detection pass.
//
// This regex operates on the CONTENT around the call site, not on the already-
// extracted raw string. See synthesizePHPClient concat-scan loop below.
//
// Pattern: $var . "suffix" or $var . 'suffix'
var phpConcatLeadingVarRe = regexp.MustCompile(
	`(\$[A-Za-z_][\w]*)\s*\.\s*(?:"([^"\n\r]{0,256})"|'([^'\n\r]{0,256})')`,
)

// phpLaravelHttpInterpRe matches Laravel Http:: calls whose URL argument is a
// double-quoted interpolated PHP string starting with a variable:
//
//	Http::get("{$ordersUrl}/orders/{$orderId}")
//	Http::post("{$notifUrl}/notifications", [...])
//	Http::withToken(config('x'))->get("{$erpUrl}/api/erp/invoices/{$id}")
//
// Capture groups: 1 = verb, 2 = raw interpolated string body (between the
// outer double-quotes, after the leading `{$`).
var phpLaravelHttpInterpRe = regexp.MustCompile(
	`\bHttp\s*(?:::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\((?:[^)(]|\([^)]*\))*\)\s*->\s*)?\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*"\{(\$[A-Za-z_][\w]*[^"]{0,256})`,
)

// phpLaravelHttpChainedInterpRe matches the chained Laravel form with an
// interpolated URL:
//
//	Http::withToken(config('x'))->post("{$erpUrl}/api/erp/invoices")
var phpLaravelHttpChainedInterpRe = regexp.MustCompile(
	`\bHttp\s*::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\((?:[^)(]|\([^)]*\))*\)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*"\{(\$[A-Za-z_][\w]*[^"]{0,256})`,
)

// phpGuzzleVerbInterpRe matches Guzzle verb methods with an interpolated URL:
//
//	$client->get("{$ordersUrl}/orders/{$orderId}")
//	$http->post("{$notifUrl}/api/notifications")
var phpGuzzleVerbInterpRe = regexp.MustCompile(
	`\$(?:client|http|guzzle|httpClient)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*"\{(\$[A-Za-z_][\w]*[^"]{0,256})`,
)

// phpLaravelHttpConcatRe matches Laravel Http:: calls where the URL is a
// variable followed by string concatenation:
//
//	Http::get($ordersUrl . "/orders/" . $id)
//	Http::post($notifUrl . '/notifications', $data)
var phpLaravelHttpConcatRe = regexp.MustCompile(
	`\bHttp\s*(?:::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\((?:[^)(]|\([^)]*\))*\)\s*->\s*)?\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*(\$[A-Za-z_][\w]*)\s*\.`,
)

// phpLaravelHttpChainedConcatRe is the chained form with concatenation:
//
//	Http::withToken(config('x'))->post($erpUrl . "/api/erp/invoices", $data)
var phpLaravelHttpChainedConcatRe = regexp.MustCompile(
	`\bHttp\s*::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\((?:[^)(]|\([^)]*\))*\)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*(\$[A-Za-z_][\w]*)\s*\.`,
)

// phpGuzzleVerbConcatRe matches Guzzle verb methods with concatenation:
//
//	$client->get($ordersUrl . "/orders/" . $id)
var phpGuzzleVerbConcatRe = regexp.MustCompile(
	`\$(?:client|http|guzzle|httpClient)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*(\$[A-Za-z_][\w]*)\s*\.`,
)

// ---------------------------------------------------------------------------
// Enclosing function index
// ---------------------------------------------------------------------------

// phpEnclosingFnRe captures PHP function/method definitions:
//
//	function foo(
//	public function foo(
//	private function foo(
//	protected function foo(
//	public static function foo(
//	abstract public function foo(
var phpEnclosingFnRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:abstract|final|public|protected|private|static|\s)+\s+)?function\s+([A-Za-z_]\w*)\s*\(`,
)

// phpClassDeclRe captures PHP class declarations so we can qualify method
// names in the same way the PHP tree-sitter extractor does
// (Name = "ClassName.methodName"). Without this, the synthesizer emits
// source_caller="Function:methodName" but the resolver looks for
// "SCOPE.Operation:ClassName.methodName" — the name mismatch means
// caller_resolved stays 0 for all class-based PHP consumers. (#1490)
//
// We only need the class NAME and its start offset; the class body
// extends from the `{` after the declaration to the matching `}`.
// We approximate class body end with the next top-level class
// declaration or end-of-file, which is accurate for the single-class-
// per-file convention used in Laravel/Symfony services.
var phpClassDeclRe = regexp.MustCompile(
	`(?m)^(?:(?:abstract|final|readonly)\s+)*class\s+([A-Za-z_]\w*)`,
)

// phpClassSpan records the source-offset range of a class body and its name.
// Used by indexPHPEnclosingFns to produce class-qualified method names.
type phpClassSpan struct {
	start int    // byte offset of the `class` keyword
	end   int    // byte offset just past the class body (exclusive)
	name  string // bare class name
}

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// phpClientEmitFn is the runtime-aware emitter type for PHP clients.
type phpClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizePHPClient is the package-level entry point referenced from
// applyHTTPEndpointSynthesis.
func synthesizePHPClient(content string, emit emitFn) {
	synthesizePHPClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizePHPClientWithRuntime runs the full PHP client scan.
func synthesizePHPClientWithRuntime(content string, emit phpClientEmitFn) {
	if !phpHasAnyHTTPClient(content) {
		return
	}
	funcs := indexPHPEnclosingFns(content)
	syms := buildPHPStringSymbolTable(content)

	// ----- Guzzle instance: $client->get($url), $client->post($url) -----
	for _, m := range phpGuzzleVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := phpPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "guzzle", "Function", caller, false)
	}

	// ----- Guzzle request: $client->request('POST', $url, [...]) -----
	for _, m := range phpGuzzleRequestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verb := phpPickVerb(content, m, 2)
		raw := phpPickURLArg(content, m, 6, syms)
		if verb == "" || raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "guzzle", "Function", caller, false)
	}

	// ----- Symfony HttpClient: HttpClient::create()->request('GET', $url) -----
	for _, m := range phpSymfonyRequestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verb := phpPickVerb(content, m, 2)
		raw := phpPickURLArg(content, m, 6, syms)
		if verb == "" || raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "symfony_http", "Function", caller, false)
	}

	// ----- cURL: curl_init("url") -----
	for _, m := range phpCurlInitRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		// Extract URL from groups 1 (double), 2 (single), 3 (variable).
		raw := ""
		if m[2] >= 0 {
			raw = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			raw = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			varName := content[m[6]:m[7]]
			if val, ok := syms[varName]; ok {
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

		// Determine verb: scan 1024 bytes forward for CURLOPT_POST or CURLOPT_CUSTOMREQUEST.
		verb := "GET"
		end := m[1] + 1024
		if end > len(content) {
			end = len(content)
		}
		window := content[m[1]:end]
		if pm := phpCurlPostRe.FindStringSubmatch(window); pm != nil {
			if pm[1] != "" {
				verb = strings.ToUpper(pm[1])
			} else if pm[2] != "" {
				verb = strings.ToUpper(pm[2])
			} else {
				verb = "POST"
			}
		}

		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "curl", "Function", caller, false)
	}

	// ----- file_get_contents("https://...") -----
	for _, m := range phpFileGetContentsRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
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
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit("GET", canonical, "file_get_contents", "Function", caller, false)
	}

	// ----- WordPress: wp_remote_get($url), wp_remote_post($url) -----
	for _, m := range phpWPRemoteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		wpVerb := content[m[2]:m[3]]
		// wp_remote_request gets verb from args; default to GET for request form.
		verb := "GET"
		switch wpVerb {
		case "get":
			verb = "GET"
		case "post":
			verb = "POST"
		case "put":
			verb = "PUT"
		case "delete":
			verb = "DELETE"
		case "patch":
			verb = "PATCH"
		case "head":
			verb = "HEAD"
		case "request":
			verb = "GET" // default; a later phase could inspect the $args array
		}
		raw := phpPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "wp_remote", "Function", caller, false)
	}

	// ----- Laravel Http facade: Http::get($url), Http::post($url) -----
	for _, m := range phpLaravelHttpRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := phpPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "laravel_http", "Function", caller, false)
	}

	// ----- Laravel Http chained: Http::withHeaders([...])->get("/path") -----
	for _, m := range phpLaravelHttpChainedRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := phpPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "laravel_http", "Function", caller, false)
	}

	// ----- Env-var concat: $client->get(getenv('X') . '/path') -----
	for _, m := range phpGuzzleEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := phpPickEnvSuffix(content, m, 4)
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "guzzle", "Function", caller, true)
	}

	// ----- Env-var concat: Http::get(getenv('X') . '/path') -----
	for _, m := range phpLaravelEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := phpPickEnvSuffix(content, m, 4)
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "laravel_http", "Function", caller, true)
	}

	// ----- PHP string interpolation: Http::get("{$ordersUrl}/orders/{$id}") -----
	for _, m := range phpLaravelHttpInterpRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]] // e.g. "$ordersUrl}/orders/{$orderId}"
		path := resolvePHPInterpURL(raw, syms)
		if path == "" {
			continue
		}
		normed, ok := normalizeRawClientPath(path)
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, normed)
		emit(verb, canonical, "laravel_http", "Function", caller, true)
	}

	// ----- PHP string interpolation (chained): Http::withToken()->get("{$url}/path") -----
	for _, m := range phpLaravelHttpChainedInterpRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		path := resolvePHPInterpURL(raw, syms)
		if path == "" {
			continue
		}
		normed, ok := normalizeRawClientPath(path)
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, normed)
		emit(verb, canonical, "laravel_http", "Function", caller, true)
	}

	// ----- PHP string interpolation: $client->get("{$ordersUrl}/orders/{$id}") -----
	for _, m := range phpGuzzleVerbInterpRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		path := resolvePHPInterpURL(raw, syms)
		if path == "" {
			continue
		}
		normed, ok := normalizeRawClientPath(path)
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, normed)
		emit(verb, canonical, "guzzle", "Function", caller, true)
	}

	// ----- PHP variable concat: Http::get($ordersUrl . "/orders/" . $id) -----
	for _, m := range phpLaravelHttpConcatRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		varName := content[m[4]:m[5]]
		path := resolvePHPConcatURL(content, m[1], varName, syms)
		if path == "" {
			continue
		}
		normed, ok := normalizeRawClientPath(path)
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, normed)
		emit(verb, canonical, "laravel_http", "Function", caller, true)
	}

	// ----- PHP variable concat (chained): Http::withToken()->post($url . "/path") -----
	for _, m := range phpLaravelHttpChainedConcatRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		varName := content[m[4]:m[5]]
		path := resolvePHPConcatURL(content, m[1], varName, syms)
		if path == "" {
			continue
		}
		normed, ok := normalizeRawClientPath(path)
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, normed)
		emit(verb, canonical, "laravel_http", "Function", caller, true)
	}

	// ----- PHP variable concat: $client->get($ordersUrl . "/orders/" . $id) -----
	for _, m := range phpGuzzleVerbConcatRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		varName := content[m[4]:m[5]]
		path := resolvePHPConcatURL(content, m[1], varName, syms)
		if path == "" {
			continue
		}
		normed, ok := normalizeRawClientPath(path)
		if !ok {
			continue
		}
		caller := enclosingPHPFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, normed)
		emit(verb, canonical, "guzzle", "Function", caller, true)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func phpHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "GuzzleHttp") ||
		strings.Contains(content, "->get(") ||
		strings.Contains(content, "->post(") ||
		strings.Contains(content, "->request(") ||
		strings.Contains(content, "HttpClient") ||
		strings.Contains(content, "curl_init") ||
		strings.Contains(content, "file_get_contents") ||
		strings.Contains(content, "wp_remote_get") ||
		strings.Contains(content, "wp_remote_post") ||
		strings.Contains(content, "wp_remote_request") ||
		strings.Contains(content, "Http::get") ||
		strings.Contains(content, "Http::post") ||
		strings.Contains(content, "Http::put") ||
		strings.Contains(content, "Http::delete") ||
		strings.Contains(content, "Http::patch") ||
		strings.Contains(content, "Http::withHeaders") ||
		strings.Contains(content, "Http::withToken")
}

// buildPHPStringSymbolTable returns a map from $variable → string value
// for simple PHP variable string assignments and config/env calls.
//
// Config/env calls ($x = config(...) / $x = env(...)) are stored with the
// sentinel value phpRuntimeDynamicSentinel so that interpolation/concat
// resolvers know the variable is a runtime-dynamic base URL.
func buildPHPStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	// Literal string assignments: $url = "http://..." or $url = '/path'
	for _, m := range phpStringVarRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		if m[2] < 0 {
			continue
		}
		name := content[m[2]:m[3]]
		val := ""
		if m[4] >= 0 {
			val = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			val = content[m[6]:m[7]]
		}
		if _, dup := syms[name]; !dup {
			syms[name] = val
		}
	}
	// config(...) / env(...) assignments — runtime-dynamic base URLs (#1473).
	for _, m := range phpConfigVarRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		name := content[m[2]:m[3]]
		if _, dup := syms[name]; !dup {
			syms[name] = phpRuntimeDynamicSentinel
		}
	}
	return syms
}

// phpRuntimeDynamicSentinel is stored in the symbol table for variables whose
// value comes from config(...) or env(...). It signals that the variable is a
// runtime-dynamic base URL prefix, not a literal path component.
const phpRuntimeDynamicSentinel = "\x00runtime_dynamic\x00"

// resolvePHPInterpURL resolves a PHP double-quoted interpolated string body
// that starts immediately after the opening `"` and the leading `{$`.
//
// Input raw is the content AFTER the regex has consumed `Http::get("{`, so it
// looks like: `$ordersUrl}/orders/{$orderId}"` (the closing `"` may or may
// not be present — we stop at the first `"`).
//
// Resolution:
//  1. Recover the full body: prepend `{$`, take up to (not including) `"`.
//  2. Match phpInterpLeadingVarRe: leading variable + static suffix.
//  3. If the leading variable is runtime-dynamic (config/env assigned), return
//     the static suffix with `{$inner}` → `{inner}` expansion.
//  4. If the leading variable has a literal value in the symbol table, try to
//     resolve via normalizeRawClientPath (returns a host-stripped path).
func resolvePHPInterpURL(raw string, syms map[string]string) string {
	// raw starts after `"{` — prepend `{` to reconstruct the interpolation
	body := "{" + raw
	// Trim at first double-quote (end of the string literal).
	if idx := strings.Index(body, `"`); idx >= 0 {
		body = body[:idx]
	}
	// Also strip trailing `)` or `,` that may appear if the closing quote was
	// not in the regex capture window.
	body = strings.TrimRight(body, ");, \t\n\r")

	m := phpInterpLeadingVarRe.FindStringSubmatch(body)
	if len(m) < 3 {
		return ""
	}
	varName := "$" + m[1]
	suffix := m[2] // static path fragment, may contain more {$x} segments

	val, known := syms[varName]
	if !known {
		// Unknown variable — cannot resolve.
		return ""
	}

	if val == phpRuntimeDynamicSentinel {
		// Variable is runtime-dynamic (config/env). Extract the path suffix.
		path := phpExpandInnerInterp(suffix)
		return path
	}

	// Literal base URL: strip host, then append suffix with inner params expanded.
	base, ok := normalizeRawClientPath(val)
	if !ok {
		// Probably a relative path; use it directly and append suffix.
		base = val
	}
	path := base + phpExpandInnerInterp(suffix)
	return path
}

// resolvePHPConcatURL resolves a PHP variable-concatenation URL like:
//
//	$ordersUrl . "/orders/" . $orderId
//
// contentAfterDot is the content immediately after the first `.` operator.
// We scan forward to collect all consecutive string-literal and variable
// segments in the concat chain and assemble the static path.
//
// varName is the leading variable (already past the opening `$var .` part
// captured by phpLaravelHttpConcatRe). We look up its value in syms and then
// scan forward from afterDotOffset in content for more segments.
func resolvePHPConcatURL(content string, afterDotOffset int, varName string, syms map[string]string) string {
	val, known := syms[varName]
	if !known {
		return ""
	}

	// Only runtime-dynamic and literal-path bases are handled.
	// For literal bases that start with http(s)://, strip the host.
	var base string
	isDynamic := val == phpRuntimeDynamicSentinel
	if isDynamic {
		base = ""
	} else {
		stripped, ok := normalizeRawClientPath(val)
		if !ok {
			base = val // may be relative path
		} else {
			base = stripped
		}
	}

	// Scan forward from afterDotOffset to collect path segments.
	window := content[afterDotOffset:]
	if len(window) > 512 {
		window = window[:512]
	}
	path := base + phpCollectConcatSuffix(window)
	return path
}

// phpCollectConcatSuffix scans a PHP concat expression tail, collecting
// string literals and converting `$var` segments to `{var}` OpenAPI
// path-param placeholders.
//
// window starts immediately after the first `.` operator.
// Example window: ` "/orders/" . $orderId . "/status")`
// Returns:        `/orders/{orderId}/status`
func phpCollectConcatSuffix(window string) string {
	var buf strings.Builder
	rest := strings.TrimLeft(window, " \t")

	// phpConcatSegRe matches one segment: a string literal or a variable.
	// We process iteratively.
	segRe := regexp.MustCompile(
		`^(?:` +
			`"([^"\n\r]{0,256})"` + // group 1: double-quoted literal
			`|` +
			`'([^'\n\r]{0,256})'` + // group 2: single-quoted literal
			`|` +
			`(\$[A-Za-z_][\w]*)` + // group 3: variable
			`)`,
	)
	dotRe := regexp.MustCompile(`^\s*\.\s*`)

	for rest != "" {
		m := segRe.FindStringSubmatch(rest)
		if m == nil {
			break
		}
		if m[1] != "" {
			buf.WriteString(m[1])
		} else if m[2] != "" {
			buf.WriteString(m[2])
		} else if m[3] != "" {
			// $varName → {varName}
			vn := m[3][1:] // strip leading $
			buf.WriteString("{")
			buf.WriteString(vn)
			buf.WriteString("}")
		}
		rest = rest[len(m[0]):]
		// Consume optional `.` separator.
		dm := dotRe.FindString(rest)
		if dm == "" {
			break
		}
		rest = rest[len(dm):]
	}
	return buf.String()
}

// phpExpandInnerInterp replaces `{$varName}` and `{$this->prop}` segments in a
// path suffix with the OpenAPI path-parameter form `{varName}` / `{prop}`.
// For property accesses like `{$this->orderId}` the last segment after `->` is used.
func phpExpandInnerInterp(suffix string) string {
	return phpInnerInterpRe.ReplaceAllStringFunc(suffix, func(match string) string {
		inner := phpInnerInterpRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return "{param}"
		}
		expr := inner[1]
		// Take the last identifier segment after `->` (e.g. "this->orderId" → "orderId").
		if idx := strings.LastIndex(expr, "->"); idx >= 0 {
			expr = expr[idx+2:]
		}
		if expr == "" {
			return "{param}"
		}
		return "{" + expr + "}"
	})
}

// phpPickURLArg extracts the URL string from a match's double-quoted /
// single-quoted / variable group triple. litStart is the index within m
// of the first literal group.
func phpPickURLArg(content string, m []int, litStart int, syms map[string]string) string {
	// Double-quoted literal.
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	// Single-quoted literal.
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		return content[m[litStart+2]:m[litStart+3]]
	}
	// PHP variable ($url, $endpoint) — resolve via symbol table.
	if litStart+5 < len(m) && m[litStart+4] >= 0 {
		varName := content[m[litStart+4]:m[litStart+5]]
		if val, ok := syms[varName]; ok {
			return val
		}
	}
	return ""
}

// phpPickVerb extracts the HTTP verb from a request() call match.
// Groups at litStart (single-quoted) and litStart+2 (double-quoted) are checked.
func phpPickVerb(content string, m []int, litStart int) string {
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return strings.ToUpper(content[m[litStart]:m[litStart+1]])
	}
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		return strings.ToUpper(content[m[litStart+2]:m[litStart+3]])
	}
	return ""
}

// phpPickEnvSuffix extracts the path suffix from an env-var concat match.
// Groups at litStart (double-quoted) and litStart+2 (single-quoted).
func phpPickEnvSuffix(content string, m []int, litStart int) string {
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		return content[m[litStart+2]:m[litStart+3]]
	}
	return ""
}

// indexPHPClassSpans builds a sorted slice of phpClassSpan for every class
// declaration in content. The span.end is determined by brace-counting from
// the opening `{` of the class body to its matching `}`, so file-scope
// functions after the closing brace are NOT included in the class span.
func indexPHPClassSpans(content string) []phpClassSpan {
	ms := phpClassDeclRe.FindAllStringSubmatchIndex(content, -1)
	if len(ms) == 0 {
		return nil
	}
	out := make([]phpClassSpan, 0, len(ms))
	for _, m := range ms {
		if len(m) < 4 {
			continue
		}
		end := phpClassBodyEnd(content, m[0])
		out = append(out, phpClassSpan{
			start: m[0],
			end:   end,
			name:  content[m[2]:m[3]],
		})
	}
	return out
}

// phpClassBodyEnd scans content from classStart (the byte offset of the
// `class` keyword) and returns the byte offset just past the closing `}` of
// the class body. Brace depth handles nested classes, method bodies, and
// anonymous function literals. String literals are skipped so braces inside
// PHP string values don't affect the depth counter.
func phpClassBodyEnd(content string, classStart int) int {
	// Locate the first `{` that opens the class body.
	openIdx := strings.IndexByte(content[classStart:], '{')
	if openIdx < 0 {
		return len(content)
	}
	pos := classStart + openIdx + 1
	depth := 1
	for pos < len(content) && depth > 0 {
		ch := content[pos]
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
		case '"', '\'':
			// Skip the string body so braces inside strings are ignored.
			pos++
			for pos < len(content) {
				c := content[pos]
				if c == ch {
					break
				}
				if c == '\\' {
					pos++ // skip escaped character
				}
				pos++
			}
		}
		pos++
	}
	return pos
}

// phpEnclosingClassAt returns the class name for a call-site at pos, or ""
// when the call is at file scope (outside any class body).
func phpEnclosingClassAt(classes []phpClassSpan, pos int) string {
	for i := len(classes) - 1; i >= 0; i-- {
		c := classes[i]
		if pos >= c.start && pos < c.end {
			return c.name
		}
	}
	return ""
}

// indexPHPEnclosingFns builds a sorted (offset, qualifiedName) list for every
// PHP function/method definition in the file.
//
// When the function is a method inside a class body the returned name is
// "ClassName.methodName" — exactly the form produced by the PHP tree-sitter
// extractor (internal/extractors/php/php.go, line "rec.Name = parentClass +
// "." + bareName"). This makes the source_caller ref shape match the real
// entity name so the http-endpoint-resolve pass can successfully emit the
// FETCHES edge (caller_resolved > 0). Fix for #1490.
func indexPHPEnclosingFns(content string) []jsFuncSpan {
	classes := indexPHPClassSpans(content)
	var out []jsFuncSpan
	for _, m := range phpEnclosingFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		bareName := content[m[2]:m[3]]
		name := bareName
		if cls := phpEnclosingClassAt(classes, m[0]); cls != "" {
			name = cls + "." + bareName
		}
		out = append(out, jsFuncSpan{offset: m[0], name: name})
	}
	return out
}

// enclosingPHPFnAt returns the name of the nearest preceding function
// definition for a call site at pos. For methods inside a class the returned
// name is class-qualified ("ClassName.method") to match entity names emitted
// by the PHP tree-sitter extractor.
func enclosingPHPFnAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
