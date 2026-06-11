// tests_route_e2e.go — Dart package:test → endpoint route-hit linkage.
//
// #4758 (the Dart slice of the coverage-linkage tail epic #4749; closes the
// #4757 N/A now that the Dart producer exists). Emits ONE test_suite entity per
// Dart test file that drives an HTTP endpoint by ROUTE STRING, stamping the
// captured `VERB route` pairs onto the suite's `e2e_route_calls` property (one
// "VERB route" per line). The shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints, #4351/#4369) then matches each pair
// against the cross-file http_endpoint_definition index — which, for Dart, is
// populated by synthesizeShelfRoutes / synthesizeDartFrogRoutes /
// synthesizeConduitRoutes (#4758, internal/engine/http_endpoint_dart.go) — and
// emits a TESTS edge to the exact shelf/dart_frog/conduit endpoint exercised,
// exactly as the Nim, Crystal, Swift, Java, Ruby, PHP and C# slices do. The
// engine pass is language-agnostic (it fires on any test_suite carrying
// e2e_route_calls), so the only Dart-specific work here is the route capture.
//
// Route-driving idioms captured (shelf / dart_frog / package:http test utils):
//   - await handler(Request('GET', Uri.parse('/todos')))      → GET    /todos
//   - handler(Request('POST', Uri.parse('http://x/todos')))   → POST   /todos
//   - await router.call(Request('DELETE', Uri.parse('/x/1'))) → DELETE /x/1
//   - http.get(Uri.parse('http://localhost:8080/health'))     → GET    /health
//   - client.post(Uri.parse('/users'), body: …)               → POST   /users
//
// package:test's DSL uses `test('description', () { … })` closures (and `group(
// '…', () { … })` containers). The description is PROSE, not a code symbol, and
// the closure body carries no callable entity name the production-symbol
// resolver can bind to. So, like the JS #4680 / Ruby #4719 / Nim #4758
// anonymous-closure case, the route-hit signal is carried by the SUITE-LEVEL
// test_suite entity emitted here (the scope-owner role): the test_suite is the
// owner that carries the e2e_route_calls the engine links from.
//
// Local-variable / receiver typing (#4749 part a) is N/A for Dart coverage
// linkage: shelf route dispatch is keyed by the literal route string carried in
// the `Request('VERB', Uri.parse('/path'))` constructor, not by an
// `obj.method()` receiver call, so a `receiver_type` stamp would be a dead
// annotation. The honest, working coverage mechanism for Dart is the route-hit
// → endpoint linkage in THIS file, exactly as for Nim and Crystal.
//
// Honest exclusions (no fabricated edges):
//   - A route built from interpolation/concatenation with no static `/segment`
//     literal is dropped.
//   - Non-request test files (pure unit tests that never hit a route) emit no
//     suite.
//
// Registration key: "custom_dart_tests_route_e2e".
package dart

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_dart_tests_route_e2e", &dartTestRouteE2EExtractor{})
}

type dartTestRouteE2EExtractor struct{}

func (e *dartTestRouteE2EExtractor) Language() string {
	return "custom_dart_tests_route_e2e"
}

var (
	// dartRequestRouteRE matches a shelf/dart_frog test handler hit constructed
	// as `Request('VERB', Uri.parse('/path'))`: capture group 1 is the verb
	// literal, group 2 is the URL/path string inside `Uri.parse(...)`. Both
	// quote styles are accepted on each literal.
	dartRequestRouteRE = regexp.MustCompile(
		`Request\s*\(\s*(?:'([^'\n\r]*)'|"([^"\n\r]*)")\s*,\s*Uri\.parse\s*\(\s*(?:'([^'\n\r]*)'|"([^"\n\r]*)")`)

	// dartHTTPVerbCallRE matches a package:http / shelf client verb call whose
	// first argument is `Uri.parse('…')`: `http.get(Uri.parse('/health'))`,
	// `client.post(Uri.parse('http://x/users'), body: …)`. Capture group 1 is
	// the verb; groups 2/3 are the URL/path literal.
	dartHTTPVerbCallRE = regexp.MustCompile(
		`\.(get|post|put|delete|patch|head|options)\s*\(\s*Uri\.parse\s*\(\s*(?:'([^'\n\r]*)'|"([^"\n\r]*)")`)
)

func (e *dartTestRouteE2EExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "dart" {
		return nil, nil
	}
	if !isDartTestFile(file.Path) {
		return nil, nil
	}
	source := string(file.Content)
	routeCalls := collectDartTestRouteCalls(source)
	if len(routeCalls) == 0 {
		return nil, nil
	}
	rec := types.EntityRecord{
		Name:       dartTestSuiteBaseName(file.Path),
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: file.Path,
		Language:   "dart",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":       "package:test",
			"provenance":      "INFERRED_FROM_DART_TEST_ROUTE_E2E",
			"e2e_route_calls": strings.Join(routeCalls, "\n"),
		},
	}
	rec.ID = rec.ComputeID()
	return []types.EntityRecord{rec}, nil
}

// collectDartTestRouteCalls returns the de-duplicated "VERB route" pairs a Dart
// test file drives by route string, from both the `Request('VERB',
// Uri.parse('/x'))` handler-hit form and the `client.get(Uri.parse('/x'))`
// http-client form. Routes are normalised to a leading-slash path; a route with
// no static `/segment` literal is dropped.
func collectDartTestRouteCalls(source string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawURL string) {
		route := normaliseDartTestRoute(rawURL)
		if route == "" {
			return
		}
		line := strings.ToUpper(verb) + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}
	for _, m := range dartRequestRouteRE.FindAllStringSubmatch(source, -1) {
		verb := firstNonEmpty(m[1], m[2])
		url := firstNonEmpty(m[3], m[4])
		add(verb, url)
	}
	for _, m := range dartHTTPVerbCallRE.FindAllStringSubmatch(source, -1) {
		add(m[1], firstNonEmpty(m[2], m[3]))
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// normaliseDartTestRoute reduces a raw URL/path literal to a path: strips a
// scheme+authority prefix (http://127.0.0.1:8080/x → /x), drops a query/fragment
// tail, ensures a single leading slash, collapses repeated slashes. A literal
// that is only a host/scheme with no path, that has no static `/segment`, or
// that is interpolated (`$`) is dropped (returns "").
func normaliseDartTestRoute(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" || strings.Contains(p, "$") {
		return ""
	}
	if i := strings.Index(p, "://"); i >= 0 {
		rest := p[i+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			p = rest[slash:]
		} else {
			return ""
		}
	}
	if q := strings.IndexAny(p, "?#"); q >= 0 {
		p = p[:q]
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if p == "/" {
		return ""
	}
	return strings.TrimRight(p, "/")
}

// isDartTestFile reports whether path looks like a Dart test — a `*_test.dart`
// file (the package:test convention is `test/foo_test.dart`) or any file under
// a `/test/` directory. Keeps the route-hit suite off production code that
// merely registers a route.
func isDartTestFile(path string) bool {
	lp := filepath.ToSlash(path)
	base := filepath.Base(lp)
	if !strings.HasSuffix(base, ".dart") {
		return false
	}
	stem := strings.TrimSuffix(base, ".dart")
	if strings.HasSuffix(stem, "_test") {
		return true
	}
	return strings.Contains(lp, "/test/") || strings.Contains(lp, "/integration_test/")
}

// dartTestSuiteBaseName derives a suite label from the test file path
// (`.../todos_test.dart` → `todos_test`).
func dartTestSuiteBaseName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
