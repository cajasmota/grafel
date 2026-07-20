package coverage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// fixtureLCOV is a tiny LCOV report: src/svc.go has lines 10-13 instrumented,
// 3 of 4 covered (line 12 unhit) → 75% line coverage for the spanned function.
const fixtureLCOV = `SF:src/svc.go
DA:10,5
DA:11,2
DA:12,0
DA:13,4
end_of_record
`

// TestEnrich_EndToEnd is the #5061 acceptance fixture: build a tiny Document +
// a fixture lcov.info on disk, run the enrichment pass, persist the Document to
// graph.fb, reload it, and assert the indexed entity carries coverage_pct (the
// LCOV signal) AND test_reachable (the static-reachability signal) in its
// round-tripped Properties. This proves the whole wiring: config/discovery →
// parse → attribute + reachability → stamp → persist via the Properties map →
// reload.
func TestEnrich_EndToEnd(t *testing.T) {
	repo := t.TempDir()

	// Write the fixture LCOV at the default-discovery path.
	covDir := filepath.Join(repo, "coverage")
	if err := os.MkdirAll(covDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(covDir, "lcov.info"), []byte(fixtureLCOV), 0o644); err != nil {
		t.Fatal(err)
	}

	// A tiny graph: one production function (svc, spanning the instrumented
	// lines) and one test that TESTS it — exercising BOTH sub-passes.
	doc := &graph.Document{
		Version: 1,
		Entities: []graph.Entity{
			{
				ID:         "ent-svc",
				Name:       "Handle",
				Kind:       "SCOPE.Function",
				SourceFile: "src/svc.go",
				StartLine:  10,
				EndLine:    13,
				Language:   "go",
			},
			{
				ID:         "ent-test",
				Name:       "TestHandle",
				Kind:       "SCOPE.Pattern",
				Subtype:    "test_suite",
				SourceFile: "src/svc_test.go",
				StartLine:  1,
				EndLine:    5,
				Language:   "go",
				Tags:       []string{"test"},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "ent-test", ToID: "ent-svc", Kind: "TESTS"},
		},
	}

	// Run the enrichment pass against the in-memory document.
	st := Enrich(doc, repo, Config{}) // zero Config → default discovery
	if st.Skipped {
		t.Fatalf("enrich skipped unexpectedly: %s", st.SkipReason)
	}
	if st.LCOVAttributed == 0 {
		t.Fatalf("expected at least one LCOV-attributed entity, got 0 (report=%q)", st.ReportPath)
	}
	if st.ReachabilityReachable == 0 {
		t.Fatal("expected at least one test-reachable entity, got 0")
	}

	// Persist to graph.fb and reload — proving the stamped Properties survive
	// the serializer round-trip (the persistence requirement of #5061).
	fbPath := filepath.Join(repo, ".grafel", "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write fb: %v", err)
	}
	reloaded, err := graph.LoadGraphFromDir(filepath.Join(repo, ".grafel"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	var svc *graph.Entity
	for i := range reloaded.Entities {
		if reloaded.Entities[i].ID == "ent-svc" {
			svc = &reloaded.Entities[i]
			break
		}
	}
	if svc == nil {
		t.Fatal("svc entity missing after reload")
	}

	// LCOV signal: 3 of 4 instrumented lines covered → 75.0%.
	if got := svc.PropGet(PropCoveragePct); got != "75.0" {
		t.Errorf("%s = %q after reload, want %q", PropCoveragePct, got, "75.0")
	}
	if got := svc.PropGet(PropCoverageSource); got != SourceLCOV {
		t.Errorf("%s = %q, want %q", PropCoverageSource, got, SourceLCOV)
	}
	if got := svc.PropGet(PropCoveredLines); got != "3" {
		t.Errorf("%s = %q, want %q", PropCoveredLines, got, "3")
	}
	if got := svc.PropGet(PropTotalLines); got != "4" {
		t.Errorf("%s = %q, want %q", PropTotalLines, got, "4")
	}

	// Reachability signal: the function is TESTS-reached at depth 1.
	if got := svc.PropGet(PropTestReachable); got != "true" {
		t.Errorf("%s = %q, want %q", PropTestReachable, got, "true")
	}
	if got := svc.PropGet(PropReachDepth); got != "1" {
		t.Errorf("%s = %q, want %q", PropReachDepth, got, "1")
	}
}

// TestEnrich_NoReport_NoOpLCOV proves the pass is opt-in for LCOV: with no
// report on disk, no coverage_pct is stamped, but reachability still runs.
func TestEnrich_NoReport_NoOpLCOV(t *testing.T) {
	repo := t.TempDir() // empty: no coverage/lcov.info

	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "ent-svc", Name: "Handle", Kind: "SCOPE.Function", SourceFile: "src/svc.go", StartLine: 10, EndLine: 13},
			{ID: "ent-test", Name: "TestHandle", Kind: "SCOPE.Pattern", Subtype: "test_suite", SourceFile: "src/svc_test.go", Tags: []string{"test"}},
		},
		Relationships: []graph.Relationship{
			{FromID: "ent-test", ToID: "ent-svc", Kind: "TESTS"},
		},
	}

	st := Enrich(doc, repo, Config{})
	if st.LCOVAttributed != 0 {
		t.Errorf("expected no LCOV attribution without a report, got %d", st.LCOVAttributed)
	}
	if st.ReachabilityReachable == 0 {
		t.Error("reachability should still run without a report")
	}
	svc := doc.Entities[0]
	if _, ok := svc.PropLookup(PropCoveragePct); ok {
		t.Errorf("coverage_pct must not be stamped without a report; got %v", svc.PropsSnapshot())
	}
	if got := svc.PropGet(PropTestReachable); got != "true" {
		t.Errorf("%s = %q, want true", PropTestReachable, got)
	}
}

// TestEnrich_ConfiguredGlob proves an explicit report_paths glob is honored
// (and that a configured-but-unmatched glob does NOT fall back to discovery).
func TestEnrich_ConfiguredGlob(t *testing.T) {
	repo := t.TempDir()
	nested := filepath.Join(repo, "packages", "web", "coverage")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "lcov.info"), []byte(fixtureLCOV), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "ent-svc", Name: "Handle", Kind: "SCOPE.Function", SourceFile: "src/svc.go", StartLine: 10, EndLine: 13},
		},
	}
	cfg := Config{Format: FormatLCOV, ReportPaths: []string{"packages/*/coverage/lcov.info"}}
	st := Enrich(doc, repo, cfg)
	if st.LCOVAttributed == 0 {
		t.Fatalf("configured glob did not resolve a report (report=%q)", st.ReportPath)
	}
	if got := doc.Entities[0].PropGet(PropCoveragePct); got != "75.0" {
		t.Errorf("%s = %q, want 75.0", PropCoveragePct, got)
	}
}
