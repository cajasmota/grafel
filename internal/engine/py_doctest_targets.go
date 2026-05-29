// py_doctest_targets.go — doctest target extraction — #3078.
//
// Extracts test targets from Python files that use the stdlib doctest module.
// The most common patterns are:
//
//  1. Inline doctest invocation in __main__ guard:
//
//	if __name__ == "__main__":
//	    import doctest
//	    doctest.testmod()
//
//  2. Explicit testfile/testmod calls in a test suite:
//
//	import doctest
//	doctest.testmod(mymodule)
//	doctest.testfile("myfile.txt")
//
//  3. pytest doctest plugin detection (conftest.py / pytest.ini):
//
//	--doctest-modules
//
// For each source file where doctest usage is detected ApplyDoctestTargets emits
// a TESTS edge indicating the file exercises its own docstrings (self-referential
// edge from the module stub to itself).
//
// dependency_graph: not_applicable — doctest is a stdlib test harness; it has
// no dependency-graph semantics between production entities.
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

// doctestTestmodRE matches `doctest.testmod(` or `doctest.testfile(`.
var doctestTestmodRE = regexp.MustCompile(
	`\bdoctest\s*\.\s*(?:testmod|testfile|run_docstring_examples)\s*\(`,
)

// doctestImportRE matches `import doctest` or `from doctest import`.
var doctestImportRE = regexp.MustCompile(
	`(?m)^\s*(?:import\s+doctest|from\s+doctest\s+import)`,
)

// ---------------------------------------------------------------------------
// ApplyDoctestTargets
// ---------------------------------------------------------------------------

// ApplyDoctestTargets returns TESTS edge stubs for every Python source file
// that contains a doctest.testmod / doctest.testfile invocation.
//
// Parameters:
//
//	paths      — repo-relative paths of all indexed files.
//	fileReader — returns raw source bytes for a path; nil bytes → skip.
func ApplyDoctestTargets(
	paths []string,
	fileReader NestedURLConfFileReader,
) []types.RelationshipRecord {
	if fileReader == nil {
		return nil
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{}

	for _, p := range paths {
		if !strings.HasSuffix(strings.ToLower(p), ".py") {
			continue
		}
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		// Gate: file must import doctest.
		if !doctestImportRE.MatchString(src) {
			continue
		}

		// Detect a testmod/testfile call.
		matches := doctestTestmodRE.FindAllStringIndex(src, -1)
		if len(matches) == 0 {
			continue
		}

		for _, loc := range matches {
			callPos := loc[0]
			// Determine which invocation variant was matched.
			callText := src[loc[0]:loc[1]]
			invocation := "testmod"
			if strings.Contains(callText, "testfile") {
				invocation = "testfile"
			} else if strings.Contains(callText, "run_docstring_examples") {
				invocation = "run_docstring_examples"
			}

			// Enclosing function (if any) — used for the test_function property.
			enclosingFunc := enclosingPyTestFunc(src, callPos)
			if enclosingFunc == "" {
				enclosingFunc = "__module__"
			}

			key := p + "|" + invocation + "|" + enclosingFunc
			if seen[key] {
				continue
			}
			seen[key] = true

			out = append(out, types.RelationshipRecord{
				// Self-referential: the file tests its own docstrings.
				FromID: "scope:operation:" + p + "#" + enclosingFunc,
				ToID:   "scope:operation:" + p + "#" + enclosingFunc,
				Kind:   "TESTS",
				Properties: map[string]string{
					"via":            "doctest_" + invocation,
					"test_file":      p,
					"test_function":  enclosingFunc,
					"pattern_type":   "doctest_" + invocation,
					"test_framework": "doctest",
					"confidence":     "medium",
				},
			})
		}
	}
	return out
}
