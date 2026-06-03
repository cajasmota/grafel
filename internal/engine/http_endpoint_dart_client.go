// Dart / Flutter consumer-side HTTP client synthesis (#3574, epic #3571).
//
// Flutter mobile screens reach their backend through one of two dominant Dart
// HTTP clients:
//
//   - Dio (https://pub.dev/packages/dio) — the de-facto Flutter networking lib:
//     dio.get("/users"), dio.post("/auth/login", data: body)
//     dio.put("/users/$id"), dio.delete("/users/${user.id}")
//     _dio.get("https://api.example.com/v1/users")
//
//   - package:http (https://pub.dev/packages/http) — the lower-level client:
//     http.get(Uri.parse("https://api.example.com/v1/users"))
//     http.post(Uri.parse("/auth/login"), body: payload)
//     client.get(Uri.parse("/users/$id"))
//
// Before this pass a Flutter app that called a downstream API produced no
// http_endpoint_call entity and no cross-repo FETCHES edge — the mobile side of
// the cross-link graph was invisible, so a backend route served by the Upvate
// API had no consumer edge from the mobile screen that calls it.
//
// This emits one synthetic http_endpoint_call per detected Dio / http verb call
// site (via the shared emitClientRuntime from applyHTTPEndpointSynthesis), so
// the existing cross-repo linker pairs them with producer-side route
// definitions by canonical path Name. The entity shape is IDENTICAL to the
// scala/elixir/kotlin client synthesizers (kind=http_endpoint_call,
// id=http:<VERB>:<path>, FETCHES edge from the enclosing function) — the
// cross-repo join depends on that identical shape.
//
// The URL host is stripped (the producer serves "/v1/users" without the host);
// Dart string-interpolation markers `$id` / `${expr}` become `{id}` / `{param}`
// placeholders to match server-route normalisation. The enclosing
// `<ret> name(...)` / `Future<...> name(...) async` is attributed as the
// calling reference.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// dartDioVerbRe matches a Dio verb call whose first positional argument is a
// double-quoted or single-quoted string literal:
//
//	dio.get("/users")            _dio.post('/auth/login')
//	apiClient.dio.put("/x/$id")  client.delete("...")
//
// Receiver allow-list (left side of the `.`): dio, _dio, client, _client,
// apiClient, httpClient, http (the `http` form is also matched by the http.get
// + Uri.parse pattern below; the side-scoped dedup collapses the duplicate ID).
// Group 1 = verb, group 2 = double-quoted body, group 3 = single-quoted body.
var dartDioVerbRe = regexp.MustCompile(
	`\b(?:dio|_dio|client|_client|apiClient|httpClient|http)\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*(?:"([^"\n\r]+)"|'([^'\n\r]+)')`,
)

// dartHttpUriVerbRe matches the package:http form where the URL is wrapped in
// `Uri.parse(...)`:
//
//	http.get(Uri.parse("https://api.example.com/v1/users"))
//	http.post(Uri.parse('/auth/login'), body: payload)
//	client.get(Uri.parse("/users/$id"))
//
// Group 1 = verb, group 2 = double-quoted body, group 3 = single-quoted body.
var dartHttpUriVerbRe = regexp.MustCompile(
	`\b(?:http|client|_client)\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*Uri\s*\.\s*parse\s*\(\s*(?:"([^"\n\r]+)"|'([^'\n\r]+)')`,
)

// dartInterpRe rewrites a Dart string-interpolation marker (`$id` or
// `${expr}`) into a `{id}` / `{param}` placeholder so the canonical path is
// stable across call sites and matches the server-route normalisation.
var dartInterpRe = regexp.MustCompile(`\$\{[^}]*\}|\$[A-Za-z_]\w*`)

// dartEnclosingFuncRe captures Dart function / method declarations for caller
// attribution. Handles bare functions, async functions, and methods with a
// return type:
//
//	Future<List<User>> fetchUsers() async { ... }
//	void createUser(User u) { ... }
//	login(String email) async { ... }
//
// The return type (if any) is optional; group 1 is the declared name. The
// trailing `(` anchors it to a callable declaration (not a field). Dart
// keywords that can precede a method (`static`, `Future`, `void`, etc.) are
// absorbed by the optional type prefix.
var dartEnclosingFuncRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(?:static|final|external|abstract)\s+)*(?:[\w<>,\[\]?.]+\s+)?([A-Za-z_]\w*)\s*\([^;{]*\)\s*(?:async\s*\*?\s*)?\{`,
)

// dartClientEmitFn is the runtime-aware emitter type for the Dart client.
type dartClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeDartClientWithRuntime is the package-level entry point referenced
// from applyHTTPEndpointSynthesis. Emits one outbound http_endpoint_call per
// Dio / http verb call site + a FETCHES edge from the enclosing function.
func synthesizeDartClientWithRuntime(content string, emit dartClientEmitFn) {
	// GraphQL client operations (graphql_flutter / graphql `gql(...)` documents)
	// emit operation-level http_endpoint_call entities keyed to the server
	// endpoint shape http:GRAPHQL:/graphql/<Root>/<field> (#4036). Run BEFORE the
	// REST early-exit guard below, since a pure-GraphQL Flutter file may contain
	// none of the Dio / package:http markers.
	if strings.Contains(content, "gql(") {
		synthesizeDartGraphQLClient(content, indexDartFuncs(content), emit)
	}

	if !strings.Contains(content, "dio") && !strings.Contains(content, "Dio") &&
		!strings.Contains(content, "http.") && !strings.Contains(content, "Uri.parse") &&
		!strings.Contains(content, ".get(") && !strings.Contains(content, ".post(") {
		return
	}

	funcs := indexDartFuncs(content)

	emitMatch := func(m []int, verbG, dqG, sqG int, framework string) {
		verb := strings.ToUpper(content[m[verbG]:m[verbG+1]])
		raw := ""
		if m[dqG] >= 0 {
			raw = content[m[dqG]:m[dqG+1]]
		} else if m[sqG] >= 0 {
			raw = content[m[sqG]:m[sqG+1]]
		}
		if raw == "" {
			return
		}
		runtimeDynamic := strings.Contains(raw, "$")
		raw = canonicalizeDartInterpolation(raw)
		path, ok := normalizeRawClientPath(raw)
		if !ok {
			return
		}
		caller := enclosingDartFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, framework, "Function", caller, runtimeDynamic)
	}

	// package:http via Uri.parse runs FIRST: `http.get(Uri.parse("..."))`
	// overlaps the bare `http.get("...")` receiver shape, so claiming the ID
	// here stamps the correct `http` framework label; the side-scoped dedup in
	// applyHTTPEndpointSynthesis then suppresses the Dio re-emission.
	for _, m := range dartHttpUriVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		emitMatch(m, 2, 4, 6, "http_dart")
	}

	// Dio verb calls.
	for _, m := range dartDioVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		emitMatch(m, 2, 4, 6, "dio")
	}
}

// canonicalizeDartInterpolation rewrites Dart `$x` / `${expr}` interpolation
// markers inside a URL-literal body to `{param}` placeholders.
func canonicalizeDartInterpolation(raw string) string {
	return dartInterpRe.ReplaceAllStringFunc(raw, func(tok string) string {
		if strings.HasPrefix(tok, "${") {
			return "{param}"
		}
		name := strings.TrimPrefix(tok, "$")
		return "{" + name + "}"
	})
}

// indexDartFuncs builds a sorted (offset, name) list for every Dart
// function / method declaration in the file, for enclosing-fn attribution.
func indexDartFuncs(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range dartEnclosingFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		// Skip control-flow keywords the loose return-type prefix can capture
		// when a block follows (`if (...) {`, `for (...) {`, etc.).
		switch name {
		case "if", "for", "while", "switch", "catch", "return":
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: name})
	}
	return out
}

// enclosingDartFuncAt returns the nearest preceding function name for a call site.
func enclosingDartFuncAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
