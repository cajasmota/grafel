// Tests for ApplyDoctestTargets — #3078.
package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// doctestModuleSrc simulates a module that invokes doctest.testmod() in its
// __main__ guard — the canonical doctest pattern.
const doctestModuleSrc = `"""Module with docstring tests.

>>> add(1, 2)
3
"""

import doctest


def add(a, b):
    """Return the sum of a and b.

    >>> add(2, 3)
    5
    """
    return a + b


if __name__ == "__main__":
    doctest.testmod()
`

// doctestTestFileSrc uses doctest.testfile to run an external text file.
const doctestTestFileSrc = `import doctest
import unittest


def load_tests(loader, tests, ignore):
    tests.addTests(doctest.testfile("mymodule.txt"))
    return tests
`

// doctestNoInvocationSrc imports doctest but never calls testmod/testfile.
const doctestNoInvocationSrc = `import doctest

# we might use doctest.DocTestSuite but never call testmod
suite = doctest.DocTestSuite()
`

// ---------------------------------------------------------------------------
// TestApplyDoctestTargets_TestmodPattern
// ---------------------------------------------------------------------------

func TestApplyDoctestTargets_TestmodPattern(t *testing.T) {
	paths := []string{"mymodule.py"}
	reader := func(p string) []byte {
		if p == "mymodule.py" {
			return []byte(doctestModuleSrc)
		}
		return nil
	}

	edges := ApplyDoctestTargets(paths, reader)

	if len(edges) == 0 {
		t.Fatal("expected at least one TESTS edge for doctest.testmod(), got none")
	}
	for _, e := range edges {
		if e.Kind != "TESTS" {
			t.Errorf("unexpected Kind %q (want TESTS)", e.Kind)
		}
		if e.Properties["test_framework"] != "doctest" {
			t.Errorf("expected test_framework=doctest, got %q", e.Properties["test_framework"])
		}
		if e.Properties["test_file"] != "mymodule.py" {
			t.Errorf("expected test_file=mymodule.py, got %q", e.Properties["test_file"])
		}
	}
}

// TestApplyDoctestTargets_TestfilePattern verifies that doctest.testfile() is
// also detected.
func TestApplyDoctestTargets_TestfilePattern(t *testing.T) {
	paths := []string{"tests/test_docs.py"}
	reader := func(p string) []byte { return []byte(doctestTestFileSrc) }

	edges := ApplyDoctestTargets(paths, reader)
	if len(edges) == 0 {
		t.Fatal("expected TESTS edge for doctest.testfile(), got none")
	}
	found := false
	for _, e := range edges {
		if e.Properties["test_framework"] == "doctest" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected doctest TESTS edge; got %+v", edges)
	}
}

// TestApplyDoctestTargets_NoInvocation verifies that merely importing doctest
// without calling testmod/testfile produces no edges.
func TestApplyDoctestTargets_NoInvocation(t *testing.T) {
	paths := []string{"mymodule.py"}
	reader := func(p string) []byte { return []byte(doctestNoInvocationSrc) }

	edges := ApplyDoctestTargets(paths, reader)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges when testmod/testfile are not called; got %d: %+v", len(edges), edges)
	}
}

// TestApplyDoctestTargets_NilReader ensures nil fileReader returns empty.
func TestApplyDoctestTargets_NilReader(t *testing.T) {
	edges := ApplyDoctestTargets([]string{"mymodule.py"}, nil)
	if len(edges) != 0 {
		t.Errorf("nil reader must return empty; got %d edges", len(edges))
	}
}

// TestApplyDoctestTargets_NonPyFile ensures non-.py files are skipped.
func TestApplyDoctestTargets_NonPyFile(t *testing.T) {
	paths := []string{"mymodule.txt"}
	reader := func(p string) []byte { return []byte(doctestModuleSrc) }

	edges := ApplyDoctestTargets(paths, reader)
	if len(edges) != 0 {
		t.Errorf("non-.py file must not produce edges; got %d edges", len(edges))
	}
}
