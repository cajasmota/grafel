// Package scala — Scala test→endpoint route-hit linkage.
//
// #4749 (the Scala slice of epic #4615/#4749, all-framework test→endpoint
// coverage linkage). Emits ONE test_suite entity per Scala test file that
// drives an HTTP endpoint by ROUTE STRING, stamping the captured `VERB route`
// pairs onto the suite's `e2e_route_calls` property. The shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints, #4351/#4369) then matches each pair
// against the cross-file http_endpoint_definition index and emits a TESTS edge
// to the exact endpoint exercised — exactly as the Java junit5 extractor
// (internal/custom/java/junit5.go) and the Kotlin extractor
// (internal/custom/kotlin/tests_route_e2e.go) do for their JVM stacks. The
// engine pass is language-agnostic (it fires on any test_suite carrying
// e2e_route_calls), so the only Scala-specific work is the route capture.
//
// The unit-test controller-call linkage (`val c = new FooController(...);
// c.method()` resolving to a CALLS+credit edge) is handled separately by the
// core Scala extractor's local-variable receiver typing (#4749, in
// internal/extractors/scala/scala.go). Anonymous ScalaTest/specs2 leaf blocks
// (`"x" should "y" in { … }` / `test("…"){ … }`) already get a subject-aware
// TESTS edge from the deep testmap linkage
// (internal/extractors/cross/testmap/frameworks.go detectScalaTest). This file
// only adds the route-hit (b) family.
//
// Three route-driving idioms are captured:
//   - Play test (`route(app, FakeRequest(GET, "/path"))` and the bare
//     `FakeRequest(GET, "/path")`) — the verb is a Play HTTP-method constant
//     (GET/POST/…), the route its second string argument.
//   - Akka/Pekko HTTP route-test DSL (`Get("/path") ~> route`,
//     `Post("/path", entity) ~> Route.seal(route)`) — the verb is the
//     RequestBuilding helper (Get/Post/…), the route its first string argument,
//     anchored on the trailing `~>` so a plain `Get("/x")` outbound client call
//     (handled by the effect sniffer) is not misread as a route test.
//   - http4s client (`client.run(GET(uri"/path"))`, `Request[IO](method = GET,
//     uri = uri"/path")`) — the verb is the method, the route the `uri"…"`
//     interpolator body.
//
// Registration key: "custom_scala_tests_route_e2e".
package scala

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_scala_tests_route_e2e", &scalaTestRouteE2EExtractor{})
}

type scalaTestRouteE2EExtractor struct{}

func (e *scalaTestRouteE2EExtractor) Language() string {
	return "custom_scala_tests_route_e2e"
}

var (
	// Play: route(app, FakeRequest(GET, "/api/v1/x")) and bare
	// FakeRequest(GET, "/api/v1/x"). The verb is a Play HTTP-method constant
	// (GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS); the route its second arg. We
	// anchor on `FakeRequest(` so a plain `route(app, req)` with a pre-built
	// request elsewhere is not captured (no route string to extract there).
	scalaPlayFakeRequestRE = regexp.MustCompile(
		`\bFakeRequest\s*\(\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*,\s*"([^"\n\r]+)"`)

	// Akka/Pekko HTTP route-test DSL: Get("/x") ~> route. The RequestBuilding
	// helper (Get/Post/Put/Delete/Patch/Head/Options) names the verb; the first
	// string argument is the route. The trailing `~>` (possibly after a second
	// argument like an entity) distinguishes a route test from an outbound
	// client `Get("/x")`. We allow an arbitrary non-newline tail before `~>`.
	scalaAkkaRouteTestRE = regexp.MustCompile(
		`\b(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*"([^"\n\r]+)"[^\n\r]*?~>`)

	// http4s: Request[IO](method = Method.GET, uri = uri"/x") and the verb-helper
	// form GET(uri"/x"). First, the explicit Request[F](method = …, uri = uri"…").
	scalaHttp4sRequestRE = regexp.MustCompile(
		`\bmethod\s*=\s*(?:Method\.)?(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b[^\n\r]*?\buri\s*=\s*uri"([^"\n\r]+)"`)

	// http4s verb-helper builder: GET(uri"/x") / POST(body, uri"/x"). The verb is
	// the helper, the route the uri"…" interpolator. Anchored on `uri"` to avoid
	// capturing a non-route GET().
	scalaHttp4sVerbHelperRE = regexp.MustCompile(
		`\b(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\([^\n\r]*?\buri"([^"\n\r]+)"`)
)

func (e *scalaTestRouteE2EExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}
	if !isScalaTestFile(file.Path) {
		return nil, nil
	}
	source := string(file.Content)
	routeCalls := collectScalaTestRouteCalls(source)
	if len(routeCalls) == 0 {
		return nil, nil
	}
	rec := types.EntityRecord{
		Name:       scalaTestSuiteBaseName(file.Path),
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: file.Path,
		Language:   "scala",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":       scalaRouteTestFramework(source),
			"provenance":      "INFERRED_FROM_SCALA_TEST_ROUTE_E2E",
			"e2e_route_calls": strings.Join(routeCalls, "\n"),
		},
	}
	return []types.EntityRecord{rec}, nil
}

// collectScalaTestRouteCalls returns the de-duplicated "VERB route" pairs a
// Scala test file drives by route string (Play FakeRequest, Akka/Pekko HTTP
// route DSL, http4s client). Routes are normalised to a leading-slash path;
// non-path / variable-route literals are dropped (honest exclusion).
func collectScalaTestRouteCalls(source string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawRoute string) {
		route := normaliseScalaTestRoute(rawRoute)
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
	for _, m := range scalaPlayFakeRequestRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range scalaAkkaRouteTestRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range scalaHttp4sRequestRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range scalaHttp4sVerbHelperRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	return out
}

// scalaRouteTestFramework names the route-test framework from the source's
// import/usage fingerprint. Falls back to "scalatest" (the dominant runner) when
// no distinctive HTTP-test marker is present.
func scalaRouteTestFramework(source string) string {
	switch {
	case strings.Contains(source, "FakeRequest") || strings.Contains(source, "play.api.test"):
		return "play"
	case strings.Contains(source, "~>") &&
		(strings.Contains(source, "akka.http") || strings.Contains(source, "ScalatestRouteTest") ||
			strings.Contains(source, "RouteTest")):
		return "akka-http"
	case strings.Contains(source, "pekko.http") || strings.Contains(source, "PekkoHttp"):
		return "pekko-http"
	case strings.Contains(source, "http4s") || strings.Contains(source, "uri\""):
		return "http4s"
	}
	return "scalatest"
}

// normaliseScalaTestRoute reduces a raw route literal to a path: strips a
// scheme+authority prefix, drops query/fragment, collapses repeated slashes.
// Path-param placeholders ({id}) and casing are preserved (the resolver
// wildcards templates and compares case-insensitively).
func normaliseScalaTestRoute(raw string) string {
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

// isScalaTestFile reports whether path looks like a Scala test source — under a
// test source set (`src/test/`, `/test/`, `/it/`) or a *Test/*Spec/*Suite/*IT
// named file. Keeps the route-hit suite off production controllers that merely
// mention a route.
func isScalaTestFile(path string) bool {
	lp := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(lp, "/src/test/") || strings.Contains(lp, "/src/it/") ||
		strings.Contains(lp, "/test/") {
		return true
	}
	base := strings.TrimSuffix(filepath.Base(lp), ".scala")
	return strings.HasSuffix(base, "test") || strings.HasSuffix(base, "tests") ||
		strings.HasSuffix(base, "spec") || strings.HasSuffix(base, "specs") ||
		strings.HasSuffix(base, "suite") || strings.HasSuffix(base, "it")
}

// scalaTestSuiteBaseName derives a suite label from the test file path
// (`.../CountControllerSpec.scala` → `CountControllerSpec`).
func scalaTestSuiteBaseName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
