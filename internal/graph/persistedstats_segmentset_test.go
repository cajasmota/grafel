package graph_test

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// TestPersistedStatsFromDir_SegmentSet is the FIX-1 RED test (#5915 J1): the
// cheap header-only stats probe must be segment-aware. Before this slice it
// opened CurrentGraphPath(dir)+fbreader.Open, and for a segment-set that flat
// path is absent → ok=false → the caller sees "0 entities / never indexed" and
// the incremental.go:331 gate force-reindex-loops the repo. It must instead
// resolve the descriptor, open a MultiReader over the gen dir, and report the
// summed counts with ok=true.
func TestPersistedStatsFromDir_SegmentSet(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 3, threeSegDocs())

	ps, ok := graph.PersistedStatsFromDir(dir)
	if !ok {
		t.Fatal("PersistedStatsFromDir(segment-set) ok=false; want ok=true (segment-aware). This is the 'never indexed' cascade at incremental.go:331.")
	}
	if ps.Entities != 6 {
		t.Errorf("Entities = %d, want 6 (summed across segments, NOT 0)", ps.Entities)
	}
	if ps.Relationships != 3 {
		t.Errorf("Relationships = %d, want 3 (summed across segments)", ps.Relationships)
	}
}

// TestPersistedStatsFromDir_IncrementalGateCleared is the explicit cascade
// guard: the incremental.go:331 "absent-graph-nonempty-tree → force full
// reindex" gate keys off exactly PersistedStatsFromDir(...) ok. A segment-set
// must read ok=true so a segmented repo is NOT treated as never-indexed and does
// NOT enter the forced-reindex loop.
func TestPersistedStatsFromDir_IncrementalGateCleared(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 7, threeSegDocs())
	if _, ok := graph.PersistedStatsFromDir(dir); !ok {
		t.Fatal("segment-set reads as !ok at the incremental.go:331 gate → forced-reindex loop")
	}
}

// TestPersistedStatsFromDir_SingleFileParity guards that the single-file gen
// path is byte-identical after the descriptor-routing change: correct counts,
// ok=true.
func TestPersistedStatsFromDir_SingleFileParity(t *testing.T) {
	dir := t.TempDir()
	doc := &graph.Document{Repo: "single", Entities: []graph.Entity{
		{ID: "aaaa0001", QualifiedName: "p.A", Kind: "function", Name: "A"},
		{ID: "aaaa0002", QualifiedName: "p.B", Kind: "struct", Name: "B"},
	}, Relationships: []graph.Relationship{{FromID: "aaaa0001", ToID: "aaaa0002", Kind: "CALLS"}}}
	if err := fbwriter.WriteAtomic(filepath.Join(dir, graph.GenFileName(1)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(dir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}
	ps, ok := graph.PersistedStatsFromDir(dir)
	if !ok {
		t.Fatal("single-file parity: ok=false, want true")
	}
	if ps.Entities != 2 || ps.Relationships != 1 {
		t.Errorf("single-file counts = %d/%d, want 2/1", ps.Entities, ps.Relationships)
	}
}

// TestPersistedStatsFromDir_Absent: a never-indexed dir still reports ok=false
// (parity with the pre-#5915 absent semantics).
func TestPersistedStatsFromDir_Absent(t *testing.T) {
	dir := t.TempDir()
	if _, ok := graph.PersistedStatsFromDir(dir); ok {
		t.Error("absent dir: ok=true, want false")
	}
}
