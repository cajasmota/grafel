// Tests for ApplyHypothesisTargets — #3078.
package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const hypothesisTestSrc = `from hypothesis import given, settings
from hypothesis import strategies as st

@given(st.integers(min_value=0, max_value=100))
def test_encode_decode(n):
    assert decode(encode(n)) == n

@given(st.text())
def test_round_trip_str(s):
    assert decode_str(encode_str(s)) == s

def test_plain_no_given():
    # not decorated with @given — should NOT produce a TESTS edge
    assert True
`

const hypothesisNamespacedSrc = `import hypothesis

@hypothesis.given(hypothesis.strategies.integers())
def test_namespaced_given(n):
    assert n >= 0
`

// ---------------------------------------------------------------------------
// TestApplyHypothesisTargets_HappyPath
// ---------------------------------------------------------------------------

func TestApplyHypothesisTargets_HappyPath(t *testing.T) {
	paths := []string{"tests/test_codec.py"}
	reader := func(p string) []byte {
		if p == "tests/test_codec.py" {
			return []byte(hypothesisTestSrc)
		}
		return nil
	}

	edges := ApplyHypothesisTargets(paths, reader)

	if len(edges) < 2 {
		t.Fatalf("expected at least 2 TESTS edges for @given functions, got %d: %+v", len(edges), edges)
	}

	funcs := map[string]bool{}
	for _, e := range edges {
		if e.Kind != "TESTS" {
			t.Errorf("unexpected Kind %q (want TESTS)", e.Kind)
		}
		if e.Properties["test_framework"] != "hypothesis" {
			t.Errorf("expected test_framework=hypothesis, got %q", e.Properties["test_framework"])
		}
		if e.Properties["confidence"] != "high" {
			t.Errorf("expected confidence=high, got %q", e.Properties["confidence"])
		}
		funcs[e.Properties["test_function"]] = true
	}

	if !funcs["test_encode_decode"] {
		t.Errorf("expected TESTS edge for test_encode_decode; got funcs=%v", funcs)
	}
	if !funcs["test_round_trip_str"] {
		t.Errorf("expected TESTS edge for test_round_trip_str; got funcs=%v", funcs)
	}
	// Plain function without @given must not appear.
	if funcs["test_plain_no_given"] {
		t.Errorf("test_plain_no_given must NOT produce a TESTS edge (no @given decorator)")
	}
}

// TestApplyHypothesisTargets_NamespacedDecorator verifies that
// `@hypothesis.given(...)` (with the full module prefix) is also detected.
func TestApplyHypothesisTargets_NamespacedDecorator(t *testing.T) {
	paths := []string{"tests/test_ns.py"}
	reader := func(p string) []byte { return []byte(hypothesisNamespacedSrc) }

	edges := ApplyHypothesisTargets(paths, reader)
	if len(edges) == 0 {
		t.Fatal("expected at least one TESTS edge for @hypothesis.given, got none")
	}
	found := false
	for _, e := range edges {
		if e.Properties["test_function"] == "test_namespaced_given" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TESTS edge for test_namespaced_given; got %+v", edges)
	}
}

// TestApplyHypothesisTargets_NilReader ensures nil fileReader returns empty.
func TestApplyHypothesisTargets_NilReader(t *testing.T) {
	edges := ApplyHypothesisTargets([]string{"tests/test_codec.py"}, nil)
	if len(edges) != 0 {
		t.Errorf("nil reader must return empty; got %d edges", len(edges))
	}
}

// TestApplyHypothesisTargets_NonTestFile ensures non-test files are skipped.
func TestApplyHypothesisTargets_NonTestFile(t *testing.T) {
	paths := []string{"app/codec.py"} // not a test file
	reader := func(p string) []byte { return []byte(hypothesisTestSrc) }

	edges := ApplyHypothesisTargets(paths, reader)
	if len(edges) != 0 {
		t.Errorf("non-test file must not produce edges; got %d edges", len(edges))
	}
}

// TestApplyHypothesisTargets_Dedup ensures the same (file, func) pair produces
// only one edge even when @given appears multiple times (e.g. stacked decorators).
func TestApplyHypothesisTargets_Dedup(t *testing.T) {
	const stackedSrc = `from hypothesis import given, settings
from hypothesis import strategies as st

@settings(max_examples=200)
@given(st.integers())
def test_stacked(n):
    assert n >= 0
`
	paths := []string{"tests/test_stacked.py"}
	reader := func(p string) []byte { return []byte(stackedSrc) }

	edges := ApplyHypothesisTargets(paths, reader)
	count := 0
	for _, e := range edges {
		if e.Properties["test_function"] == "test_stacked" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 edge for test_stacked (dedup), got %d", count)
	}
}
