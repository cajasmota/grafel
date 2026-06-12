package dashboard

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/coverage"
	"github.com/cajasmota/archigraph/internal/graph"
)

// ent builds a graph.Entity carrying the stamped ingested-line-coverage props
// (#5036) for the accumulator tests.
func ent(id, file, source, covered, total, measuredAt string) graph.Entity {
	props := map[string]string{}
	if source != "" {
		props[coverage.PropCoverageSource] = source
	}
	if covered != "" {
		props[coverage.PropCoveredLines] = covered
	}
	if total != "" {
		props[coverage.PropTotalLines] = total
	}
	if measuredAt != "" {
		props[coverage.PropCoverageMeasAt] = measuredAt
	}
	return graph.Entity{ID: id, SourceFile: file, Properties: props}
}

func TestLineCovAccumulator_NoStampReturnsNil(t *testing.T) {
	var a lineCovAccumulator
	a.accumulate(&graph.Document{Entities: []graph.Entity{
		{ID: "e1", SourceFile: "a.ts"}, // no coverage props
		{ID: "e2", SourceFile: "b.ts", Properties: map[string]string{"other": "x"}},
	}})
	if got := a.summarize(); got != nil {
		t.Fatalf("expected nil summary when nothing stamped, got %+v", got)
	}
}

func TestLineCovAccumulator_NilDocSafe(t *testing.T) {
	var a lineCovAccumulator
	a.accumulate(nil)
	if got := a.summarize(); got != nil {
		t.Fatalf("expected nil summary for nil doc, got %+v", got)
	}
}

func TestLineCovAccumulator_RollUpAndSourceAndMeasuredAt(t *testing.T) {
	var a lineCovAccumulator
	a.accumulate(&graph.Document{Entities: []graph.Entity{
		ent("a-file", "a.ts", "lcov", "8", "10", "2026-06-10T00:00:00Z"),
		ent("b-file", "b.ts", "lcov", "5", "20", "2026-06-12T00:00:00Z"),
	}})
	s := a.summarize()
	if s == nil {
		t.Fatal("expected a summary")
	}
	if s.Source != "lcov" {
		t.Errorf("source = %q, want lcov", s.Source)
	}
	if s.CoveredLines != 13 || s.TotalLines != 30 {
		t.Errorf("covered/total = %d/%d, want 13/30", s.CoveredLines, s.TotalLines)
	}
	wantPct := 100.0 * 13.0 / 30.0
	if s.CoveragePct != wantPct {
		t.Errorf("pct = %v, want %v", s.CoveragePct, wantPct)
	}
	if s.MeasuredAt != "2026-06-12T00:00:00Z" {
		t.Errorf("measuredAt = %q, want latest 2026-06-12...", s.MeasuredAt)
	}
	if s.Entities != 2 {
		t.Errorf("entities = %d, want 2", s.Entities)
	}
}

// Nested span entities in the same file must not inflate the line totals: the
// widest (whole-file) stamp wins per file.
func TestLineCovAccumulator_NoDoubleCountWithinFile(t *testing.T) {
	var a lineCovAccumulator
	a.accumulate(&graph.Document{Entities: []graph.Entity{
		// whole-file stamp
		ent("file-scope", "a.ts", "lcov", "40", "50", ""),
		// nested function span in the same file — narrower total
		ent("fn1", "a.ts", "lcov", "8", "10", ""),
		ent("fn2", "a.ts", "lcov", "5", "12", ""),
	}})
	s := a.summarize()
	if s == nil {
		t.Fatal("expected a summary")
	}
	if s.TotalLines != 50 || s.CoveredLines != 40 {
		t.Errorf("covered/total = %d/%d, want 40/50 (widest stamp wins)", s.CoveredLines, s.TotalLines)
	}
	if s.Entities != 3 {
		t.Errorf("entities = %d, want 3 (all stamped count)", s.Entities)
	}
}

// A real (if zero-line) ingestion still surfaces — entities>0 with no valid
// line numbers yields a non-nil summary at 0%.
func TestLineCovAccumulator_StampedButNoValidLines(t *testing.T) {
	var a lineCovAccumulator
	a.accumulate(&graph.Document{Entities: []graph.Entity{
		ent("e1", "a.ts", "lcov", "", "", "2026-06-12T00:00:00Z"),
	}})
	s := a.summarize()
	if s == nil {
		t.Fatal("expected non-nil summary for stamped-but-no-lines ingestion")
	}
	if s.TotalLines != 0 || s.CoveragePct != 0 {
		t.Errorf("want 0 total / 0 pct, got %d / %v", s.TotalLines, s.CoveragePct)
	}
}

// Accumulating across multiple repos (docs) merges into one roll-up.
func TestLineCovAccumulator_MultiDoc(t *testing.T) {
	var a lineCovAccumulator
	a.accumulate(&graph.Document{Entities: []graph.Entity{
		ent("r1", "x.ts", "lcov", "10", "10", "2026-06-09T00:00:00Z"),
	}})
	a.accumulate(&graph.Document{Entities: []graph.Entity{
		ent("r2", "y.ts", "lcov", "0", "10", "2026-06-11T00:00:00Z"),
	}})
	s := a.summarize()
	if s.CoveredLines != 10 || s.TotalLines != 20 {
		t.Errorf("covered/total = %d/%d, want 10/20", s.CoveredLines, s.TotalLines)
	}
	if s.MeasuredAt != "2026-06-11T00:00:00Z" {
		t.Errorf("measuredAt = %q, want latest across docs", s.MeasuredAt)
	}
}
