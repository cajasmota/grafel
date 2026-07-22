package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSetRefForEmbeddingWalk hand-builds a 1-segment gen dir
// (graph.<gen>/ with seg-0000.fb + manifest.json) under refDir, points
// `current` at it, and stamps the single entity with embeddingRef — mirroring
// the fixture builders used elsewhere in this slice
// (internal/daemon/state_path_graphfbexists_test.go). No flat graph.fb is
// ever written, matching a real segment-set ref.
func writeSegmentSetRefForEmbeddingWalk(t *testing.T, refDir string, gen uint64, embeddingRef string) {
	t.Helper()
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A", EmbeddingRef: embeddingRef},
	}}
	genDir := filepath.Join(refDir, graph.GenDirName(gen))
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
	if err := graph.WriteCurrentPointerRaw(refDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

// TestCollectActiveEmbeddingHashes_SegmentSet is the RED test for #5915 J2
// slice-3: a live segment-set ref's embedding_ref values must be reported
// active. Before the fix, seg-NNNN.fb matches neither graph.IsGraphFileName's
// legacy-flat nor graph.<gen>.fb pattern, so the walk skipped a segment-set
// ref's state dir entirely — its embedding refs were never collected as
// active, so a subsequent TTL-based embedding-cache sweep could reap a
// .vec cache entry that a live segment-set graph still references.
func TestCollectActiveEmbeddingHashes_SegmentSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)

	refDir := filepath.Join(home, "store", "myrepo-abc123", "refs", "main")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSegmentSetRefForEmbeddingWalk(t, refDir, 4, "sha256:seg-live-hash")

	active, err := collectActiveEmbeddingHashes()
	if err != nil {
		t.Fatalf("collectActiveEmbeddingHashes: %v", err)
	}
	if !active["sha256:seg-live-hash"] {
		t.Fatalf("active = %v, want it to include the segment-set ref's embedding hash", active)
	}
}

// TestCollectActiveEmbeddingHashes_SingleFileParity guards the single-gen-file
// (and legacy flat) resolution is unchanged: exactly one state dir's
// embedding refs are collected, not double-counted across generations.
func TestCollectActiveEmbeddingHashes_SingleFileParity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)

	refDir := filepath.Join(home, "store", "myrepo-abc123", "refs", "main")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := &graph.Document{Repo: "single", Entities: []graph.Entity{
		{ID: "aaaa0001", QualifiedName: "p.A", Kind: "function", Name: "A", EmbeddingRef: "sha256:single-live-hash"},
	}}
	if err := fbwriter.WriteAtomic(filepath.Join(refDir, graph.GenFileName(1)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(refDir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}

	active, err := collectActiveEmbeddingHashes()
	if err != nil {
		t.Fatalf("collectActiveEmbeddingHashes: %v", err)
	}
	if !active["sha256:single-live-hash"] {
		t.Fatalf("active = %v, want it to include the single-file ref's embedding hash (parity)", active)
	}
}
