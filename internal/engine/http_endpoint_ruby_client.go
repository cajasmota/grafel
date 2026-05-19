// Ruby consumer-side HTTP client synthesis (#721 wave 2b).
//
// Mirrors http_endpoint_go_client.go / http_endpoint_kotlin_client.go for
// Ruby consumer-side HTTP patterns. Emits one synthetic `http_endpoint`
// entity (consumer side) per detected client call site, AND a FETCHES edge
// from the enclosing function/method to that endpoint.
//
// Patterns covered:
//
//   - Net::HTTP standard library:
//     Net::HTTP.get(URI(url)), Net::HTTP.post(URI(url), data)
//     http = Net::HTTP.new(host, port); http.get(path), http.post(path)
//     Net::HTTP.start(host, port) { |http| http.get(path) } (beyond-minimum)
//
//   - Faraday:
//     Faraday.get(url), Faraday.post(url)
//     conn = Faraday.new(url: ...); conn.get(path), conn.post(path) { |req| ... }
//
//   - HTTParty:
//     HTTParty.get(url), HTTParty.post(url, body: ...)
//     include HTTParty; get(path), post(path) (module-included form)
//
//   - RestClient:
//     RestClient.get(url), RestClient.post(url, payload)
//     RestClient::Resource.new(url).get, RestClient::Resource.new(url).post
//
// Beyond-minimum behaviours:
//   - Net::HTTP.start(host, port) { |http| http.get(path) } — block form
//   - Env-var concatenation: Net::HTTP.get(URI(ENV['API_URL'] + '/users'))
//     → emit with runtime_dynamic=true
//   - All standard HTTP verbs: get, post, put, patch, delete, head, options
//
// The enclosing method/function is identified by scanning for the nearest
// preceding `def <name>` declaration.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Net::HTTP package-level class methods
// ---------------------------------------------------------------------------

// rubyNetHTTPClassVerbRe matches `Net::HTTP.get(URI(url))`,
// `Net::HTTP.post(URI(url), data)`, `Net::HTTP.put_form(uri, data)`.
// The url group accepts string literals in single or double quotes, or a
// bare identifier.
var rubyNetHTTPClassVerbRe = regexp.MustCompile(
	`\bNet::HTTP\s*\.\s*(get|post|put|delete|head|patch|options)\s*\(\s*(?:URI\s*\(\s*)?(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted literal
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted literal
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare identifier
		`)`,
)

// rubyNetHTTPInstanceVerbRe matches instance method calls on a Net::HTTP
// object: `http.get(path)`, `http.post(path, body)`, etc.
// Receiver allow-list: http, conn, client, connection, net_http, nethttp, @http, @client.
var rubyNetHTTPInstanceVerbRe = regexp.MustCompile(
	`\b(@?(?:http|conn|client|connection|net_http|nethttp))\s*\.\s*(get|post|put|delete|head|patch|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted literal
		`|` +
		`'([^'\n\r]+)'` + // group 4: single-quoted literal
		`|` +
		`([A-Za-z_][\w]*)` + // group 5: bare identifier
		`)`,
)

// rubyNetHTTPStartBlockRe matches the block form:
// `Net::HTTP.start(host, port) { |http| http.get(path) }`
// and `Net::HTTP.start(host, port) do |http| http.post(path, body) end`.
// We capture the path from the inner `http.<verb>(path)` call inside the block.
var rubyNetHTTPStartBlockRe = regexp.MustCompile(
	`\bNet::HTTP\s*\.\s*start\s*\([^)]+\)\s*(?:\{|do)\s*\|\s*\w+\s*\|[^}]*?\b(\w+)\s*\.\s*(get|post|put|delete|head|patch|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted path
		`|` +
		`'([^'\n\r]+)'` + // group 4: single-quoted path
		`|` +
		`([A-Za-z_][\w]*)` + // group 5: identifier path
		`)`,
)

// ---------------------------------------------------------------------------
// Faraday
// ---------------------------------------------------------------------------

// rubyFaradayClassVerbRe matches `Faraday.get(url)`, `Faraday.post(url)`, etc.
var rubyFaradayClassVerbRe = regexp.MustCompile(
	`\bFaraday\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// rubyFaradayInstanceVerbRe matches `conn.get(path)`, `conn.post(path) { |req| ... }`.
// Receiver allow-list: conn, connection, faraday, client, @conn, @connection, @client, @faraday.
var rubyFaradayInstanceVerbRe = regexp.MustCompile(
	`\b(@?(?:conn|connection|faraday|client))\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted path
		`|` +
		`'([^'\n\r]+)'` + // group 4: single-quoted path
		`|` +
		`([A-Za-z_][\w]*)` + // group 5: identifier path
		`)`,
)

// ---------------------------------------------------------------------------
// HTTParty
// ---------------------------------------------------------------------------

// rubyHTTPartyClassVerbRe matches `HTTParty.get(url)`, `HTTParty.post(url, body: ...)`.
var rubyHTTPartyClassVerbRe = regexp.MustCompile(
	`\bHTTParty\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// rubyHTTPartyIncludedVerbRe matches the included-module form where HTTParty
// is mixed in and called as a plain method: `get(url)`, `post(url, body: ...)`.
// We require `include HTTParty` in the same file as a guard.
// Capture groups: 1 = verb, 2 = double-quoted url, 3 = single-quoted url,
// 4 = identifier url.
var rubyHTTPartyIncludedVerbRe = regexp.MustCompile(
	`(?:^|[^\w.])(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// ---------------------------------------------------------------------------
// RestClient
// ---------------------------------------------------------------------------

// rubyRestClientClassVerbRe matches `RestClient.get(url)`, `RestClient.post(url, payload)`.
var rubyRestClientClassVerbRe = regexp.MustCompile(
	`\bRestClient\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: identifier url
		`)`,
)

// rubyRestClientResourceVerbRe matches
// `RestClient::Resource.new(url).get` and
// `RestClient::Resource.new(url).post(payload)`.
// We capture the verb from the method call after `.new(url)`.
var rubyRestClientResourceVerbRe = regexp.MustCompile(
	`\bRestClient::Resource\s*\.\s*new\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 1: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 2: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 3: identifier url
		`)\s*\)\s*\.\s*(get|post|put|patch|delete|head|options)`,
)

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// rubyEnvConcatRe matches `ENV['API_URL'] + '/path'` or
// `ENV["API_URL"] + "/path"` as a URL prefix. Used to detect runtime-dynamic
// URLs in Net::HTTP.get / HTTParty.get / RestClient.get / Faraday calls.
//
// Capture groups:
//
//	1 = path suffix (the string concatenated after the env var)
var rubyEnvConcatRe = regexp.MustCompile(
	`ENV\s*\[['"][^'"]+['"]\]\s*\+\s*(?:"([^"\n\r]*)"|'([^'\n\r]*)')`,
)

// rubyNetHTTPEnvVerbRe matches `Net::HTTP.get(URI(ENV['X'] + '/path'))`.
// Capture groups: 1 = verb, 2 = path suffix (double-quoted), 3 = path suffix (single-quoted).
var rubyNetHTTPEnvVerbRe = regexp.MustCompile(
	`\bNet::HTTP\s*\.\s*(get|post|put|delete|head|patch|options)\s*\(\s*(?:URI\s*\(\s*)?ENV\s*\[['"][^'"]+['"]\]\s*\+\s*(?:"([^"\n\r]*)"|'([^'\n\r]*)')`,
)

// rubyHTTPartyEnvVerbRe matches `HTTParty.get(ENV['X'] + '/path')`.
// Capture groups: 1 = verb, 2 = path suffix (double-quoted), 3 = path suffix (single-quoted).
var rubyHTTPartyEnvVerbRe = regexp.MustCompile(
	`\bHTTParty\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*ENV\s*\[['"][^'"]+['"]\]\s*\+\s*(?:"([^"\n\r]*)"|'([^'\n\r]*)')`,
)

// rubyRestClientEnvVerbRe matches `RestClient.get(ENV['X'] + '/path')`.
// Capture groups: 1 = verb, 2 = path suffix (double-quoted), 3 = path suffix (single-quoted).
var rubyRestClientEnvVerbRe = regexp.MustCompile(
	`\bRestClient\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*ENV\s*\[['"][^'"]+['"]\]\s*\+\s*(?:"([^"\n\r]*)"|'([^'\n\r]*)')`,
)

// ---------------------------------------------------------------------------
// String constant table
// ---------------------------------------------------------------------------

// rubyStringConstRe captures simple Ruby constant/variable declarations:
//
//	API_URL = "/value"
//	base_url = "/value"
//	BASE = '/value'
var rubyStringConstRe = regexp.MustCompile(
	`(?m)([A-Z_][A-Z0-9_]*|[a-z_][a-z0-9_]*)\s*=\s*(?:"([^"\n\r]{1,256})"|'([^'\n\r]{1,256})')`,
)

// ---------------------------------------------------------------------------
// Enclosing function index
// ---------------------------------------------------------------------------

// rubyEnclosingMethodRe captures Ruby method definitions:
//
//	def foo(...)
//	def self.foo(...)
//	def initialize(...)
var rubyEnclosingMethodRe = regexp.MustCompile(
	`(?m)^\s*def\s+(?:self\s*\.\s*)?([A-Za-z_]\w*)\s*[(\n]`,
)

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// rubyClientEmitFn is the runtime-aware emitter type for Ruby clients.
type rubyClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeRubyClient is the package-level entry point referenced from
// applyHTTPEndpointSynthesis.
func synthesizeRubyClient(content string, emit emitFn) {
	synthesizeRubyClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizeRubyClientWithRuntime runs the full Ruby client scan.
func synthesizeRubyClientWithRuntime(content string, emit rubyClientEmitFn) {
	if !rubyHasAnyHTTPClient(content) {
		return
	}
	funcs := indexRubyEnclosingMethods(content)
	syms := buildRubyStringSymbolTable(content)
	hasHTTPartyInclude := strings.Contains(content, "include HTTParty")

	// ----- Net::HTTP class-level verbs: Net::HTTP.get/post/... -----
	for _, m := range rubyNetHTTPClassVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := rubyPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "net_http", "Function", caller, false)
	}

	// ----- Net::HTTP instance verbs: http.get(path), conn.post(path) -----
	for _, m := range rubyNetHTTPInstanceVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := rubyPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "net_http", "Function", caller, false)
	}

	// ----- Net::HTTP.start(host, port) { |http| http.get(path) } -----
	for _, m := range rubyNetHTTPStartBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := rubyPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "net_http", "Function", caller, false)
	}

	// ----- Faraday class-level: Faraday.get(url) -----
	for _, m := range rubyFaradayClassVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := rubyPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "faraday", "Function", caller, false)
	}

	// ----- Faraday instance: conn.get(path), conn.post(path) { ... } -----
	for _, m := range rubyFaradayInstanceVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := rubyPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "faraday", "Function", caller, false)
	}

	// ----- HTTParty class-level: HTTParty.get(url) -----
	for _, m := range rubyHTTPartyClassVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := rubyPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "httparty", "Function", caller, false)
	}

	// ----- HTTParty included form: get(url), post(url) (only when include HTTParty present) -----
	if hasHTTPartyInclude {
		for _, m := range rubyHTTPartyIncludedVerbRe.FindAllStringSubmatchIndex(content, -1) {
			if len(m) < 10 {
				continue
			}
			verb := strings.ToUpper(content[m[2]:m[3]])
			raw := rubyPickURLArg(content, m, 4, syms)
			if raw == "" {
				continue
			}
			path, pathOK := normalizeRawClientPath(raw) // #807
			if !pathOK {
				continue
			}
			caller := enclosingRubyMethodAt(funcs, m[0])
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emit(verb, canonical, "httparty", "Function", caller, false)
		}
	}

	// ----- RestClient class-level: RestClient.get(url) -----
	for _, m := range rubyRestClientClassVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := rubyPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "rest_client", "Function", caller, false)
	}

	// ----- RestClient::Resource.new(url).get / .post -----
	for _, m := range rubyRestClientResourceVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		// URL is in groups 1 (double-quoted), 2 (single-quoted), 3 (identifier).
		// Verb is in group 4.
		raw := ""
		if m[2] >= 0 {
			raw = content[m[2]:m[3]]
		} else if m[4] >= 0 {
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
		verb := strings.ToUpper(content[m[8]:m[9]])
		path, ok := normalizeRawClientPath(raw) // #807
		if !ok {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "rest_client", "Function", caller, false)
	}

	// ----- Env-var concat: Net::HTTP.get(URI(ENV['X'] + '/path')) -----
	for _, m := range rubyNetHTTPEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := rubyPickEnvSuffix(content, m, 4)
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "net_http", "Function", caller, true)
	}

	// ----- Env-var concat: HTTParty.get(ENV['X'] + '/path') -----
	for _, m := range rubyHTTPartyEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := rubyPickEnvSuffix(content, m, 4)
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "httparty", "Function", caller, true)
	}

	// ----- Env-var concat: RestClient.get(ENV['X'] + '/path') -----
	for _, m := range rubyRestClientEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := rubyPickEnvSuffix(content, m, 4)
		suffix, suffixOK := normalizeRawClientPath(suffix) // #807
		if !suffixOK {
			continue
		}
		caller := enclosingRubyMethodAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "rest_client", "Function", caller, true)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func rubyHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "Net::HTTP") ||
		strings.Contains(content, "Faraday") ||
		strings.Contains(content, "HTTParty") ||
		strings.Contains(content, "RestClient") ||
		strings.Contains(content, "faraday") ||
		strings.Contains(content, "conn.get") ||
		strings.Contains(content, "conn.post") ||
		strings.Contains(content, "client.get") ||
		strings.Contains(content, "client.post")
}

// buildRubyStringSymbolTable returns a map from identifier → string value
// for simple constant/variable declarations in the file.
func buildRubyStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range rubyStringConstRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		var name, val string
		if m[2] >= 0 {
			name = content[m[2]:m[3]]
		}
		if name == "" {
			continue
		}
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

// rubyPickURLArg extracts the URL string from a match's double-quoted /
// single-quoted / identifier group triple. `litStart` is the index within
// `m` of the first literal group.
func rubyPickURLArg(content string, m []int, litStart int, syms map[string]string) string {
	// Double-quoted literal.
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	// Single-quoted literal.
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

// rubyPickEnvSuffix extracts the path suffix string from an env-var concat
// regex match. Groups at litStart (double-quoted) and litStart+2
// (single-quoted) are checked.
func rubyPickEnvSuffix(content string, m []int, litStart int) string {
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	if litStart+3 < len(m) && m[litStart+2] >= 0 {
		return content[m[litStart+2]:m[litStart+3]]
	}
	return ""
}

// indexRubyEnclosingMethods builds a sorted (offset, name) list for every
// Ruby method definition in the file.
func indexRubyEnclosingMethods(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range rubyEnclosingMethodRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingRubyMethodAt returns the name of the nearest preceding method
// definition for a call site at `pos`.
func enclosingRubyMethodAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
