package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSetFixtureForTierTest hand-builds a 1-segment gen dir
// (graph.<gen>/ with seg-0000.fb + manifest.json) under stateDir and points
// `current` at it, then stamps the manifest.json mtime — same fixture shape
// used elsewhere in this slice (internal/daemon/state_path_graphfbexists_test.go,
// internal/cli/status_stats_segmentset_test.go).
func writeSegmentSetFixtureForTierTest(t *testing.T, stateDir string, gen uint64, mtime time.Time) {
	t.Helper()
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities, EntityCount: 1,
		MinKey: "aa1", MaxKey: "aa1",
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifestPath := filepath.Join(genDir, graph.ManifestFileName)
	if err := os.Chtimes(manifestPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

// TestInferTierFromDisk_SegmentSet is the RED test for #5915 J2 slice-3: a
// segment-set state dir (graph.<gen>/ dir + manifest.json, no flat graph.fb)
// indexed a minute ago must infer tier "hot" with a non-zero lastSeen. Before
// the fix, inferTierFromDisk stat'd only [graph.CurrentGraphPath(stateDir),
// graph.json] — both absent for a segment-set dir — so it silently inferred
// tier "cold" with a zero lastSeen (mtime never found).
func TestInferTierFromDisk_SegmentSet(t *testing.T) {
	stateDir := t.TempDir()
	mtime := time.Now().Add(-1 * time.Minute)
	writeSegmentSetFixtureForTierTest(t, stateDir, 5, mtime)

	tierStr, lastSeen := inferTierFromDisk(stateDir)

	if tierStr != "hot" {
		t.Errorf("tierStr = %q, want %q", tierStr, "hot")
	}
	if lastSeen.IsZero() {
		t.Fatal("lastSeen is zero, want non-zero (segment-set manifest.json mtime)")
	}
	if diff := lastSeen.Sub(mtime); diff < -time.Second || diff > time.Second {
		t.Errorf("lastSeen = %v, want ~%v", lastSeen, mtime)
	}
}

// TestInferTierFromDisk_SingleFileParity guards the single-gen-file (and
// legacy flat) resolution is byte-identical to the pre-fix behavior.
func TestInferTierFromDisk_SingleFileParity(t *testing.T) {
	stateDir := t.TempDir()
	flat := filepath.Join(stateDir, "graph.fb")
	mtime := time.Now().Add(-1 * time.Minute)
	if err := os.WriteFile(flat, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(flat, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	tierStr, lastSeen := inferTierFromDisk(stateDir)

	if tierStr != "hot" {
		t.Errorf("tierStr = %q, want %q", tierStr, "hot")
	}
	if diff := lastSeen.Sub(mtime); diff < -time.Second || diff > time.Second {
		t.Errorf("lastSeen = %v, want ~%v", lastSeen, mtime)
	}
}

// TestInferTierFromDisk_NeverIndexed guards a never-indexed state dir infers
// tier "cold" with a zero lastSeen.
func TestInferTierFromDisk_NeverIndexed(t *testing.T) {
	stateDir := t.TempDir()
	tierStr, lastSeen := inferTierFromDisk(stateDir)
	if tierStr != "cold" {
		t.Errorf("tierStr = %q, want %q", tierStr, "cold")
	}
	if !lastSeen.IsZero() {
		t.Errorf("lastSeen = %v, want zero", lastSeen)
	}
}
