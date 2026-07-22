package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// writeSegmentSetFixtureForColdRef hand-builds a 1-segment gen dir
// (graph.<gen>/ with seg-0000.fb + manifest.json) under refDir and points
// `current` at it — same fixture shape used elsewhere in this slice.
func writeSegmentSetFixtureForColdRef(t *testing.T, refDir string, gen uint64) {
	t.Helper()
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	genDir := filepath.Join(refDir, graph.GenDirName(gen))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	ids := []string{"aa1"}
	sort.Strings(ids)
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities, EntityCount: 1,
		MinKey: ids[0], MaxKey: ids[0],
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(refDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

// TestComputeStatusSummary_ColdRefs_SegmentSet is the RED test for #5915 J2
// P2 slice 1: a cold ref slot backed by a segment-set (graph.<gen>/ dir +
// manifest.json, no flat graph.fb) must still be reported as a cold ref.
// Before the fix, the gate stat'd graph.CurrentGraphPath(refDir) — the flat
// .fb path, absent for a segment-set — so the cold ref was silently dropped.
func TestComputeStatusSummary_ColdRefs_SegmentSet(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "myrepo-seg")
	os.MkdirAll(repoPath, 0o755)

	hotStateDir := daemon.StateDirForRepo(repoPath)
	os.MkdirAll(hotStateDir, 0o755)

	side := graph.GraphStatsSidecar{
		Version:       1,
		ComputedAt:    time.Now().Add(-2 * time.Minute),
		TotalEntities: 100,
	}
	sideData, _ := json.Marshal(side)
	os.WriteFile(filepath.Join(hotStateDir, "graph-stats.json"), sideData, 0o644)

	// Cold ref slot backed by a SEGMENT-SET (no flat graph.fb).
	refsDir := filepath.Dir(hotStateDir)
	coldRefDir := filepath.Join(refsDir, "develop-seg")
	os.MkdirAll(coldRefDir, 0o755)
	writeSegmentSetFixtureForColdRef(t, coldRefDir, 8)

	repos := []registry.Repo{{Slug: "myrepo-seg", Path: repoPath}}
	summary := ComputeStatusSummary("grp", repos)

	rs, ok := summary.RepoStats["myrepo-seg"]
	if !ok {
		t.Fatal("myrepo-seg not found in RepoStats")
	}
	found := false
	for _, r := range rs.ColdRefs {
		if r == "develop-seg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ColdRefs = %v, want to include develop-seg (segment-set cold ref)", rs.ColdRefs)
	}
}
