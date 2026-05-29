// py_hypothesis_targets.go — Hypothesis @given test target extraction — #3078.
//
// Extracts test targets from Python files that use the Hypothesis property-based
// testing library.  The distinguishing signal is the @given decorator applied to
// a test function:
//
//	from hypothesis import given
//	from hypothesis import strategies as st
//
//	@given(st.integers())
//	def test_round_trip(n):
//	    assert decode(encode(n)) == n
//
// For each @given-decorated function ApplyHypothesisTargets emits a TESTS edge
// whose FromID is the test-function structural ref so that the resolver binds it
// to the actual SCOPE.Operation test-function entity.
//
// dependency_graph: not_applicable — Hypothesis is a test library; it does not
// produce dependency-graph edges between production entities.
//
// Refs #3078.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Regexes
// ---------------------------------------------------------------------------

// hypothesisGivenRE matches the @given decorator line (with or without the
// `hypothesis.` namespace prefix).
var hypothesisGivenRE = regexp.MustCompile(
	`(?m)^[ \t]*@(?:hypothesis\.)?given\s*\(`,
)

// hypothesisFuncNameRE matches a `def <name>(` header on the line(s) following
// a @given decorator.  We scan forward up to 5 lines to skip any intermediate
// decorator lines between @given and def.
var hypothesisFuncNameRE = regexp.MustCompile(
	`(?m)^[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`,
)

// ---------------------------------------------------------------------------
// ApplyHypothesisTargets
// ---------------------------------------------------------------------------

// ApplyHypothesisTargets returns TESTS edge stubs for every @given-decorated
// test function found across the supplied Python source files.
//
// Parameters:
//
//	paths      — repo-relative paths of all indexed files.
//	fileReader — returns raw source bytes for a path; nil bytes → skip.
//
// The returned records use stub FromID/ToID in the same form as
// ApplyTestsViaImports so that the downstream resolver binds them correctly.
func ApplyHypothesisTargets(
	paths []string,
	fileReader NestedURLConfFileReader,
) []types.RelationshipRecord {
	if fileReader == nil {
		return nil
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{}

	for _, p := range paths {
		if !isPyTestFilePath(p) {
			continue
		}
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		// Quick gate: file must reference @given.
		if !strings.Contains(src, "@given") && !strings.Contains(src, "hypothesis") {
			continue
		}

		// Find all @given decorator positions.
		for _, loc := range hypothesisGivenRE.FindAllStringIndex(src, -1) {
			decoratorEnd := loc[1]
			// Scan forward (up to ~200 chars) for the def line.
			window := src[decoratorEnd:]
			if len(window) > 400 {
				window = window[:400]
			}
			fm := hypothesisFuncNameRE.FindStringSubmatch(window)
			if fm == nil {
				continue
			}
			funcName := fm[1]
			if funcName == "" {
				continue
			}

			key := p + "|" + funcName
			if seen[key] {
				continue
			}
			seen[key] = true

			out = append(out, types.RelationshipRecord{
				// FromID: structural test-function ref (same form as tests_imports.go).
				FromID: "scope:operation:" + p + "#" + funcName,
				// ToID: self-referential stub — the test function IS the target;
				// the resolver maps it to the entity via the file+name index.
				ToID: "scope:operation:" + p + "#" + funcName,
				Kind: "TESTS",
				Properties: map[string]string{
					"via":            "hypothesis_given",
					"test_file":      p,
					"test_function":  funcName,
					"pattern_type":   "hypothesis_given_decorator",
					"test_framework": "hypothesis",
					"confidence":     "high",
				},
			})
		}
	}
	return out
}
