// Package erlang — Erlang test→endpoint route-hit linkage.
//
// #4749 (the Erlang slice of epic #4615, all-framework test→endpoint coverage
// linkage). Emits ONE test_suite entity per Erlang eunit / common_test test
// file that drives an HTTP endpoint by ROUTE STRING, stamping the captured
// `VERB route` pairs onto the suite's `e2e_route_calls` property. The shared
// resolve pass (engine.linkE2ERouteTestsToEndpoints, #4351/#4369) then matches
// each pair against the cross-file http_endpoint_definition index (produced for
// Erlang by engine.synthesizeCowboy via the new `case "erlang"` synthesis
// dispatch) and emits a TESTS edge to the exact endpoint exercised — exactly as
// the Elixir/Phoenix, Ruby, PHP, C#, Swift and Clojure slices do for their
// stacks. The engine pass is language-agnostic (it fires on any test_suite
// carrying e2e_route_calls), so the only Erlang-specific work is the route
// capture.
//
// Route-driving idioms captured (eunit / common_test against a test Cowboy /
// Elli server, or direct HTTP client calls):
//
//	httpc:request(get,  {"http://localhost:8080/users/1", []}, [], [])
//	httpc:request(post, {"/users", [], "application/json", Body}, [], [])
//	httpc:request("http://localhost:8080/todos")            % bare GET form
//	gun:get(Conn,  "/todos")
//	gun:post(Conn, "/users", Headers, Body)
//	hackney:request(get,  <<"http://localhost:8080/health">>, ...)
//	hackney:get(<<"/todos/1">>, ...)
//
// httpc's verb is the FIRST argument atom (`get`/`post`/…) and the URL is the
// first string in the request tuple (or the bare first string arg for the
// 1-arity GET form). gun/hackney encode the verb in the function name
// (`gun:get`, `hackney:post`). hackney URLs are frequently binaries
// (`<<"...">>`), which we unwrap.
//
// Erlang is FUNCTIONAL / process-based: there are NO OO receiver objects, so the
// "local-variable receiver typing" gap (#4680/#4681) does NOT apply — a Cowboy
// handler is dispatched by the literal route path carried on the request, not by
// an `obj.method()` receiver. The route-hit → endpoint linkage IS the coverage
// mechanism here (mirrors the Elixir #4688 / Clojure #4749 documented N/A for
// receiver typing).
//
// Test scope: eunit `name_test() -> ...` / `name_test_() -> ...` generators and
// common_test `case(Config) -> ...` clauses are NAMED top-level functions
// already mined by the base extractor's CALLS graph; the route-hit calls live
// inside those bodies, so the suite is keyed to the FILE (one suite per test
// file) the same way the Jest / ExUnit / clojure.test one-suite-per-file model
// works — no synthetic anonymous-test-block scope owner is needed (Erlang test
// "blocks" are named function clauses, not closures).
//
// Honest exclusion (matches the negative acceptance of the slice):
//   - Built / concatenated URLs (`httpc:request(get, {"http://localhost:" ++
//     integer_to_list(Port) ++ "/users", []}, ...)`) where the path is not a
//     single string literal are NOT statically recoverable and are dropped. This
//     is the COMMON real-world shape (eunit/CT spin a server on an ephemeral
//     port and build the URL), so Erlang test-route linkage is recorded as
//     PARTIAL: only fully-literal-path hits link. See the registry note + the
//     #4749 Erlang follow-up for `++`-built-URL recovery.
//   - Shape-only tests (assert on a value, never hit a route) emit no suite.
//
// Registration key: "custom_erlang_tests_route_e2e".
package erlang

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_erlang_tests_route_e2e", &erlangTestRouteE2EExtractor{})
}

type erlangTestRouteE2EExtractor struct{}

func (e *erlangTestRouteE2EExtractor) Language() string {
	return "custom_erlang_tests_route_e2e"
}

var (
	// httpc:request(get, {"url", ...}, ...) — verb-tagged request tuple form.
	// Group 1 is the verb atom; group 2 is the first string literal in the tuple
	// (the URL). The URL must be a bare double-quoted string literal.
	erlHttpcVerbTupleRE = regexp.MustCompile(
		`httpc\s*:\s*request\s*\(\s*([a-z]+)\s*,\s*\{\s*"([^"\n\r]*)"`)

	// httpc:request("url") / httpc:request("url", ...) — bare 1/2-arity GET form
	// where the first arg is the URL string literal (verb defaults to GET).
	// Group 1 is the URL. The negative-lookahead-free regex requires the first
	// arg to be a string literal (so the verb-tuple form above is not re-matched,
	// since that starts with an atom not a quote).
	erlHttpcBareRE = regexp.MustCompile(
		`httpc\s*:\s*request\s*\(\s*"([^"\n\r]*)"`)

	// gun:get(Conn, "/path") / gun:post(Conn, "/path", ...) — verb in the
	// function name, path is the SECOND argument string literal.
	// Group 1 is the verb; group 2 is the path literal.
	erlGunRE = regexp.MustCompile(
		`gun\s*:\s*(get|post|put|delete|patch|head|options)\s*\(\s*[^,]+,\s*"([^"\n\r]*)"`)

	// hackney:request(get, <<"url">>, ...) / hackney:request(get, "url", ...).
	// Group 1 is the verb; group 2 is the URL (binary or string literal).
	erlHackneyVerbRE = regexp.MustCompile(
		`hackney\s*:\s*request\s*\(\s*([a-z]+)\s*,\s*(?:<<\s*)?"([^"\n\r]*)"`)

	// hackney:get(<<"url">>, ...) / hackney:post("url", ...) — verb in the
	// function name, URL is the first argument (binary or string literal).
	// Group 1 is the verb; group 2 is the URL.
	erlHackneyVerbFnRE = regexp.MustCompile(
		`hackney\s*:\s*(get|post|put|delete|patch|head|options)\s*\(\s*(?:<<\s*)?"([^"\n\r]*)"`)
)

func (e *erlangTestRouteE2EExtractor) Extract(
	_ context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "erlang" {
		return nil, nil
	}
	if !isErlangTestFile(file.Path) {
		return nil, nil
	}
	source := string(file.Content)
	routeCalls := collectErlangTestRouteCalls(source)
	if len(routeCalls) == 0 {
		return nil, nil
	}
	rec := types.EntityRecord{
		Name:       erlangTestSuiteBaseName(file.Path),
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: file.Path,
		Language:   "erlang",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":       "cowboy",
			"provenance":      "INFERRED_FROM_ERLANG_TEST_ROUTE_E2E",
			"e2e_route_calls": strings.Join(routeCalls, "\n"),
		},
	}
	return []types.EntityRecord{rec}, nil
}

// collectErlangTestRouteCalls returns the de-duplicated "VERB route" pairs an
// eunit / common_test file drives by route string (httpc, gun, hackney forms).
// URLs are reduced to their path; built / concatenated / variable URLs are not
// matched by the string-literal regexes and are dropped (honest exclusion).
func collectErlangTestRouteCalls(source string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawURL string) {
		route := normaliseErlangTestRoute(rawURL)
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

	// Track byte ranges already consumed by the verb-tuple form so the bare GET
	// regex doesn't double-count the same httpc:request call's URL.
	consumed := map[int]bool{}
	for _, m := range erlHttpcVerbTupleRE.FindAllStringSubmatchIndex(source, -1) {
		add(source[m[2]:m[3]], source[m[4]:m[5]])
		consumed[m[0]] = true
	}
	for _, m := range erlHttpcBareRE.FindAllStringSubmatchIndex(source, -1) {
		if consumed[m[0]] {
			continue
		}
		add("GET", source[m[2]:m[3]])
	}
	for _, m := range erlGunRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range erlHackneyVerbRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	for _, m := range erlHackneyVerbFnRE.FindAllStringSubmatch(source, -1) {
		add(m[1], m[2])
	}
	return out
}

// normaliseErlangTestRoute reduces a raw URL literal to a path: strips a
// scheme+authority prefix, drops query/fragment, collapses repeated slashes.
// Cowboy path-param placeholders (`:id`) and casing are preserved (the resolver
// wildcards templates and compares case-insensitively).
func normaliseErlangTestRoute(raw string) string {
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

// isErlangTestFile reports whether path looks like an eunit / common_test source
// — a `*_tests.erl` (eunit) or `*_SUITE.erl` (common_test) file, or any `.erl`
// under a `/test/` directory. Keeps the route-hit suite off production
// handlers/routers that merely mention a route.
func isErlangTestFile(path string) bool {
	lp := strings.ToLower(filepath.ToSlash(path))
	if strings.HasSuffix(lp, "_tests.erl") || strings.HasSuffix(lp, "_suite.erl") {
		return true
	}
	if strings.Contains(lp, "/test/") && strings.HasSuffix(lp, ".erl") {
		return true
	}
	return false
}

// erlangTestSuiteBaseName derives a suite label from the test file path
// (`.../todo_handler_tests.erl` → `todo_handler_tests`).
func erlangTestSuiteBaseName(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
