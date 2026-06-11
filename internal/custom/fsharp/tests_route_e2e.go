// tests_route_e2e.go — F# test → endpoint route-hit linkage.
//
// #4749 (the F# slice of the coverage-linkage tail epic #4749/#4615; mirrors the
// Crystal/Kemal slice #4760, the Swift/Vapor slice #4755 and the functional
// Elixir slice #4688).
//
// Emits ONE test_suite entity per F# test file that drives an HTTP endpoint by
// ROUTE STRING through an in-memory `TestServer` HTTP client, stamping the
// captured `VERB route` pairs onto the suite's `e2e_route_calls` property (one
// "VERB route" per line). The shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints, #4351/#4369) then matches each pair
// against the cross-file http_endpoint_definition index — which, for F#, is
// populated by synthesizeGiraffeRoutes (#4749) — and emits a TESTS edge to the
// exact Giraffe/Saturn endpoint exercised, exactly as the Crystal, Swift, Rust,
// Java, Ruby, PHP, C# and Elixir slices do for their stacks. The engine pass is
// language-agnostic (it fires on any test_suite carrying e2e_route_calls), so
// the only F#-specific work here is the test route capture.
//
// F# route-driving idioms captured (the standard Giraffe/Saturn integration-test
// shape — an ASP.NET Core `TestServer` HttpClient):
//   - client.GetAsync("/users")            → GET /users
//   - client.PostAsync("/users", content)  → POST /users
//   - client.DeleteAsync("/users/1")       → DELETE /users/1
//   - client.PutAsync("/users/1", c)       → PUT /users/1
//   - client.PatchAsync("/users/1", c)     → PATCH /users/1
//   - HttpRequestMessage(HttpMethod.Get, "/users")  → GET /users
//
// The HttpClient verb methods (`GetAsync`/`PostAsync`/…) carry a leading
// string-literal URI argument. An `HttpRequestMessage(HttpMethod.X, "/path")`
// constructor carries the verb as the first arg and the URI as the second.
//
// Test-scope / scope-owner
// ------------------------
// F# test DSLs use ANONYMOUS closure blocks for individual cases:
//   - Expecto: `testCase "name" <| fun _ -> ...` (and `testList "..." [ ... ]`)
//   - xUnit:   `[<Fact>] let ``test name`` () = ...` (named — but the route hit
//     is still attributed at suite granularity to match the cross-language pass)
// The `testCase`/`testList` example body owns no named call-bearing entity (it
// is a `fun _ -> ...` closure passed to a combinator), so — exactly like the
// Ruby #4684 / JS #4680 / Crystal #4760 anonymous-block scope-owner — the
// route-hit signal is carried by the SUITE-LEVEL test_suite entity emitted here.
// This is the F# analog of those scope-owners: the test_suite is the owner that
// carries the e2e_route_calls the engine links from.
//
// Local-variable / receiver typing (#4749 part a) is N/A for F# coverage
// linkage: F# is functional — Giraffe handlers are `let`-bound `HttpHandler`
// values composed with `>=>`, not `obj.method()` receiver calls, and the fsharp
// base extractor names `let` entities by their BARE name with no class-qualified
// receiver resolver to consume a `receiver_type` stamp. It would be a dead
// annotation. The honest, working coverage mechanism for F# is the route-hit →
// endpoint linkage in THIS file (route dispatch is keyed by the literal route
// string, not by an `obj.method()` receiver), exactly as for the functional
// Elixir and Crystal slices. See the coverage-doc note for the recorded N/A
// rationale.
//
// Honest exclusions (no fabricated edges):
//   - Interpolated / variable routes capture the literal prefix only when a
//     static string is present; a fully interpolated route (`$"{path}"`) is
//     dropped.
//   - Non-request tests (pure unit tests that never hit a route) emit no suite.
//   - A route helper appearing OUTSIDE a test file is left to the producer-side
//     synthesizer (synthesizeGiraffeRoutes); this extractor only runs on tests.
//
// Registration key: "custom_fsharp_tests_route_e2e".
package fsharp

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_fsharp_tests_route_e2e", &fsharpTestRouteE2EExtractor{})
}

type fsharpTestRouteE2EExtractor struct{}

func (e *fsharpTestRouteE2EExtractor) Language() string {
	return "custom_fsharp_tests_route_e2e"
}

// fsharpClientVerbRE matches an ASP.NET Core HttpClient verb call with a leading
// string-literal URI:
//
//	client.GetAsync("/users")
//	client.PostAsync("/users", content)
//	httpClient.DeleteAsync("/users/1")
//
// Capture group 1 is the verb (Get/Post/Put/Delete/Patch/Head/Options); group 2
// is the route literal.
var fsharpClientVerbRE = regexp.MustCompile(
	`\.(Get|Post|Put|Delete|Patch|Head|Options)Async\s*\(\s*\$?"([^"\n\r]*)"`,
)

// fsharpRequestMessageRE matches an `HttpRequestMessage(HttpMethod.X, "/path")`
// constructor. Capture group 1 is the verb; group 2 is the route literal.
var fsharpRequestMessageRE = regexp.MustCompile(
	`HttpRequestMessage\s*\(\s*HttpMethod\.(Get|Post|Put|Delete|Patch|Head|Options)\s*,\s*\$?"([^"\n\r]*)"`,
)

func (e *fsharpTestRouteE2EExtractor) Extract(
	_ context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "fsharp" {
		return nil, nil
	}
	if !isFSharpTestFile(file.Path, string(file.Content)) {
		return nil, nil
	}
	source := string(file.Content)
	routeCalls := collectFSharpTestRouteCalls(source)
	if len(routeCalls) == 0 {
		return nil, nil
	}
	rec := types.EntityRecord{
		Name:       fsharpTestSuiteBaseName(file.Path),
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: file.Path,
		Language:   "fsharp",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":       "fsharp-testserver",
			"provenance":      "INFERRED_FROM_FSHARP_TEST_ROUTE_E2E",
			"e2e_route_calls": strings.Join(routeCalls, "\n"),
		},
	}
	rec.ID = rec.ComputeID()
	return []types.EntityRecord{rec}, nil
}

// collectFSharpTestRouteCalls returns the de-duplicated "VERB route" pairs a
// test file drives by route string. Routes are normalised to a leading-slash
// path; fully-interpolated routes (no static literal) are dropped.
func collectFSharpTestRouteCalls(source string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verbRaw, rawPath string) {
		verb := strings.ToUpper(verbRaw)
		route := normaliseFSharpTestRoute(rawPath)
		if route == "" {
			return
		}
		line := verb + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}
	for _, m := range fsharpClientVerbRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range fsharpRequestMessageRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	return out
}

// normaliseFSharpTestRoute reduces a raw test route literal to a path: ensures a
// single leading slash, drops a query/fragment tail, collapses repeated slashes.
// A route whose value begins with a string interpolation (`$"{...}"` / `{...}`)
// with no static prefix is dropped (returns ""). Path-param placeholders and
// casing are preserved (the resolver wildcards templates and compares
// case-insensitively).
func normaliseFSharpTestRoute(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	// Truncate at the first interpolation marker — the static prefix is the
	// only statically-recoverable part. `"users/{id}"` → `/users`.
	if i := strings.Index(p, "{"); i >= 0 {
		p = p[:i]
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
	// A bare "/" after truncation carries no route identity — drop it.
	if p == "/" {
		return ""
	}
	return strings.TrimRight(p, "/")
}

// isFSharpTestFile reports whether path/content looks like an F# test file — a
// `*Test(s).fs` / `*Spec(s).fs` file, a file under a `/test(s)/` directory, or a
// file referencing a known F# test framework (Expecto / xUnit / NUnit). Keeps
// the route-hit suite off production code that merely registers a route.
func isFSharpTestFile(path, content string) bool {
	lp := filepath.ToSlash(path)
	if !strings.HasSuffix(lp, ".fs") && !strings.HasSuffix(lp, ".fsx") {
		return false
	}
	base := strings.ToLower(filepath.Base(lp))
	if strings.Contains(base, "test") || strings.Contains(base, "spec") {
		return true
	}
	low := strings.ToLower(lp)
	if strings.Contains(low, "/test/") || strings.Contains(low, "/tests/") ||
		strings.Contains(low, "/spec/") || strings.Contains(low, "/specs/") {
		return true
	}
	// Framework signal: Expecto / xUnit / NUnit references in the file.
	return strings.Contains(content, "Expecto") ||
		strings.Contains(content, "testCase") ||
		strings.Contains(content, "testList") ||
		strings.Contains(content, "[<Fact>]") ||
		strings.Contains(content, "[<Theory>]") ||
		strings.Contains(content, "Xunit") ||
		strings.Contains(content, "NUnit")
}

// fsharpTestSuiteBaseName derives a suite label from the test file path
// (`.../UsersTests.fs` → `UsersTests`).
func fsharpTestSuiteBaseName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
