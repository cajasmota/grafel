// Swift (iOS) consumer-side HTTP client synthesis (#3574, epic #3571).
//
// iOS mobile screens reach their backend through one of two dominant Swift
// HTTP clients:
//
//   - URLSession (Foundation, first-party):
//     URLSession.shared.dataTask(with: URL(string: "https://api.example.com/v1/users"))
//     var request = URLRequest(url: URL(string: "/auth/login")!); request.httpMethod = "POST"
//
//   - Alamofire (https://github.com/Alamofire/Alamofire) — the de-facto 3rd-party lib:
//     AF.request("https://api.example.com/v1/users")
//     AF.request("/auth/login", method: .post)
//     session.request("/users/\(id)", method: .put)
//
// Before this pass an iOS app that called a downstream API produced no
// http_endpoint_call entity and no cross-repo FETCHES edge — the iOS side of
// the cross-link graph was invisible.
//
// This emits one synthetic http_endpoint_call per detected URLSession /
// Alamofire call site (via the shared emitClientRuntime from
// applyHTTPEndpointSynthesis), so the existing cross-repo linker pairs them
// with producer-side route definitions by canonical path Name. The entity shape
// is IDENTICAL to the scala/elixir/kotlin/dart client synthesizers
// (kind=http_endpoint_call, id=http:<VERB>:<path>, FETCHES edge from the
// enclosing function) — the cross-repo join depends on that identical shape.
//
// The URL host is stripped; Swift string-interpolation markers `\(expr)` become
// `{param}` placeholders to match server-route normalisation. The enclosing
// `func name(...)` is attributed as the calling reference.
//
// Verb resolution:
//   - URLSession defaults to GET unless an adjacent `httpMethod = "VERB"`
//     assignment is found within a forward window of the URL site.
//   - Alamofire defaults to GET unless a `method: .verb` argument is present on
//     the same request call.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// swiftURLStringRe matches a `URL(string: "...")` / `URL(string: "...")!`
// constructor whose argument is a string literal. Group 1 = the URL body.
var swiftURLStringRe = regexp.MustCompile(
	`URL\s*\(\s*string\s*:\s*"([^"\n\r]+)"`,
)

// swiftAlamofireRequestRe matches an Alamofire request whose first positional
// argument is a string-literal URL, optionally followed by `method: .verb`:
//
//	AF.request("https://.../path")
//	AF.request("/path", method: .post)
//	session.request("/path", method: .put)
//
// Group 1 = the URL body. The verb is resolved separately by scanning the
// request call's argument list for `method: .<verb>` (swiftAFMethodRe).
var swiftAlamofireRequestRe = regexp.MustCompile(
	`\b(?:AF|session|_session|manager|sessionManager)\s*\.\s*request\s*\(\s*"([^"\n\r]+)"`,
)

// swiftAFMethodRe captures an Alamofire `method: .get` / `.post` argument.
var swiftAFMethodRe = regexp.MustCompile(
	`method\s*:\s*\.\s*(get|post|put|patch|delete|head|options)\b`,
)

// swiftHTTPMethodAssignRe captures `request.httpMethod = "POST"` (URLSession /
// URLRequest verb assignment). Group 1 = the verb string.
var swiftHTTPMethodAssignRe = regexp.MustCompile(
	`\.\s*httpMethod\s*=\s*"([A-Za-z]+)"`,
)

// swiftInterpRe rewrites a Swift string-interpolation marker `\(expr)` into a
// `{param}` placeholder so the canonical path is stable across call sites.
var swiftInterpRe = regexp.MustCompile(`\\\([^)]*\)`)

// swiftEnclosingFuncRe captures Swift function / method declarations for caller
// attribution:
//
//	func fetchUsers() async throws -> [User] { ... }
//	private func login(_ email: String) { ... }
//	@objc func reload() { ... }
var swiftEnclosingFuncRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:@\w+\s+)*(?:(?:public|private|internal|fileprivate|open|static|final|override|class|mutating|nonisolated)\s+)*func\s+([A-Za-z_]\w*)\s*[\(<]`,
)

// swiftClientEmitFn is the runtime-aware emitter type for the Swift client.
type swiftClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeSwiftClientWithRuntime is the package-level entry point referenced
// from applyHTTPEndpointSynthesis. Emits one outbound http_endpoint_call per
// URLSession / Alamofire call site + a FETCHES edge from the enclosing func.
func synthesizeSwiftClientWithRuntime(content string, emit swiftClientEmitFn) {
	// GraphQL client operations (Apollo-iOS `apollo.fetch/perform/subscribe`)
	// emit honest-partial operation references keyed to the server endpoint shape
	// http:GRAPHQL:/graphql/<Root>/<OpName> (#4036). Run BEFORE the REST
	// early-exit guard below, since a pure-Apollo Swift file may contain none of
	// the URLSession / Alamofire markers.
	if strings.Contains(content, "apollo") || strings.Contains(content, "Apollo") {
		synthesizeSwiftGraphQLClient(content, indexSwiftFuncs(content), emit)
	}

	if !strings.Contains(content, "URL(string:") && !strings.Contains(content, "URL(string :") &&
		!strings.Contains(content, ".request(") && !strings.Contains(content, "URLSession") &&
		!strings.Contains(content, "AF.") {
		return
	}

	funcs := indexSwiftFuncs(content)

	doEmit := func(siteOffset int, rawURL, verb, framework string) {
		runtimeDynamic := strings.Contains(rawURL, `\(`)
		rawURL = canonicalizeSwiftInterpolation(rawURL)
		path, ok := normalizeRawClientPath(rawURL)
		if !ok {
			return
		}
		caller := enclosingSwiftFuncAt(funcs, siteOffset)
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, framework, "Function", caller, runtimeDynamic)
	}

	// Alamofire FIRST: `AF.request("...")` does not wrap the URL in
	// URL(string:), so its sites are disjoint from URLSession; ordering only
	// matters for label correctness on the rare overlap.
	for _, m := range swiftAlamofireRequestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		rawURL := content[m[2]:m[3]]
		verb := swiftResolveAlamofireVerb(content, m[1])
		doEmit(m[0], rawURL, verb, "alamofire")
	}

	// URLSession / URLRequest via URL(string: "...").
	urlSites := swiftURLStringRe.FindAllStringSubmatchIndex(content, -1)
	for i, m := range urlSites {
		if len(m) < 4 {
			continue
		}
		rawURL := content[m[2]:m[3]]
		// Clamp the httpMethod search window to the neighbouring URL(string:)
		// sites so a verb assignment belonging to a DIFFERENT request (a later
		// function) can't leak back onto this URL. The configuration of a
		// URLRequest follows its URL construction, so we look forward up to the
		// next URL(string:) site (or 512 bytes, whichever is closer).
		windowEnd := len(content)
		if i+1 < len(urlSites) {
			windowEnd = urlSites[i+1][0]
		}
		verb := swiftResolveURLSessionVerb(content, m[1], windowEnd)
		doEmit(m[0], rawURL, verb, "urlsession")
	}
}

// swiftResolveAlamofireVerb scans the `.request(...)` argument list (bounded by
// the balanced closing paren, capped at 200 bytes) for a `method: .verb`
// argument; defaults to GET. Bounding to the call's own arg list prevents a
// `method:` belonging to a LATER request call from being attributed here.
func swiftResolveAlamofireVerb(content string, pos int) string {
	end := swiftCallArgEnd(content, pos, 200)
	if mm := swiftAFMethodRe.FindStringSubmatch(content[pos:end]); len(mm) >= 2 {
		return strings.ToUpper(mm[1])
	}
	return "GET"
}

// swiftCallArgEnd returns the offset just past the balanced `)` that closes the
// argument list whose opening `(` precedes `pos` (depth starts at 1 because the
// caller's match consumed the opening paren and the first string arg). Capped
// at pos+maxLen so a missing close paren can't run to EOF.
func swiftCallArgEnd(content string, pos, maxLen int) int {
	limit := pos + maxLen
	if limit > len(content) {
		limit = len(content)
	}
	depth := 1
	for i := pos; i < limit; i++ {
		switch content[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return limit
}

// swiftResolveURLSessionVerb scans forward from the URL(string:) site up to
// windowEnd (the next URL site or EOF) for a `.httpMethod = "VERB"` assignment;
// defaults to GET. The request var is configured AFTER its URL is constructed,
// so a forward-only, neighbour-clamped scan attributes the verb correctly.
func swiftResolveURLSessionVerb(content string, pos, windowEnd int) string {
	if windowEnd > len(content) {
		windowEnd = len(content)
	}
	if windowEnd <= pos {
		return "GET"
	}
	if mm := swiftHTTPMethodAssignRe.FindStringSubmatch(content[pos:windowEnd]); len(mm) >= 2 {
		return strings.ToUpper(mm[1])
	}
	return "GET"
}

// canonicalizeSwiftInterpolation rewrites Swift `\(expr)` interpolation markers
// inside a URL-literal body to `{param}` placeholders.
func canonicalizeSwiftInterpolation(raw string) string {
	return swiftInterpRe.ReplaceAllString(raw, "{param}")
}

// indexSwiftFuncs builds a sorted (offset, name) list for every Swift
// function / method declaration in the file, for enclosing-fn attribution.
func indexSwiftFuncs(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range swiftEnclosingFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingSwiftFuncAt returns the nearest preceding func name for a call site.
func enclosingSwiftFuncAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
