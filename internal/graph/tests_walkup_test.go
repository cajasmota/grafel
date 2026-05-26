package graph

import (
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// DeriveTestsWalkUp unit tests
// ─────────────────────────────────────────────────────────────────────────────

// makeWalkUpFixture builds the canonical scenario described in the task:
//
//	test_fn  --TESTS-->  helper_op  <--CALLS--  viewset_method
//
// Expected: DeriveTestsWalkUp emits a derived TESTS edge
// test_fn → viewset_method with derived=helper:<helperID>.
func makeWalkUpFixture() *Document {
	return makeDoc(
		[]Entity{
			// Production: helper operation (private, called by viewset)
			{ID: "helper1", Name: "_calculate_total", Kind: "SCOPE.Operation",
				SourceFile: "orders/service.py"},
			// Production: viewset method that calls the helper
			{ID: "viewset1", Name: "InspectionViewSet.create_deficiency", Kind: "SCOPE.Operation",
				SourceFile: "orders/views.py"},
			// Test entity
			{ID: "test1", Name: "test_calculate_total", Kind: "SCOPE.Operation",
				SourceFile: "tests/test_service.py"},
		},
		[]Relationship{
			// testmap direct: test → helper
			{ID: "t1", FromID: "test1", ToID: "helper1", Kind: "TESTS"},
			// CALLS: viewset calls the helper
			{ID: "c1", FromID: "viewset1", ToID: "helper1", Kind: "CALLS"},
		},
	)
}

// TestDeriveTestsWalkUp_BasicScenario verifies that the canonical
// test → helper → viewset chain produces a derived TESTS edge.
func TestDeriveTestsWalkUp_BasicScenario(t *testing.T) {
	t.Parallel()
	doc := makeWalkUpFixture()
	stats := DeriveTestsWalkUp(doc)

	if stats.DerivedEdges != 1 {
		t.Errorf("DerivedEdges want 1, got %d", stats.DerivedEdges)
	}
	if stats.HelperTargets != 1 {
		t.Errorf("HelperTargets want 1, got %d", stats.HelperTargets)
	}
	if stats.SkippedHighFanIn != 0 {
		t.Errorf("SkippedHighFanIn want 0, got %d", stats.SkippedHighFanIn)
	}

	// Verify the derived edge exists in the document.
	var found bool
	for _, rel := range doc.Relationships {
		if rel.FromID == "test1" && rel.ToID == "viewset1" && rel.Kind == "TESTS" {
			found = true
			if rel.Properties["source"] != "tests-walkup" {
				t.Errorf("derived edge source want tests-walkup, got %q", rel.Properties["source"])
			}
			if rel.Properties["confidence"] != "0.7" {
				t.Errorf("derived edge confidence want 0.7, got %q", rel.Properties["confidence"])
			}
			if !containsSubstring(rel.Properties["derived"], "helper:") {
				t.Errorf("derived edge property want 'helper:<id>', got %q", rel.Properties["derived"])
			}
		}
	}
	if !found {
		t.Error("derived TESTS edge test1 → viewset1 not found in doc.Relationships")
	}
}

// TestDeriveTestsWalkUp_NoDerivedWhenNoCallers verifies that when the TESTS
// target has no inbound CALLS, no derived edge is emitted.
func TestDeriveTestsWalkUp_NoDerivedWhenNoCallers(t *testing.T) {
	t.Parallel()
	doc := makeDoc(
		[]Entity{
			{ID: "fn1", Name: "standalone_fn", Kind: "SCOPE.Operation", SourceFile: "lib.py"},
			{ID: "t1", Name: "test_standalone", Kind: "SCOPE.Operation", SourceFile: "tests/test_lib.py"},
		},
		[]Relationship{
			{ID: "r1", FromID: "t1", ToID: "fn1", Kind: "TESTS"},
			// No CALLS edges — fn1 is not a helper called by anyone.
		},
	)
	stats := DeriveTestsWalkUp(doc)
	if stats.DerivedEdges != 0 {
		t.Errorf("DerivedEdges want 0 (no callers), got %d", stats.DerivedEdges)
	}
	// Relationship count should be unchanged.
	if len(doc.Relationships) != 1 {
		t.Errorf("doc.Relationships len want 1 (unchanged), got %d", len(doc.Relationships))
	}
}

// TestDeriveTestsWalkUp_HighFanInSkipped verifies that helpers with more than
// maxCallersPerHelper callers are skipped (wide utility detection).
func TestDeriveTestsWalkUp_HighFanInSkipped(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "helper1", Name: "_util_fn", Kind: "SCOPE.Operation", SourceFile: "utils.py"},
		{ID: "test1", Name: "test_util", Kind: "SCOPE.Operation", SourceFile: "tests/test_utils.py"},
	}
	rels := []Relationship{
		{ID: "rt1", FromID: "test1", ToID: "helper1", Kind: "TESTS"},
	}
	// Add maxCallersPerHelper+1 callers.
	for i := 0; i <= maxCallersPerHelper; i++ {
		callerID := "caller" + string(rune('0'+i))
		entities = append(entities, Entity{
			ID: callerID, Name: "Caller" + string(rune('A'+i)),
			Kind: "SCOPE.Operation", SourceFile: "prod.py",
		})
		rels = append(rels, Relationship{
			ID:     "c" + string(rune('0'+i)),
			FromID: callerID, ToID: "helper1", Kind: "CALLS",
		})
	}
	doc := makeDoc(entities, rels)
	stats := DeriveTestsWalkUp(doc)

	if stats.SkippedHighFanIn != 1 {
		t.Errorf("SkippedHighFanIn want 1, got %d", stats.SkippedHighFanIn)
	}
	if stats.DerivedEdges != 0 {
		t.Errorf("DerivedEdges want 0 (high fan-in skipped), got %d", stats.DerivedEdges)
	}
}

// TestDeriveTestsWalkUp_DuplicateSuppressed verifies that if an explicit TESTS
// edge already exists for the caller, the derived edge is suppressed.
func TestDeriveTestsWalkUp_DuplicateSuppressed(t *testing.T) {
	t.Parallel()
	doc := makeDoc(
		[]Entity{
			{ID: "helper1", Name: "_helper", Kind: "SCOPE.Operation", SourceFile: "svc.py"},
			{ID: "viewset1", Name: "ViewSet.create", Kind: "SCOPE.Operation", SourceFile: "views.py"},
			{ID: "test1", Name: "test_create", Kind: "SCOPE.Operation", SourceFile: "tests/test_views.py"},
		},
		[]Relationship{
			// Explicit TESTS: test → helper
			{ID: "r1", FromID: "test1", ToID: "helper1", Kind: "TESTS"},
			// Explicit TESTS: test → viewset (already present — no derived needed)
			{ID: "r2", FromID: "test1", ToID: "viewset1", Kind: "TESTS"},
			// CALLS: viewset → helper
			{ID: "c1", FromID: "viewset1", ToID: "helper1", Kind: "CALLS"},
		},
	)
	stats := DeriveTestsWalkUp(doc)
	if stats.DuplicatesSuppressed != 1 {
		t.Errorf("DuplicatesSuppressed want 1, got %d", stats.DuplicatesSuppressed)
	}
	if stats.DerivedEdges != 0 {
		t.Errorf("DerivedEdges want 0 (duplicate suppressed), got %d", stats.DerivedEdges)
	}
}

// TestDeriveTestsWalkUp_MultipleHelpersSharedCaller verifies that when a test
// reaches the same caller via two different helpers, only one derived edge is
// emitted (deduplication).
func TestDeriveTestsWalkUp_MultipleHelpersSharedCaller(t *testing.T) {
	t.Parallel()
	doc := makeDoc(
		[]Entity{
			{ID: "h1", Name: "_helper_a", Kind: "SCOPE.Operation", SourceFile: "svc.py"},
			{ID: "h2", Name: "_helper_b", Kind: "SCOPE.Operation", SourceFile: "svc.py"},
			{ID: "vs1", Name: "ViewSet.create", Kind: "SCOPE.Operation", SourceFile: "views.py"},
			{ID: "t1", Name: "test_create", Kind: "SCOPE.Operation", SourceFile: "tests/test_views.py"},
		},
		[]Relationship{
			// test → helper_a
			{ID: "r1", FromID: "t1", ToID: "h1", Kind: "TESTS"},
			// test → helper_b
			{ID: "r2", FromID: "t1", ToID: "h2", Kind: "TESTS"},
			// viewset → helper_a
			{ID: "c1", FromID: "vs1", ToID: "h1", Kind: "CALLS"},
			// viewset → helper_b
			{ID: "c2", FromID: "vs1", ToID: "h2", Kind: "CALLS"},
		},
	)
	stats := DeriveTestsWalkUp(doc)
	// Should emit exactly ONE derived edge t1 → vs1, not two.
	if stats.DerivedEdges != 1 {
		t.Errorf("DerivedEdges want 1 (deduped multi-helper), got %d", stats.DerivedEdges)
	}
}

// TestDeriveTestsWalkUp_TestCallerSkipped verifies that CALLS edges from test
// entities are not treated as "viewset callers" (only production callers count).
func TestDeriveTestsWalkUp_TestCallerSkipped(t *testing.T) {
	t.Parallel()
	doc := makeDoc(
		[]Entity{
			{ID: "h1", Name: "_helper", Kind: "SCOPE.Operation", SourceFile: "svc.py"},
			{ID: "t1", Name: "test_helper", Kind: "SCOPE.Operation", SourceFile: "tests/test_svc.py"},
			// Another test that CALLS the helper directly — should NOT be a walk-up target.
			{ID: "t2", Name: "test_other", Kind: "SCOPE.Operation", SourceFile: "tests/test_other.py"},
		},
		[]Relationship{
			{ID: "r1", FromID: "t1", ToID: "h1", Kind: "TESTS"},
			{ID: "c1", FromID: "t2", ToID: "h1", Kind: "CALLS"}, // caller is test — skip
		},
	)
	stats := DeriveTestsWalkUp(doc)
	if stats.DerivedEdges != 0 {
		t.Errorf("DerivedEdges want 0 (test callers must be skipped), got %d", stats.DerivedEdges)
	}
}

// TestDeriveTestsWalkUp_IDStability verifies that running DeriveTestsWalkUp
// twice does not append duplicate derived edges (the edge id is content-addressed
// and existingTests dedup prevents double-emission on a single call; the test
// checks that the second call adds 0 edges because all callers are already in
// existingTests via the first derived edge).
func TestDeriveTestsWalkUp_IDStability(t *testing.T) {
	t.Parallel()
	doc := makeWalkUpFixture()
	s1 := DeriveTestsWalkUp(doc)
	s2 := DeriveTestsWalkUp(doc)

	if s1.DerivedEdges != 1 {
		t.Errorf("first run DerivedEdges want 1, got %d", s1.DerivedEdges)
	}
	if s2.DerivedEdges != 0 {
		t.Errorf("second run DerivedEdges want 0 (idempotent), got %d", s2.DerivedEdges)
	}
}

// TestDeriveTestsWalkUp_CoverageIntegration verifies that ComputeCoverage
// counts the viewset method as covered after DeriveTestsWalkUp runs.
func TestDeriveTestsWalkUp_CoverageIntegration(t *testing.T) {
	t.Parallel()
	doc := makeWalkUpFixture()

	// Before walk-up: only helper1 is covered.
	beforeReport := ComputeCoverage(doc)
	if beforeReport.CoveredProduction != 1 {
		t.Errorf("before: CoveredProduction want 1 (helper only), got %d", beforeReport.CoveredProduction)
	}

	// Run walk-up.
	stats := DeriveTestsWalkUp(doc)
	if stats.DerivedEdges != 1 {
		t.Fatalf("DeriveTestsWalkUp should emit 1 derived edge, got %d", stats.DerivedEdges)
	}

	// After walk-up: both helper1 and viewset1 should be covered.
	afterReport := ComputeCoverage(doc)
	if afterReport.CoveredProduction != 2 {
		t.Errorf("after: CoveredProduction want 2 (helper + viewset), got %d", afterReport.CoveredProduction)
	}
	if afterReport.CoveragePct < 99.9 {
		t.Errorf("after: CoveragePct want ~100, got %f", afterReport.CoveragePct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstringHelper(s, sub))
}

func containsSubstringHelper(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
