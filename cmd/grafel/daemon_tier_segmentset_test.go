package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/tier"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSetAt hand-builds a multi-segment generation under stateDir:
// graph.<gen>/seg-NNNN.fb + manifest.json + a `current` pointer naming the gen
// dir. Mirrors internal/graph's writeSegmentSet test helper (not importable
// across packages). Returns the gen dir.
func writeSegmentSetAt(t *testing.T, stateDir string, gen uint64, docs []*graph.Document) string {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion}
	for i, doc := range docs {
		name := graph.SegmentFileName(i)
		if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		seg := graph.SegmentMeta{
			File:        name,
			Kind:        graph.SegmentEntities,
			EntityCount: len(doc.Entities),
			RelCount:    len(doc.Relationships),
		}
		if len(doc.Entities) > 0 {
			ids := make([]string, 0, len(doc.Entities))
			for _, e := range doc.Entities {
				ids = append(ids, e.ID)
			}
			sort.Strings(ids)
			seg.MinKey, seg.MaxKey = ids[0], ids[len(ids)-1]
		} else {
			seg.Kind = graph.SegmentRelationships
		}
		m.Segments = append(m.Segments, seg)
	}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
	return genDir
}

func segTestDocs() []*graph.Document {
	return []*graph.Document{
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "a", QualifiedName: "p.A", Kind: "function", Name: "A"},
			{ID: "b", QualifiedName: "p.B", Kind: "struct", Name: "B"},
		}, Relationships: []graph.Relationship{{FromID: "a", ToID: "z", Kind: "CALLS"}}},
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "y", QualifiedName: "p.Y", Kind: "function", Name: "Y"},
			{ID: "z", QualifiedName: "p.Z", Kind: "struct", Name: "Z"},
		}, Relationships: []graph.Relationship{{FromID: "y", ToID: "a", Kind: "REFERENCES"}}},
	}
}

// TestTierReloadCallback_SegmentSet is the FIX-3 test: the cold-wake reload
// callback must warm a segment-set repo. Before #5915 J1 it did
// daemonMCPCache.Get(CurrentGraphPath(stateDir)); for a segment-set the flat
// path is absent → Get errors → a cold-evicted segmented repo could never be
// re-warmed (monolith serving break). The fix routes through the segment-aware
// GetForRepoRef, which resolves the descriptor and opens a MultiReader over the
// gen dir.
func TestTierReloadCallback_SegmentSet(t *testing.T) {
	if daemonMCPCache == nil {
		t.Skip("daemonMCPCache not initialised in this test binary")
	}
	// Isolate the store under a temp GRAFEL_DAEMON_ROOT so StateDirForRepoRef
	// (used identically by tierReloadCallback and GetForRepoRef) resolves here.
	t.Setenv(daemon.EnvRoot, t.TempDir())

	repoPath := t.TempDir() // empty working tree — no files newer than the graph
	const ref = "main"
	stateDir := daemon.StateDirForRepoRef(repoPath, ref)
	genDir := writeSegmentSetAt(t, stateDir, 3, segTestDocs())

	// Evict any resident handle so this is a genuine cold wake.
	daemonMCPCache.InvalidateDir(stateDir)
	t.Cleanup(func() { daemonMCPCache.InvalidateDir(stateDir) }) // Windows-safe: drop mmap before TempDir cleanup

	if err := tierReloadCallback(tier.SlotKey{RepoPath: repoPath, Ref: ref}); err != nil {
		t.Fatalf("tierReloadCallback(segment-set) errored (cold-wake warm failed): %v", err)
	}

	// Prove the segment-aware path was taken: the cache now holds a handle keyed
	// on the gen DIR (not the absent flat .fb), summing entities across segments.
	v, release, err := daemonMCPCache.GetForRepoRef(repoPath, ref)
	if err != nil {
		t.Fatalf("post-reload GetForRepoRef: %v", err)
	}
	defer release()
	if v.EntityCount() != 4 {
		t.Errorf("warmed segment-set EntityCount = %d, want 4 (summed across segments)", v.EntityCount())
	}
	if filepath.Base(genDir) != graph.GenDirName(3) {
		t.Fatalf("unexpected gen dir %q", genDir)
	}
}
