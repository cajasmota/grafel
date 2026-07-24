// test_frameworks.go — Erlang eunit & common_test first-class test-framework
// recognition (#4930 / #4988).
//
// This is the GENERAL extractor-level test-framework recognition, complementary
// to the route-hit test→endpoint linkage in tests_route_e2e.go (#4749, which
// only emits a suite when a test drives an HTTP route by literal path). Here we
// recognise eunit / common_test test files structurally and emit:
//
//   - one test_suite entity per test file (SCOPE.Pattern, subtype="test_suite"),
//     carrying the framework (eunit|common_test) + a TESTS edge to the
//     module-under-test resolved by the naming convention
//     (foo_tests.erl → foo, foo_SUITE.erl → foo);
//   - one test_case entity per discovered test function:
//     eunit:        name_test/0   (a single assertion test)
//     name_test_/0  (a test-generator returning a fixture)
//     common_test:  case(Config)  test-case functions named in all/0 (or in a
//     group), excluding the CT scaffolding callbacks
//     (all/0, groups/0, suite/0, init_per_*/end_per_*).
//
// eunit is recognised by the `-include_lib("eunit/include/eunit.hrl")` include
// or the *_test/*_test_ function-name convention; common_test by the
// `-include_lib("common_test/include/ct.hrl")` include, the `*_SUITE.erl`
// filename, or an `all/0` export. Files with neither signal are a no-op.
//
// Registration key: "custom_erlang_test_frameworks".
package erlang

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_erlang_test_frameworks", &erlangTestFrameworksExtractor{})
}

type erlangTestFrameworksExtractor struct{}

func (e *erlangTestFrameworksExtractor) Language() string {
	return "custom_erlang_test_frameworks"
}

var (
	// erlModuleRE matches the -module(foo). attribute (module-under-test name).
	erlModuleRE = regexp.MustCompile(`(?m)^-module\s*\(\s*([a-z][a-zA-Z0-9_@]*)\s*\)\s*\.`)

	// erlEunitTestFnRE matches an eunit test function head:
	//   name_test() -> ...      (simple test)
	//   name_test_() -> ...     (test generator)
	// Group 1 is the function name; group 2 is "_" for a generator (else "").
	erlEunitTestFnRE = regexp.MustCompile(`(?m)^([a-z][a-zA-Z0-9_@]*_test(_)?)\s*\(\s*\)\s*->`)

	// erlCTCaseFnRE matches a common_test test-case function head taking a
	// single Config argument: case_name(Config) -> ... / case_name(_Config) ->.
	// Group 1 is the case function name.
	erlCTCaseFnRE = regexp.MustCompile(`(?m)^([a-z][a-zA-Z0-9_@]*)\s*\(\s*_?[A-Z][a-zA-Z0-9_]*\s*\)\s*->`)

	// erlEunitIncludeRE / erlCTIncludeRE detect the framework header includes.
	erlEunitIncludeRE = regexp.MustCompile(`-include_lib\s*\(\s*"eunit/include/eunit\.hrl"`)
	erlCTIncludeRE    = regexp.MustCompile(`-include_lib\s*\(\s*"common_test/include/ct\.hrl"`)

	// erlAllExportRE detects an `all/0` export (common_test entry point).
	erlAllExportRE = regexp.MustCompile(`(?m)^all\s*\(\s*\)\s*->`)
)

// ctScaffoldingCallbacks are common_test framework callbacks that are NOT test
// cases (they configure the suite / groups / fixtures) and must be excluded from
// the per-case test entity set.
var ctScaffoldingCallbacks = map[string]bool{
	"all": true, "groups": true, "suite": true,
	"init_per_suite": true, "end_per_suite": true,
	"init_per_group": true, "end_per_group": true,
	"init_per_testcase": true, "end_per_testcase": true,
}

func (e *erlangTestFrameworksExtractor) Extract(
	_ context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "erlang" {
		return nil, nil
	}
	src := string(file.Content)
	framework := detectErlangTestFramework(file.Path, src)
	if framework == "" {
		return nil, nil
	}

	sut := erlangModuleUnderTest(file.Path)
	suiteName := erlangTestSuiteBaseName(file.Path)

	var out []types.EntityRecord

	// Per-case test entities.
	cases := collectErlangTestCases(framework, src)
	for _, c := range cases {
		te := makeErlangTestEntity("test:"+c.name, "test_case", file.Path, c.line)
		te.Properties["framework"] = framework
		te.Properties["test_function"] = c.name
		te.Properties["test_kind"] = c.kind
		if sut != "" {
			te.Properties["module_under_test"] = sut
		}
		out = append(out, te)
	}

	// The suite entity is emitted only when at least one test case was found
	// (no-match no-op for a framework-signal file that has no test functions).
	if len(cases) == 0 {
		return nil, nil
	}

	suite := makeErlangTestEntity("suite:"+suiteName, "test_suite", file.Path, 1)
	suite.Properties["framework"] = framework
	suite.Properties["test_case_count"] = itoa(len(cases))
	if sut != "" {
		suite.Properties["module_under_test"] = sut
		// TESTS edge to the module-under-test, resolved by bare module name
		// (foo_tests → foo / foo_SUITE → foo). FromID is left empty so it
		// defaults to the owning suite entity (mirrors the base extractor's
		// CONTAINS edges).
		suite.Relationships = append(suite.Relationships, types.RelationshipRecord{
			ToID: sut,
			Kind: "TESTS",
			Properties: types.Props{
				{K: "provenance", V: "erlang_test_naming_convention"},
				{K: "test_framework", V: framework},
			},
		})
	}
	out = append(out, suite)

	return out, nil
}

// erlangTestCase is a discovered test function.
type erlangTestCase struct {
	name string
	line int
	kind string // "eunit_test" | "eunit_generator" | "common_test_case"
}

// detectErlangTestFramework returns "eunit", "common_test", or "" when the file
// is not a recognised test file.
func detectErlangTestFramework(path, src string) string {
	lp := strings.ToLower(filepath.ToSlash(path))
	switch {
	case erlCTIncludeRE.MatchString(src), strings.HasSuffix(lp, "_suite.erl"), erlAllExportRE.MatchString(src):
		return "common_test"
	case erlEunitIncludeRE.MatchString(src), strings.HasSuffix(lp, "_tests.erl"):
		return "eunit"
	}
	return ""
}

// collectErlangTestCases returns the test functions for the detected framework.
func collectErlangTestCases(framework, src string) []erlangTestCase {
	var out []erlangTestCase
	seen := map[string]bool{}

	if framework == "eunit" {
		for _, m := range erlEunitTestFnRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if seen[name] {
				continue
			}
			seen[name] = true
			kind := "eunit_test"
			if m[4] >= 0 { // group 2 ("_") matched → generator
				kind = "eunit_generator"
			}
			out = append(out, erlangTestCase{name: name, line: lineCount(src, m[0]), kind: kind})
		}
		return out
	}

	// common_test: single-Config-arg functions, excluding scaffolding callbacks.
	for _, m := range erlCTCaseFnRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if ctScaffoldingCallbacks[name] || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, erlangTestCase{name: name, line: lineCount(src, m[0]), kind: "common_test_case"})
	}
	return out
}

// erlangModuleUnderTest derives the module-under-test name from a test file's
// path by the naming convention: foo_tests.erl → foo, foo_SUITE.erl → foo.
// Returns "" when the convention does not apply (no stripped suffix).
func erlangModuleUnderTest(path string) string {
	base := filepath.Base(filepath.ToSlash(path))
	base = strings.TrimSuffix(base, filepath.Ext(base)) // drop .erl
	switch {
	case strings.HasSuffix(base, "_tests"):
		return strings.TrimSuffix(base, "_tests")
	case strings.HasSuffix(base, "_SUITE"):
		return strings.TrimSuffix(base, "_SUITE")
	case strings.HasSuffix(base, "_suite"):
		return strings.TrimSuffix(base, "_suite")
	}
	return ""
}

// makeErlangTestEntity builds a SCOPE.Pattern test entity (test_suite /
// test_case) for the erlang test-framework extractor.
func makeErlangTestEntity(name, subtype, filePath string, line int) types.EntityRecord {
	e := types.EntityRecord{
		Name:       name,
		Kind:       string(types.EntityKindPattern),
		Subtype:    subtype,
		SourceFile: filePath,
		Language:   "erlang",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"signal": "testing",
		},
	}
	e.ID = e.ComputeID()
	return e
}

func lineCount(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
