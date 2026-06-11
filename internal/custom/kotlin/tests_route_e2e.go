// Package kotlin — Kotlin test→endpoint route-hit linkage.
//
// #4687 (the Kotlin slice of epic #4615, all-framework test→endpoint coverage
// linkage). Emits ONE test_suite entity per Kotlin test file that drives an HTTP
// endpoint by ROUTE STRING, stamping the captured `VERB route` pairs onto the
// suite's `e2e_route_calls` property. The shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints, #4351/#4369) then matches each pair
// against the cross-file http_endpoint_definition index and emits a TESTS edge
// to the exact endpoint exercised — exactly as the Java junit5 extractor
// (internal/custom/java/junit5.go) does for JVM-Java Spring tests. The engine
// pass is language-agnostic (it fires on any test_suite carrying
// e2e_route_calls), so the only Kotlin-specific work is the route capture.
//
// Two route-driving idioms are captured:
//   - Spring on Kotlin (MockMvc `mockMvc.perform(get("/path"))`, WebTestClient
//     `webTestClient.get().uri("/path")`) — identical syntax to Java Spring.
//   - Ktor test (`testApplication { client.get("/path") }` and the legacy
//     `handleRequest(HttpMethod.Get, "/path")`) — Ktor-specific.
//
// Registration key: "custom_kotlin_tests_route_e2e".
package kotlin

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_tests_route_e2e", &kotlinTestRouteE2EExtractor{})
}

type kotlinTestRouteE2EExtractor struct{}

func (e *kotlinTestRouteE2EExtractor) Language() string {
	return "custom_kotlin_tests_route_e2e"
}

var (
	// Spring MockMvc: mockMvc.perform(get("/api/v1/x/get_counts"))
	ktMockMvcRE = regexp.MustCompile(
		`\.perform\s*\(\s*(get|post|put|delete|patch)\s*\(\s*"([^"\n\r]+)"`)

	// Spring WebTestClient: webTestClient.get().uri("/api/v1/x").exchange()
	ktWebTestClientRE = regexp.MustCompile(
		`(?s)\.(get|post|put|delete|patch)\s*\(\s*\)\s*\.uri\s*\(\s*"([^"\n\r]+)"`)

	// Ktor testApplication client: client.get("/api/v1/x") / httpClient.post("/x")
	// (also matches the bare `client.get("/x")` inside `testApplication { … }`).
	// The verb is the method on the client; the route its first string argument.
	// Anchored on a `.` receiver so a bare static helper `get("/x")` is not
	// captured here (that form is the Spring MockMvc factory, handled above).
	ktKtorClientRE = regexp.MustCompile(
		`\.(get|post|put|delete|patch)\s*(?:<[^>(]*>)?\s*\(\s*"(/[^"\n\r]*)"`)

	// Ktor legacy handleRequest(HttpMethod.Get, "/api/v1/x").
	ktKtorHandleRequestRE = regexp.MustCompile(
		`\bhandleRequest\s*\(\s*HttpMethod\.(Get|Post|Put|Delete|Patch)\s*,\s*"([^"\n\r]+)"`)
)

func (e *kotlinTestRouteE2EExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	if !isKotlinTestFile(file.Path) {
		return nil, nil
	}
	source := string(file.Content)
	routeCalls := collectKotlinTestRouteCalls(source)
	if len(routeCalls) == 0 {
		return nil, nil
	}
	name := kotlinTestSuiteBaseName(file.Path)
	framework := "kotest"
	if strings.Contains(source, "mockMvc") || strings.Contains(source, "MockMvc") ||
		strings.Contains(source, "WebTestClient") || strings.Contains(source, "webTestClient") {
		framework = "spring"
	} else if strings.Contains(source, "testApplication") ||
		strings.Contains(source, "handleRequest") || strings.Contains(source, "io.ktor") {
		framework = "ktor"
	}
	rec := types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: file.Path,
		Language:   "kotlin",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":       framework,
			"provenance":      "INFERRED_FROM_KOTLIN_TEST_ROUTE_E2E",
			"e2e_route_calls": strings.Join(routeCalls, "\n"),
		},
	}
	return []types.EntityRecord{rec}, nil
}

// collectKotlinTestRouteCalls returns the de-duplicated "VERB route" pairs a
// Kotlin test file drives by route string (Spring MockMvc / WebTestClient and
// Ktor client / handleRequest). Routes are normalised to a leading-slash path;
// non-path / variable-route literals are dropped (honest exclusion).
func collectKotlinTestRouteCalls(source string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawRoute string) {
		route := normaliseKotlinTestRoute(rawRoute)
		if route == "" || !strings.HasPrefix(route, "/") {
			return
		}
		line := strings.ToUpper(verb) + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}
	for _, m := range ktMockMvcRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range ktWebTestClientRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range ktKtorClientRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range ktKtorHandleRequestRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	return out
}

// normaliseKotlinTestRoute reduces a raw route literal to a path: strips a
// scheme+authority prefix, drops query/fragment, collapses repeated slashes.
// Path-param placeholders ({id}) and casing are preserved (the resolver
// wildcards templates and compares case-insensitively).
func normaliseKotlinTestRoute(raw string) string {
	p := strings.TrimSpace(raw)
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
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

// isKotlinTestFile reports whether path looks like a Kotlin test source — under a
// test source set (`src/test/`, `/test/`) or a *Test/*Spec/*IT named file. Keeps
// the route-hit suite off production controllers that merely mention a route.
func isKotlinTestFile(path string) bool {
	lp := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(lp, "/src/test/") || strings.Contains(lp, "/test/") ||
		strings.Contains(lp, "/androidtest/") {
		return true
	}
	base := strings.TrimSuffix(filepath.Base(lp), ".kt")
	return strings.HasSuffix(base, "test") || strings.HasSuffix(base, "spec") ||
		strings.HasSuffix(base, "it") || strings.HasSuffix(base, "tests")
}

// kotlinTestSuiteBaseName derives a suite label from the test file path
// (`.../CountControllerTest.kt` → `CountControllerTest`).
func kotlinTestSuiteBaseName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
