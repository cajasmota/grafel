// py_nose2_targets.go — nose2 test target extraction — #3078.
//
// Extracts test targets from Python files that use nose2 (the successor to the
// deprecated nose test runner).  nose2 discovers tests via the unittest.TestCase
// subclassing pattern, so the distinguishing signals are:
//
//	import nose2
//	import unittest
//	class MyTests(unittest.TestCase):
//	    def test_something(self):
//	        ...
//
// and optionally a nose2.cfg / unittest.cfg config file.
//
// ApplyNose2Targets emits a TESTS edge for every test method inside a
// unittest.TestCase subclass, tagged test_framework=nose2.
//
// dependency_graph: not_applicable — nose2 is a test runner; it does not
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

// nose2ClassRE matches a class that subclasses unittest.TestCase (directly or
// via an alias).  We tolerate various spellings:
//
//	class Foo(unittest.TestCase):
//	class Foo(TestCase):          # imported as: from unittest import TestCase
//	class Foo(nose2.tools.such.A): (less common, skip for now)
var nose2ClassRE = regexp.MustCompile(
	`(?m)^class\s+(\w+)\s*\(\s*(?:unittest\.)?TestCase\s*\)`,
)

// nose2MethodRE matches a test method (def test_* or def setUp/tearDown) inside
// an indented class body.
var nose2MethodRE = regexp.MustCompile(
	`(?m)^([ \t]+)(?:async\s+)?def\s+(test_\w+|setUp|tearDown|setUpClass|tearDownClass)\s*\(`,
)

// ---------------------------------------------------------------------------
// ApplyNose2Targets
// ---------------------------------------------------------------------------

// ApplyNose2Targets returns TESTS edge stubs for every test method found inside
// unittest.TestCase subclasses in the supplied Python source files.  It is
// designed to complement the existing unittest detection (which covers vanilla
// unittest usage) by adding the nose2 framework attribution.
//
// Parameters:
//
//	paths      — repo-relative paths of all indexed files.
//	fileReader — returns raw source bytes for a path; nil bytes → skip.
func ApplyNose2Targets(
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

		// Quick gate: must contain a TestCase subclass.
		if !strings.Contains(src, "TestCase") {
			continue
		}

		// Check that this file uses nose2 (import nose2 or nose2.cfg is present).
		// We accept any file that uses TestCase subclassing AND either imports nose2
		// or is in a project with a nose2.cfg.  For per-file detection we check for
		// an explicit nose2 import; test files that only use unittest are attributed
		// to test.unittest, not test.nose2.
		isNose2 := strings.Contains(src, "import nose2") ||
			strings.Contains(src, "nose2.main") ||
			strings.Contains(src, "from nose2")

		if !isNose2 {
			continue
		}

		// Find all TestCase subclass positions.
		classMatches := nose2ClassRE.FindAllStringSubmatchIndex(src, -1)
		for ci, cm := range classMatches {
			classStart := cm[0]
			// Determine class body end: next class at same indentation or EOF.
			classEnd := len(src)
			if ci+1 < len(classMatches) {
				classEnd = classMatches[ci+1][0]
			}
			classBody := src[classStart:classEnd]
			className := src[cm[2]:cm[3]]

			// Find all test methods in this class body.
			for _, mm := range nose2MethodRE.FindAllStringSubmatch(classBody, -1) {
				methodName := mm[2]
				// Qualified name: ClassName.method_name
				qualName := className + "." + methodName
				key := p + "|" + qualName
				if seen[key] {
					continue
				}
				seen[key] = true

				out = append(out, types.RelationshipRecord{
					FromID: "scope:operation:" + p + "#" + methodName,
					ToID:   "scope:operation:" + p + "#" + methodName,
					Kind:   "TESTS",
					Properties: map[string]string{
						"via":            "nose2_testcase",
						"test_file":      p,
						"test_function":  methodName,
						"test_class":     className,
						"pattern_type":   "nose2_unittest_testcase",
						"test_framework": "nose2",
						"confidence":     "high",
					},
				})
			}
		}
	}
	return out
}
