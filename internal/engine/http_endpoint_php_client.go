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
var phpLaravelHttpRe = regexp.MustCompile(
	`\bHttp\s*(?:::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\([^)]*\)\s*->\s*)?\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`(\$[A-Za-z_][\w]*)` + // group 4: variable url
		`)`,
)

// phpLaravelHttpChainedRe matches the chained form:
// Http::withHeaders([...])->get("/path"), Http::withToken($t)->post("/path")
// Capture groups: 1 = verb, 2 = double-quoted url, 3 = single-quoted url, 4 = variable url.
var phpLaravelHttpChainedRe = regexp.MustCompile(
	`\bHttp\s*::\s*(?:withHeaders|withToken|withBasicAuth|timeout|retry|acceptJson|asJson|asForm|asMultipart|baseUrl|withOptions)\s*\([^)]*\)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
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
// ---------------------------------------------------------------------------

// phpStringVarRe captures PHP variable string assignments:
//
//	$url = "/path";
//	$base_url = "https://api.example.com";
//	$endpoint = '/api/v1/users';
var phpStringVarRe = regexp.MustCompile(
	`(?m)(\$[A-Za-z_][\w]*)\s*=\s*(?:"([^"\n\r]{1,256})"|'([^'\n\r]{1,256})')`,
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
// for simple PHP variable string assignments.
func buildPHPStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
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
	return syms
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

// indexPHPEnclosingFns builds a sorted (offset, name) list for every
// PHP function/method definition in the file.
func indexPHPEnclosingFns(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range phpEnclosingFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingPHPFnAt returns the name of the nearest preceding function
// definition for a call site at pos.
func enclosingPHPFnAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
