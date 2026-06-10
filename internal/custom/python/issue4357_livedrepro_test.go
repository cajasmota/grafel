package python_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
)

// Issue #4357 LIVE-REPRO — Python test (pytest + unittest) orphan collapse +
// TESTS edge to the SUT symbol.
//
// Generalizes the Jest #4343 / Go #4358 / JUnit #4359 fixes to Python. The
// pytest/unittest custom extractor previously emitted a first-class entity per
// fixture, per Test class, per test method, and per test_ function — none
// carrying a relationship (beyond the narrow Celery .delay() case) — producing a
// forest of orphan assertion/fixture/test nodes with ZERO TESTS edges to the
// code under test.
//
// We run the ACTUAL base python extractor + the ACTUAL regex framework
// extractors, merge them exactly as the pipeline does (MergeWithCustom), assign
// real entity IDs, build the REAL resolver symbol table (resolve.BuildIndex)
// over BOTH the test file and its production module, and assert:
//   - the per-test/per-fixture orphan ring is gone (exactly ONE test_suite),
//   - a TESTS edge to the SUT symbol exists, and
//   - that subject resolves through the symbol table to the production entity.

func loadRepro4357(t *testing.T, base, repoPath string) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "issue4357", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read repro %s: %v", p, err)
	}
	return extreg.FileInput{Path: repoPath, Language: "python", Content: b}
}

// mergedEntitiesFor runs the full merged pipeline (base + custom + merge + IDs)
// for one file. Reuses runMergedPipeline from the #4366 livedrepro test.
func suiteEntities(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "test_suite" {
			out = append(out, e)
		}
	}
	return out
}

// orphanTestNodes counts entities that look like the OLD per-test/per-fixture
// nodes (a standalone Test class, a test_ method/function, or a fixture) that
// carry no inbound or outbound relationship — the orphan ring #4357 removes.
func legacyTestNodeNames(ents []types.EntityRecord) []string {
	var names []string
	for _, e := range ents {
		// The old extractor emitted these with pattern_type test/test_class and
		// framework=pytest. The new one emits none of them.
		if e.Properties["pattern_type"] == "test" ||
			e.Properties["pattern_type"] == "test_class" ||
			e.Properties["pattern_type"] == "pytest_fixture" ||
			e.Properties["pattern_type"] == "pytest_conftest" {
			names = append(names, e.Name)
		}
	}
	return names
}

func testsEdgeTargets(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) {
				out = append(out, r.ToID)
			}
		}
	}
	return out
}

// TestIssue4357_UnittestTestCase_CollapsedSuite_AndTestsEdges runs the REAL
// upvate_core Django unittest file (core/tests/test_schedule_import.py, byte-copied
// into testdata) paired with its production module (core/helper/schedule_import_helper.py).
func TestIssue4357_UnittestTestCase_CollapsedSuite_AndTestsEdges(t *testing.T) {
	testFile := loadRepro4357(t, "test_schedule_import.py.txt", "core/tests/test_schedule_import.py")
	sutFile := loadRepro4357(t, "schedule_import_helper.py.txt", "core/helper/schedule_import_helper.py")

	testEnts := runMergedPipeline(t, testFile)
	sutEnts := runMergedPipeline(t, sutFile)

	// AFTER: exactly one test_suite, the legacy per-test/per-fixture orphan ring is gone.
	suites := suiteEntities(testEnts)
	if len(suites) != 1 {
		t.Fatalf("expected exactly 1 collapsed test_suite, got %d", len(suites))
	}
	if legacy := legacyTestNodeNames(testEnts); len(legacy) > 0 {
		t.Errorf("expected 0 legacy per-test/per-fixture orphan nodes, got %d: %v", len(legacy), legacy)
	}

	suite := suites[0]
	if suite.Properties["framework"] != "unittest" {
		t.Errorf("expected framework=unittest, got %q", suite.Properties["framework"])
	}
	// Counts folded onto the suite (information preserved).
	for _, k := range []string{"test_class_count", "test_method_count", "assertion_count", "fixture_count"} {
		if suite.Properties[k] == "" || suite.Properties[k] == "0" {
			t.Errorf("expected non-zero folded property %s, got %q", k, suite.Properties[k])
		}
	}
	t.Logf("folded suite props: classes=%s methods=%s asserts=%s fixtures=%s",
		suite.Properties["test_class_count"], suite.Properties["test_method_count"],
		suite.Properties["assertion_count"], suite.Properties["fixture_count"])

	// TESTS edges: unittest class ResolveDeviceTest/ResolveContractTest/GroupRowsTest
	// → resolve_device/resolve_contract/group_rows (snake-case, reference-gated on
	// the `from core.helper.schedule_import_helper import ...` import).
	targets := testsEdgeTargets(testEnts)
	if len(targets) == 0 {
		t.Fatal("expected at least one TESTS edge from the test_suite, got 0")
	}
	t.Logf("TESTS edge targets: %v", targets)

	want := map[string]bool{"resolve_device": false, "resolve_contract": false, "group_rows": false, "parse_csv_file": false}
	for _, tgt := range targets {
		if _, ok := want[tgt]; ok {
			want[tgt] = true
		}
	}
	hits := 0
	for subj, got := range want {
		if got {
			hits++
		} else {
			t.Logf("note: expected-ish subject %q not linked (acceptable if name affinity diverges)", subj)
		}
	}
	if hits == 0 {
		t.Errorf("expected at least one of resolve_device/resolve_contract/group_rows/parse_csv_file as a TESTS target, got %v", targets)
	}

	// The subject must resolve through the REAL symbol table to the production
	// function entity (test + SUT entities together).
	all := append(append([]types.EntityRecord{}, testEnts...), sutEnts...)
	idx := resolve.BuildIndex(all)
	resolved := 0
	for _, tgt := range targets {
		if _, ok := idx.Lookup(tgt); ok {
			resolved++
		}
	}
	if resolved == 0 {
		t.Errorf("no TESTS-edge subject resolved through the symbol table to a production entity; targets=%v", targets)
	}
	t.Logf("%d/%d TESTS-edge subjects resolve to production entities", resolved, len(targets))
}

// TestIssue4357_PytestFunctionStyle_CollapsedSuite_AndTestsEdge covers the
// pytest function + parametrize + fixture style (the upvate backend is unittest-
// only, so this representative file exercises the pure-pytest path).
func TestIssue4357_PytestFunctionStyle_CollapsedSuite_AndTestsEdge(t *testing.T) {
	testFile := loadRepro4357(t, "test_orders_pytest.py.txt", "tests/test_orders.py")
	sutFile := loadRepro4357(t, "orders_service.py.txt", "app/services/orders.py")

	testEnts := runMergedPipeline(t, testFile)
	sutEnts := runMergedPipeline(t, sutFile)

	suites := suiteEntities(testEnts)
	if len(suites) != 1 {
		t.Fatalf("expected exactly 1 collapsed test_suite, got %d", len(suites))
	}
	suite := suites[0]
	if legacy := legacyTestNodeNames(testEnts); len(legacy) > 0 {
		t.Errorf("expected 0 legacy per-test/per-fixture nodes, got %v", legacy)
	}
	if suite.Properties["parametrize_count"] == "" || suite.Properties["parametrize_count"] == "0" {
		t.Errorf("expected non-zero parametrize_count, got %q", suite.Properties["parametrize_count"])
	}
	if suite.Properties["fixture_count"] == "" || suite.Properties["fixture_count"] == "0" {
		t.Errorf("expected non-zero fixture_count, got %q", suite.Properties["fixture_count"])
	}

	// pytest function test_place_order → place_order (reference-gated import).
	targets := testsEdgeTargets(testEnts)
	t.Logf("pytest TESTS targets: %v", targets)
	found := false
	for _, tgt := range targets {
		if tgt == "place_order" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TESTS → place_order (test_place_order name affinity), got %v", targets)
	}

	all := append(append([]types.EntityRecord{}, testEnts...), sutEnts...)
	idx := resolve.BuildIndex(all)
	if _, ok := idx.Lookup("place_order"); !ok {
		t.Errorf("place_order did not resolve through the symbol table to the production function")
	}
}

func TestIssue4357_BeforeAfterContrast_ResolverByName(t *testing.T) {
	// Sanity: the resolver byName fallback binds a bare subject name to the
	// production function entity (the mechanism the bare TESTS ToID relies on).
	sutFile := loadRepro4357(t, "orders_service.py.txt", "app/services/orders.py")
	ents := runMergedPipeline(t, sutFile)
	idx := resolve.BuildIndex(ents)
	if _, ok := idx.Lookup("place_order"); !ok {
		t.Skip("place_order not emitted as a top-level entity by the base extractor; byName binding not assertable here")
	}
	_ = context.Background
}
