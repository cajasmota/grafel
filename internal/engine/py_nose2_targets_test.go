// Tests for ApplyNose2Targets — #3078.
package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const nose2TestSrc = `import nose2
import unittest


class AdditionTests(unittest.TestCase):
    def setUp(self):
        self.result = 0

    def test_add_positive(self):
        self.assertEqual(1 + 1, 2)

    def test_add_zero(self):
        self.assertEqual(0 + 0, self.result)

    def tearDown(self):
        pass


class SubtractionTests(unittest.TestCase):
    def test_subtract(self):
        self.assertEqual(5 - 3, 2)


if __name__ == '__main__':
    nose2.main()
`

const nose2NoImportSrc = `import unittest

class PurePythonTests(unittest.TestCase):
    def test_something(self):
        self.assertTrue(True)
`

// ---------------------------------------------------------------------------
// TestApplyNose2Targets_HappyPath
// ---------------------------------------------------------------------------

func TestApplyNose2Targets_HappyPath(t *testing.T) {
	paths := []string{"tests/test_math.py"}
	reader := func(p string) []byte {
		if p == "tests/test_math.py" {
			return []byte(nose2TestSrc)
		}
		return nil
	}

	edges := ApplyNose2Targets(paths, reader)

	if len(edges) == 0 {
		t.Fatal("expected at least one TESTS edge for nose2 TestCase methods, got none")
	}

	methods := map[string]bool{}
	for _, e := range edges {
		if e.Kind != "TESTS" {
			t.Errorf("unexpected Kind %q (want TESTS)", e.Kind)
		}
		if e.Properties["test_framework"] != "nose2" {
			t.Errorf("expected test_framework=nose2, got %q", e.Properties["test_framework"])
		}
		if e.Properties["confidence"] != "high" {
			t.Errorf("expected confidence=high, got %q", e.Properties["confidence"])
		}
		methods[e.Properties["test_function"]] = true
	}

	// setUp and tearDown in AdditionTests should be extracted.
	if !methods["setUp"] {
		t.Errorf("expected TESTS edge for setUp; got methods=%v", methods)
	}
	if !methods["test_add_positive"] {
		t.Errorf("expected TESTS edge for test_add_positive; got methods=%v", methods)
	}
	if !methods["test_add_zero"] {
		t.Errorf("expected TESTS edge for test_add_zero; got methods=%v", methods)
	}
	if !methods["test_subtract"] {
		t.Errorf("expected TESTS edge for test_subtract; got methods=%v", methods)
	}
}

// TestApplyNose2Targets_NoNose2Import verifies that a file using unittest.TestCase
// WITHOUT importing nose2 is NOT attributed to the nose2 extractor.
func TestApplyNose2Targets_NoNose2Import(t *testing.T) {
	paths := []string{"tests/test_pure.py"}
	reader := func(p string) []byte { return []byte(nose2NoImportSrc) }

	edges := ApplyNose2Targets(paths, reader)
	if len(edges) != 0 {
		t.Errorf("file without nose2 import must not produce nose2 edges; got %d: %+v", len(edges), edges)
	}
}

// TestApplyNose2Targets_NilReader ensures nil fileReader returns empty.
func TestApplyNose2Targets_NilReader(t *testing.T) {
	edges := ApplyNose2Targets([]string{"tests/test_math.py"}, nil)
	if len(edges) != 0 {
		t.Errorf("nil reader must return empty; got %d edges", len(edges))
	}
}

// TestApplyNose2Targets_NonTestFile ensures non-test files are skipped even
// when they import nose2.
func TestApplyNose2Targets_NonTestFile(t *testing.T) {
	paths := []string{"app/runner.py"} // not a test file by naming convention
	reader := func(p string) []byte { return []byte(nose2TestSrc) }

	edges := ApplyNose2Targets(paths, reader)
	if len(edges) != 0 {
		t.Errorf("non-test file must not produce edges; got %d edges", len(edges))
	}
}

// TestApplyNose2Targets_TestCaseAlias covers `from unittest import TestCase`
// combined with a nose2 import.
func TestApplyNose2Targets_TestCaseAlias(t *testing.T) {
	const aliasSrc = `import nose2
from unittest import TestCase

class MyTests(TestCase):
    def test_alias_works(self):
        self.assertTrue(True)
`
	paths := []string{"tests/test_alias.py"}
	reader := func(p string) []byte { return []byte(aliasSrc) }

	edges := ApplyNose2Targets(paths, reader)
	found := false
	for _, e := range edges {
		if e.Properties["test_function"] == "test_alias_works" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TESTS edge for test_alias_works with aliased TestCase; got %+v", edges)
	}
}
