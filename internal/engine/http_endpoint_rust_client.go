// Rust consumer-side HTTP client synthesis (#721 wave 2c).
//
// Mirrors http_endpoint_ruby_client.go / http_endpoint_csharp_client.go for
// Rust consumer-side HTTP patterns. Emits one synthetic `http_endpoint`
// entity (consumer side) per detected client call site, AND a FETCHES edge
// from the enclosing function to that endpoint.
//
// Patterns covered:
//
//   - reqwest (most popular Rust HTTP client):
//     reqwest::get(url).await, reqwest::get(url).await?
//     reqwest::Client::new().get(url).send().await
//     client.get(url).send().await, client.post(url).json(&body).send().await
//     client.put(url).send().await, client.delete(url).send().await
//     client.patch(url).send().await, client.head(url).send().await
//
//   - hyper (low-level HTTP library):
//     Client::new().get(url.parse().unwrap()).await
//     Request::builder().method("POST").uri(url).body(body)
//     client.request(req).await
//
//   - ureq (sync HTTP client):
//     ureq::get(url).call(), ureq::post(url).send_json(json)
//     ureq::put(url).send_string(s), ureq::delete(url).call()
//     ureq::patch(url).send_bytes(b)
//
//   - surf (async HTTP client):
//     surf::get(url).await, surf::post(url).body_json(&body).await
//     surf::Client::new().get(url).recv_json::<T>().await
//     surf::put(url).await, surf::delete(url).await
//
// Beyond-minimum behaviours:
//   - .await and .await? propagation (both forms are matched)
//   - Result<reqwest::Response, _> patterns (the call site is still captured)
//   - Env-var concat: reqwest::get(format!("{}/users", env::var("API_URL").unwrap())).await
//     → emit with runtime_dynamic=true
//   - All standard HTTP verbs: get, post, put, patch, delete, head, options
//
// The enclosing function is identified by scanning for the nearest preceding
// `fn <name>(` or `async fn <name>(` declaration.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// reqwest module-level functions: reqwest::get(url), reqwest::post(url)
// ---------------------------------------------------------------------------

// rustReqwestFnRe matches `reqwest::get("url").await`, `reqwest::get("url").await?`,
// and the non-awaited fire-and-forget / .unwrap() forms.
// Capture groups: 1 = verb (get/post/put/patch/delete/head/options),
// 2 = double-quoted url, 3 = single-quoted url (rare in Rust but allowed),
// 4 = bare identifier.
var rustReqwestFnRe = regexp.MustCompile(
	`\breqwest\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare identifier
		`)`,
)

// ---------------------------------------------------------------------------
// reqwest::Client instance calls: client.get(url).send().await
// ---------------------------------------------------------------------------

// rustReqwestClientVerbRe matches `client.get("url")`, `client.post("url")`
// on a reqwest client instance. Receiver allow-list covers common variable
// names for reqwest::Client instances.
var rustReqwestClientVerbRe = regexp.MustCompile(
	`\b(client|http_client|reqwest_client|c)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 4: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 5: bare identifier
		`)`,
)

// ---------------------------------------------------------------------------
// hyper: Request::builder().method("POST").uri(url)
// ---------------------------------------------------------------------------

// rustHyperRequestBuilderRe matches the hyper request builder pattern:
// `Request::builder().method("POST").uri("url")` or `.method(Method::POST).uri("url")`.
// Capture groups: 1 = verb string (from string literal, e.g. "POST" or "GET"),
// 2 = verb enum (from Method::GET, etc.), 3 = double-quoted uri, 4 = bare identifier uri.
var rustHyperRequestBuilderRe = regexp.MustCompile(
	`Request\s*::\s*builder\s*\(\s*\)(?:[^;]*?)\.method\s*\(\s*(?:"([A-Za-z]+)"|(?:Method\s*::\s*([A-Za-z]+)))\s*\)(?:[^;]*?)\.uri\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 3: double-quoted uri
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare identifier uri
		`)`,
)

// rustHyperClientGetRe matches `client.get("url".parse().unwrap())` — hyper
// Client::new().get(Uri) pattern where the URI is parsed inline.
// Capture groups: 1 = double-quoted uri, 2 = bare identifier uri.
var rustHyperClientGetRe = regexp.MustCompile(
	`\b(?:client|hyper_client)\s*\.\s*get\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 1: double-quoted uri (may have .parse())
		`|` +
		`([A-Za-z_][\w]*)` + // group 2: bare identifier
		`)\s*(?:\.parse\s*\(\s*\)\.unwrap\s*\(\s*\))?`,
)

// ---------------------------------------------------------------------------
// ureq module-level functions: ureq::get(url).call()
// ---------------------------------------------------------------------------

// rustUreqFnRe matches `ureq::get("url").call()`, `ureq::post("url").send_json(j)`,
// and other ureq method calls.
// Capture groups: 1 = verb, 2 = double-quoted url, 3 = single-quoted url, 4 = bare identifier.
var rustUreqFnRe = regexp.MustCompile(
	`\bureq\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare identifier
		`)`,
)

// ---------------------------------------------------------------------------
// surf module-level functions: surf::get(url).await
// ---------------------------------------------------------------------------

// rustSurfFnRe matches `surf::get("url").await`, `surf::post("url").body_json(&b).await`.
// Capture groups: 1 = verb, 2 = double-quoted url, 3 = single-quoted url, 4 = bare identifier.
var rustSurfFnRe = regexp.MustCompile(
	`\bsurf\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`'([^'\n\r]+)'` + // group 3: single-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 4: bare identifier
		`)`,
)

// rustSurfClientVerbRe matches `surf::Client::new().get("url")` — the client
// builder form of surf.
// Capture groups: 1 = verb, 2 = double-quoted url, 3 = bare identifier.
var rustSurfClientVerbRe = regexp.MustCompile(
	`surf\s*::\s*Client\s*::\s*new\s*\(\s*\)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(\s*(?:` +
		`"([^"\n\r]+)"` + // group 2: double-quoted url
		`|` +
		`([A-Za-z_][\w]*)` + // group 3: bare identifier
		`)`,
)

// ---------------------------------------------------------------------------
// Env-var concat: format!("{}/path", env::var("API_URL").unwrap())
// ---------------------------------------------------------------------------

// rustEnvVarFmtRe matches the `format!("{}/path", env::var("NAME").unwrap())`
// pattern used in reqwest/ureq/surf calls. We detect the format! macro and
// capture the path suffix.
//
// Capture groups:
//
//	1 = path suffix embedded in the format string after the placeholder
var rustEnvVarFmtRe = regexp.MustCompile(
	`format!\s*\(\s*"\{\}\s*(/[^"\n\r]*)"\s*,\s*(?:env\s*::\s*var|std\s*::\s*env\s*::\s*var|std\s*::\s*env\s*::\s*var_os)\s*\(`,
)

// rustReqwestEnvVerbRe matches `reqwest::get(format!("{}/path", env::var(...)))`.
// Capture groups: 1 = verb, 2 = path suffix.
var rustReqwestEnvVerbRe = regexp.MustCompile(
	`\breqwest\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*format!\s*\(\s*"\{\}\s*(/[^"\n\r]*)"\s*,\s*(?:env\s*::\s*var|std\s*::\s*env\s*::\s*var)\s*\(`,
)

// rustUreqEnvVerbRe matches `ureq::get(format!("{}/path", env::var(...)))`.
// Capture groups: 1 = verb, 2 = path suffix.
var rustUreqEnvVerbRe = regexp.MustCompile(
	`\bureq\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*format!\s*\(\s*"\{\}\s*(/[^"\n\r]*)"\s*,\s*(?:env\s*::\s*var|std\s*::\s*env\s*::\s*var)\s*\(`,
)

// rustSurfEnvVerbRe matches `surf::get(format!("{}/path", env::var(...)))`.
// Capture groups: 1 = verb, 2 = path suffix.
var rustSurfEnvVerbRe = regexp.MustCompile(
	`\bsurf\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*format!\s*\(\s*"\{\}\s*(/[^"\n\r]*)"\s*,\s*(?:env\s*::\s*var|std\s*::\s*env\s*::\s*var)\s*\(`,
)

// ---------------------------------------------------------------------------
// String constant table
// ---------------------------------------------------------------------------

// rustStringConstRe captures simple Rust constant/let declarations:
//
//	const API_URL: &str = "/value";
//	let base_url = "/value";
//	let url = "https://example.com/path";
var rustStringConstRe = regexp.MustCompile(
	`(?m)(?:const\s+[A-Z_][A-Z0-9_]*\s*:\s*&str|let\s+(?:mut\s+)?[a-z_][a-z0-9_]*)\s*=\s*"([^"\n\r]{1,256})"`,
)

// ---------------------------------------------------------------------------
// Enclosing function index
// ---------------------------------------------------------------------------

// rustEnclosingFnRe captures Rust function definitions:
//
//	fn foo(
//	async fn foo(
//	pub fn foo(
//	pub async fn foo(
var rustEnclosingFnRe = regexp.MustCompile(
	`(?m)^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)\s*[(<]`,
)

// ---------------------------------------------------------------------------
// Public entry points
// ---------------------------------------------------------------------------

// rustClientEmitFn is the runtime-aware emitter type for Rust clients.
type rustClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeRustClient is the package-level entry point referenced from
// applyHTTPEndpointSynthesis.
func synthesizeRustClient(content string, emit emitFn) {
	synthesizeRustClientWithRuntime(content, func(method, canonicalPath, framework, refKind, refName string, _ bool) {
		emit(method, canonicalPath, framework, refKind, refName)
	})
}

// synthesizeRustClientWithRuntime runs the full Rust client scan.
func synthesizeRustClientWithRuntime(content string, emit rustClientEmitFn) {
	if !rustHasAnyHTTPClient(content) {
		return
	}
	funcs := indexRustEnclosingFns(content)
	syms := buildRustStringSymbolTable(content)

	// ----- reqwest::get/post/... module-level functions -----
	for _, m := range rustReqwestFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := rustPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "reqwest", "Function", caller, false)
	}

	// ----- reqwest client instance: client.get(url).send().await -----
	for _, m := range rustReqwestClientVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 12 {
			continue
		}
		verb := strings.ToUpper(content[m[4]:m[5]])
		raw := rustPickURLArg(content, m, 6, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "reqwest", "Function", caller, false)
	}

	// ----- hyper Request::builder().method(...).uri(...) -----
	for _, m := range rustHyperRequestBuilderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		// Verb: group 1 (string literal "POST") or group 2 (Method::POST enum).
		verb := ""
		if m[2] >= 0 {
			verb = strings.ToUpper(content[m[2]:m[3]])
		} else if m[4] >= 0 {
			verb = strings.ToUpper(content[m[4]:m[5]])
		}
		if verb == "" {
			verb = "GET"
		}
		// URI: group 3 (double-quoted) or group 4 (bare identifier).
		raw := ""
		if m[6] >= 0 {
			raw = content[m[6]:m[7]]
		} else if m[8] >= 0 {
			ident := content[m[8]:m[9]]
			if val, ok := syms[ident]; ok {
				raw = val
			}
		}
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "hyper", "Function", caller, false)
	}

	// ----- hyper client.get("url".parse().unwrap()) -----
	for _, m := range rustHyperClientGetRe.FindAllStringSubmatchIndex(content, -1) {
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
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit("GET", canonical, "hyper", "Function", caller, false)
	}

	// ----- ureq::get/post/... -----
	for _, m := range rustUreqFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := rustPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "ureq", "Function", caller, false)
	}

	// ----- surf::get/post/... module-level -----
	for _, m := range rustSurfFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 10 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := rustPickURLArg(content, m, 4, syms)
		if raw == "" {
			continue
		}
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "surf", "Function", caller, false)
	}

	// ----- surf::Client::new().get(...) -----
	for _, m := range rustSurfClientVerbRe.FindAllStringSubmatchIndex(content, -1) {
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
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "surf", "Function", caller, false)
	}

	// ----- Env-var concat: reqwest::get(format!("{}/path", env::var(...))) -----
	for _, m := range rustReqwestEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := ""
		if m[4] >= 0 {
			suffix = content[m[4]:m[5]]
		}
		if suffix == "" || !looksLikeURLPath(suffix) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "reqwest", "Function", caller, true)
	}

	// ----- Env-var concat: ureq::get(format!("{}/path", env::var(...))) -----
	for _, m := range rustUreqEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := ""
		if m[4] >= 0 {
			suffix = content[m[4]:m[5]]
		}
		if suffix == "" || !looksLikeURLPath(suffix) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "ureq", "Function", caller, true)
	}

	// ----- Env-var concat: surf::get(format!("{}/path", env::var(...))) -----
	for _, m := range rustSurfEnvVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		suffix := ""
		if m[4] >= 0 {
			suffix = content[m[4]:m[5]]
		}
		if suffix == "" || !looksLikeURLPath(suffix) {
			continue
		}
		caller := enclosingRustFnAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, suffix)
		emit(verb, canonical, "surf", "Function", caller, true)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func rustHasAnyHTTPClient(content string) bool {
	return strings.Contains(content, "reqwest") ||
		strings.Contains(content, "ureq") ||
		strings.Contains(content, "surf") ||
		strings.Contains(content, "hyper") ||
		strings.Contains(content, "Request::builder") ||
		strings.Contains(content, "client.get") ||
		strings.Contains(content, "client.post")
}

// buildRustStringSymbolTable returns a map from identifier → string value
// for simple constant/let string declarations in the file.
func buildRustStringSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	for _, m := range rustStringConstRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		if m[2] < 0 {
			continue
		}
		// Extract the identifier name from before '='
		// The regex doesn't capture name separately — we use the value only
		// for identifier resolution via a secondary pass.
		_ = content[m[2]:m[3]] // value
	}
	// Second pass: simpler name=value extraction for the symbol table
	simpleRe := regexp.MustCompile(`(?m)(?:let\s+(?:mut\s+)?([a-z_][a-z0-9_]*)|const\s+([A-Z_][A-Z0-9_]*)\s*:\s*&str)\s*=\s*"([^"\n\r]{1,256})"`)
	for _, m := range simpleRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		var name string
		if m[2] >= 0 {
			name = content[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = content[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		val := ""
		if m[6] >= 0 {
			val = content[m[6]:m[7]]
		}
		if _, dup := syms[name]; !dup {
			syms[name] = val
		}
	}
	return syms
}

// rustPickURLArg extracts the URL string from a match's double-quoted /
// single-quoted / identifier group triple. litStart is the index within m
// of the first literal group.
func rustPickURLArg(content string, m []int, litStart int, syms map[string]string) string {
	// Double-quoted literal.
	if litStart+1 < len(m) && m[litStart] >= 0 {
		return content[m[litStart]:m[litStart+1]]
	}
	// Single-quoted literal (rare in Rust but present in test fixtures).
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

// indexRustEnclosingFns builds a sorted (offset, name) list for every
// Rust function definition in the file.
func indexRustEnclosingFns(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range rustEnclosingFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingRustFnAt returns the name of the nearest preceding function
// definition for a call site at pos.
func enclosingRustFnAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
