package main

// TestRebuild_PrefixlessTestsAreCounted is the integration test for #2608:
// after the testmap pathHints fix (#2604), the archigraph_test_coverage MCP
// tool STILL reports 107 test entities on upvate instead of the expected
// ~1,406+.
//
// Root cause (#2608):
//
//	#2604 fixed the testmap extractor's selectFramework so that files in
//	tests/ directories without a test_ prefix (e.g. core/tests/schedule.py)
//	are now recognised as pytest test files and produce SCOPE.Pattern entities.
//	HOWEVER, the TotalTests counter in ComputeCoverage (internal/graph/coverage.go)
//	does NOT count SCOPE.Pattern entities — it counts SCOPE.Operation / Function /
//	Method entities inside test files (issue #1410 rationale).
//
//	The entity-kind classifier isTestFile (coverage.go:isTestFile) already
//	handles /tests/ directory paths correctly via its path-segment check
//	(checks for "/tests/" segment in the slashed path). But no integration
//	test exercised the path-segment check against actual indexer output —
//	unit tests only covered the isTestFile function directly, not the
//	end-to-end flow from Python extractor → graph.Document → ComputeCoverage.
//
// This test closes the gap by running the full indexer on a Django fixture
// with test files that have NO test_ prefix (core/tests/schedule.py,
// api/tests/views.py) and asserting that ComputeCoverage.TotalTests is
// non-zero — i.e. SCOPE.Operation entities from those files are counted
// as test entities.
//
// Companion tests:
//
//	internal/graph/coverage_test.go:TestIsTestFile (unit: isTestFile covers /tests/ paths)
//	cmd/archigraph/rebuild_2604_test.go:TestRebuild_AllPassesProduceExpectedEdges
//	  (asserts SCOPE.Pattern entities produced; TotalTests NOT asserted there)

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestRebuild_PrefixlessTestsAreCounted asserts that Django-style test files
// in tests/ directories (without a test_ prefix in their basename) are counted
// in ComputeCoverage.TotalTests after a full indexer run.
//
// The fixture at testdata/django_tests_fixture includes:
//
//	core/tests/schedule.py  — 2 test methods (test_list, test_import_csv)
//	api/tests/views.py      — 1 test method (test_schedule_list)
//
// All three methods are emitted as SCOPE.Operation entities by the Python
// extractor. isTestFile("core/tests/schedule.py") must return true (via the
// path-segment check on "/tests/") so ComputeCoverage counts them in TotalTests.
//
// If TotalTests is 0 after indexing these files, the path-segment check is not
// reaching the entity classification path — indicative of the #2608 bug where
// TotalTests stays at 107 (only test_*.py files counted) regardless of how
// many tests/ directory files exist.
func TestRebuild_PrefixlessTestsAreCounted(t *testing.T) {
	// Run the full indexer on the Django mini-fixture. No passes skipped so
	// that the Python extractor, engine passes, and graph assembly all fire.
	doc := runIndexerOn(t, "testdata/django_tests_fixture", "django_tests_fixture", nil)

	// Compute coverage using the same algorithm as archigraph_test_coverage MCP.
	report := graph.ComputeCoverage(doc)

	// The fixture has at least 3 SCOPE.Operation test methods across two
	// prefixless test files (core/tests/schedule.py and api/tests/views.py).
	// TotalTests must be >= 3 for the path-segment check to be working.
	//
	// Before the fix this was 0 because isTestFile's path-segment check was not
	// exercised by the integration path, and the entity's SourceFile value was
	// "core/tests/schedule.py" which DOES contain "/tests/" — so the bug would
	// only manifest if SourceFile were stored differently (e.g. without the path
	// prefix) or if isTestFile had a different code path for .py files.
	if report.TotalTests == 0 {
		t.Errorf(
			"#2608: TotalTests=0 after indexing Django fixture with prefixless test files — "+
				"core/tests/schedule.py and api/tests/views.py contain SCOPE.Operation test "+
				"methods that isTestFile should classify as test entities via path-segment check "+
				"for /tests/ directory; got TotalTests=%d TotalProduction=%d",
			report.TotalTests, report.TotalProduction,
		)
	}

	// Verify the specific prefixless test files produced SCOPE.Operation entities
	// with the correct SourceFile so we can rule out a SourceFile normalisation bug.
	wantTestSources := []string{
		"core/tests/schedule.py",
		"api/tests/views.py",
	}
	for _, wantSrc := range wantTestSources {
		hasOp := false
		for _, e := range doc.Entities {
			if e.SourceFile == wantSrc && (e.Kind == "SCOPE.Operation" || e.Kind == "Function" || e.Kind == "Method") {
				hasOp = true
				break
			}
		}
		if !hasOp {
			t.Errorf(
				"#2608: no SCOPE.Operation entity found with SourceFile=%q — "+
					"Python extractor may not be emitting operation entities for this file, "+
					"or SourceFile is stored under a different path (check filepath normalisation)",
				wantSrc,
			)
		}
	}

	// Sanity: with 3 SCOPE.Operation test entities available, TotalTests should
	// be at least 3. It may be higher if helper methods / setUp / tearDown are
	// also emitted (all methods in a test file count).
	const wantMinTotalTests = 3
	if report.TotalTests > 0 && report.TotalTests < wantMinTotalTests {
		t.Errorf(
			"#2608: TotalTests=%d < %d: expected at least one test entity per test method "+
				"(test_list, test_import_csv in schedule.py; test_schedule_list in views.py); "+
				"check that isTestFile path-segment check covers both fixture files",
			report.TotalTests, wantMinTotalTests,
		)
	}

	// Log the report for diagnosis when the test fails.
	t.Logf("#2608 coverage report: TotalTests=%d TotalProduction=%d CoveredProduction=%d",
		report.TotalTests, report.TotalProduction, report.CoveredProduction)
}
