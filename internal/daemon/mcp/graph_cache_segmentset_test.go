package mcp

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSetFixture hand-builds a 2-segment gen dir (graph.<gen>/ with
// seg-0000.fb + seg-0001.fb + manifest.json) under stateDir and points the
// `current` pointer at the gen dir. No producer emits this yet (#5901 dark
// substrate) so the test constructs it directly. Returns the gen dir.
func writeSegmentSetFixture(t *testing.T, stateDir string, gen uint64) string {
	t.Helper()
	docs := []*graph.Document{
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
			{ID: "aa2", QualifiedName: "p.B", Kind: "struct", Name: "B"},
		}, Relationships: []graph.Relationship{{FromID: "aa1", ToID: "mm1", Kind: "CALLS"}}},
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "mm1", QualifiedName: "p.M", Kind: "function", Name: "M"},
			{ID: "mm2", QualifiedName: "p.N", Kind: "struct", Name: "N"},
		}, Relationships: []graph.Relationship{{FromID: "mm1", ToID: "aa1", Kind: "REFERENCES"}}},
	}
	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion}
	for i, doc := range docs {
		name := graph.SegmentFileName(i)
		if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		ids := make([]string, 0, len(doc.Entities))
		for _, e := range doc.Entities {
			ids = append(ids, e.ID)
		}
		sort.Strings(ids)
		m.Segments = append(m.Segments, graph.SegmentMeta{
			File: name, Kind: graph.SegmentEntities,
			EntityCount: len(doc.Entities), RelCount: len(doc.Relationships),
			MinKey: ids[0], MaxKey: ids[len(ids)-1],
		})
	}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
	return genDir
}

// TestCache_SegmentSetGenDir: the daemon graph cache opens a multi-segment gen
// dir (keyed by the gen dir path, as GetForRepoRef would resolve it) and the
// returned GraphView reads across all segments, including a cross-segment
// relationship endpoint.
func TestCache_SegmentSetGenDir(t *testing.T) {
	stateDir := t.TempDir()
	genDir := writeSegmentSetFixture(t, stateDir, 3)

	c := NewCache(4)
	// Windows teardown: close the cache (unmaps every segment) before the
	// t.TempDir cleanup unlinks the gen dir.
	t.Cleanup(func() { _ = c.Close() })

	r, release, err := c.Get(genDir)
	if err != nil {
		t.Fatalf("Get(gen dir): %v", err)
	}
	defer release()

	if got := r.EntityCount(); got != 4 {
		t.Errorf("EntityCount across segments = %d, want 4", got)
	}
	if got := r.RelationshipCount(); got != 2 {
		t.Errorf("RelationshipCount across segments = %d, want 2", got)
	}
	for _, id := range []string{"aa1", "aa2", "mm1", "mm2"} {
		if e := r.LookupEntityByID(id); e == nil || string(e.Id()) != id {
			t.Errorf("LookupEntityByID(%q) failed across segments: %v", id, e)
		}
	}
	// A second Get is a cache hit on the same gen-dir key.
	r2, rel2, err := c.Get(genDir)
	if err != nil {
		t.Fatal(err)
	}
	rel2()
	if r2 != r {
		t.Fatal("expected cache hit (same GraphView) for the gen-dir key")
	}
	if s := c.Stats(); s.Hits != 1 {
		t.Fatalf("stats after hit: %+v", s)
	}
}
