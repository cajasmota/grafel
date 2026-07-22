package graph_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbversion"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// patchSegmentVersion rewrites <genDir>/seg-0000.fb with a copy of doc whose
// on-disk Graph.version scalar is mutated down to oldVersion — the segment-set
// counterpart to writeOldFormatGraphFB. MultiReader.Version() reports segment
// 0's version, so patching seg 0 is sufficient to simulate a below-min
// segment-set. doc MUST equal the doc originally written as seg 0 so the
// manifest's MinKey/MaxKey stay valid.
func patchSegmentVersion(t *testing.T, genDir string, doc *graph.Document, oldVersion int) {
	t.Helper()
	buf, err := fbwriter.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	root := fb.GetRootAsGraph(buf, 0)
	if !root.MutateVersion(int32(oldVersion)) {
		t.Fatalf("MutateVersion(%d) returned false — slot missing?", oldVersion)
	}
	if err := os.WriteFile(filepath.Join(genDir, graph.SegmentFileName(0)), buf, 0o644); err != nil {
		t.Fatalf("overwrite seg 0: %v", err)
	}
}

// TestReindexRequiredReason_SegmentSet_CurrentVersion is the FIX-2 no-op guard:
// a current-version segment-set must NOT be flagged (required=false). Before
// #5915 J1 the header-only detector opened the absent flat .fb → returned
// (false,"") by accident; now it must return (false,"") by actually reading the
// MultiReader's version and finding it >= min.
func TestReindexRequiredReason_SegmentSet_CurrentVersion(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 3, threeSegDocs())

	if required, reason := graph.ReindexRequiredReason(dir); required {
		t.Errorf("current-version segment-set flagged stale: reason=%q", reason)
	}
}

// TestReindexRequiredReason_SegmentSet_OldVersion is the FIX-2 detection test:
// a segment-set whose segment 0 is stamped below fbversion.Version must be
// detected as reindex-required (defeating the "v4 segment-set not auto-reindexed
// after an fbversion bump → serves empty" break for exactly the large repos that
// segment).
func TestReindexRequiredReason_SegmentSet_OldVersion(t *testing.T) {
	dir := t.TempDir()
	docs := threeSegDocs()
	genDir := writeSegmentSet(t, dir, 4, docs)
	patchSegmentVersion(t, genDir, docs[0], fbversion.Version-1)

	required, reason := graph.ReindexRequiredReason(dir)
	if !required {
		t.Fatal("old-version segment-set NOT detected as reindex-required")
	}
	wantSubstrs := []string{
		fmt.Sprintf("v%d", fbversion.Version-1),
		fmt.Sprintf("v%d", fbversion.Version),
		"reindex",
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(strings.ToLower(reason), strings.ToLower(want)) {
			t.Errorf("reason %q missing expected substring %q", reason, want)
		}
	}
}
