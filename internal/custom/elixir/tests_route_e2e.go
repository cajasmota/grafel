// Package elixir — Elixir/Phoenix test→endpoint route-hit linkage.
//
// #4688 (the Elixir/Phoenix slice of epic #4615, all-framework test→endpoint
// coverage linkage; CLOSES #4688). Emits ONE test_suite entity per Elixir
// ExUnit test file that drives an HTTP endpoint by ROUTE STRING via Phoenix
// ConnTest, stamping the captured `VERB route` pairs onto the suite's
// `e2e_route_calls` property. The shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints, #4351/#4369) then matches each pair
// against the cross-file http_endpoint_definition index and emits a TESTS edge
// to the exact endpoint exercised — exactly as the Java junit5, Kotlin, Ruby,
// PHP and C# slices do for their stacks. The engine pass is language-agnostic
// (it fires on any test_suite carrying e2e_route_calls), so the only
// Elixir-specific work is the route capture.
//
// Phoenix ConnTest route-driving idioms captured:
//   - Piped form:     conn |> get("/api/v1/x") / conn |> post("/api/v1/x", %{})
//   - Direct form:    get(conn, "/api/v1/x") / post(conn, "/api/v1/x", params)
//     for the verbs get/post/put/patch/delete (the ConnTest request macros).
//
// Elixir is FUNCTIONAL: there are no OO receiver objects, so the
// "local-variable receiver typing" gap (#4680/#4681) does NOT apply — Phoenix
// dispatch is keyed by the literal route string, not by an `obj.method()`
// receiver. The route-hit → endpoint linkage IS the coverage mechanism here.
//
// Honest exclusion (matches the negative acceptance of #4688):
//   - Router-helper form `get(conn, Routes.x_path(conn, :action))` — the path
//     is not a string literal and is not statically recoverable here, so it is
//     dropped (router-helper-form limit noted in the coverage registry).
//   - Interpolated / variable routes (`get(conn, "/x/#{id}")`, `get(conn, path)`)
//     are dropped — no static route to match.
//   - Shape-only tests (assert on a struct, never hit a route) emit no suite.
//
// Registration key: "custom_elixir_tests_route_e2e".
package elixir

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_elixir_tests_route_e2e", &elixirTestRouteE2EExtractor{})
}

type elixirTestRouteE2EExtractor struct{}

func (e *elixirTestRouteE2EExtractor) Language() string {
	return "custom_elixir_tests_route_e2e"
}

var (
	// Direct ConnTest macro form: get(conn, "/api/v1/x") /
	// post(conn, "/api/v1/x", params). The first arg is the conn, the second a
	// string-literal route. Anchored on a word boundary so it is not confused
	// with a piped `|> get(...)` (which has the route as its FIRST arg).
	elxConnTestDirectRE = regexp.MustCompile(
		`\b(get|post|put|patch|delete)\s*\(\s*conn\s*,\s*"([^"\n\r]*)"`)

	// Piped ConnTest form: conn |> get("/api/v1/x") / |> post("/api/v1/x", %{}).
	// The route is the first string-literal argument after the verb. Anchored on
	// the pipe operator so the production router macro `get "/x", Ctrl, :act`
	// (no parenthesis, no pipe) is never captured.
	elxConnTestPipedRE = regexp.MustCompile(
		`\|>\s*(get|post|put|patch|delete)\s*\(\s*"([^"\n\r]*)"`)
)

func (e *elixirTestRouteE2EExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "elixir" {
		return nil, nil
	}
	if !isElixirTestFile(file.Path) {
		return nil, nil
	}
	source := string(file.Content)
	routeCalls := collectElixirTestRouteCalls(source)
	if len(routeCalls) == 0 {
		return nil, nil
	}
	rec := types.EntityRecord{
		Name:       elixirTestSuiteBaseName(file.Path),
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: file.Path,
		Language:   "elixir",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":       "phoenix",
			"provenance":      "INFERRED_FROM_ELIXIR_TEST_ROUTE_E2E",
			"e2e_route_calls": strings.Join(routeCalls, "\n"),
		},
	}
	return []types.EntityRecord{rec}, nil
}

// collectElixirTestRouteCalls returns the de-duplicated "VERB route" pairs a
// Phoenix ConnTest test file drives by route string (direct and piped forms).
// Routes are normalised to a leading-slash path; interpolated / variable /
// router-helper routes (non-path literals) are dropped (honest exclusion).
func collectElixirTestRouteCalls(source string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawRoute string) {
		route := normaliseElixirTestRoute(rawRoute)
		if route == "" || !strings.HasPrefix(route, "/") {
			return
		}
		// Drop interpolated routes — `#{...}` is not statically recoverable.
		if strings.Contains(route, "#{") {
			return
		}
		line := strings.ToUpper(verb) + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}
	for _, m := range elxConnTestDirectRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range elxConnTestPipedRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	return out
}

// normaliseElixirTestRoute reduces a raw route literal to a path: strips a
// scheme+authority prefix, drops query/fragment, collapses repeated slashes.
// Phoenix path-param placeholders (`:id`) and casing are preserved (the
// resolver wildcards templates and compares case-insensitively).
func normaliseElixirTestRoute(raw string) string {
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
		// A bare `#` (fragment) truncates; `#{` interpolation is caught by the
		// caller's contains check, which runs on the pre-truncation form only
		// when the `#` is the interpolation sigil. Guard: keep `#{` intact so
		// the caller can drop it.
		if !strings.HasPrefix(p[q:], "#{") {
			p = p[:q]
		}
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

// isElixirTestFile reports whether path looks like an ExUnit test source — an
// `*_test.exs` file or a file under a `/test/` directory. Keeps the route-hit
// suite off production controllers/routers that merely mention a route.
func isElixirTestFile(path string) bool {
	lp := strings.ToLower(filepath.ToSlash(path))
	if strings.HasSuffix(lp, "_test.exs") {
		return true
	}
	if strings.Contains(lp, "/test/") && strings.HasSuffix(lp, ".exs") {
		return true
	}
	return false
}

// elixirTestSuiteBaseName derives a suite label from the test file path
// (`.../count_controller_test.exs` → `count_controller_test`).
func elixirTestSuiteBaseName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
