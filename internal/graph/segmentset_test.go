package graph_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	fbgraph "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSet hand-builds a multi-segment generation on disk WITHOUT a
// producer (none exists yet — this is the dark read substrate): it writes each
// doc as its own seg-NNNN.fb inside <dir>/graph.<gen>/, emits a matching
// manifest.json, and points <dir>/current at the gen dir. Each doc's entities
// MUST be a disjoint, key-sorted subset (the FlatBuffers `(key)` requirement),
// which the caller guarantees. Returns the gen dir path.
//
// The manifest's per-segment MinKey/MaxKey are derived from each doc's sorted
// entity ids so the reader's key-routing has real bounds to prune with.
func writeSegmentSet(t *testing.T, dir string, gen uint64, docs []*graph.Document) string {
	t.Helper()
	genDirName := graph.GenDirName(gen)
	genDir := filepath.Join(dir, genDirName)
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
			if len(doc.Relationships) > 0 {
				seg.Kind = graph.SegmentEntities
			}
		} else {
			seg.Kind = graph.SegmentRelationships
		}
		m.Segments = append(m.Segments, seg)
	}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	// Point `current` at the gen DIR (decision 2: current may name the dir).
	if err := graph.WriteCurrentPointerRaw(dir, genDirName); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
	return genDir
}

// threeSegDocs returns three disjoint, key-sorted entity subsets with a couple
// of cross-segment relationships, mirroring fbreader's threeSegmentFixture.
func threeSegDocs() []*graph.Document {
	return []*graph.Document{
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "a", QualifiedName: "p.A", Kind: "function", Name: "A"},
			{ID: "b", QualifiedName: "p.B", Kind: "struct", Name: "B"},
		}, Relationships: []graph.Relationship{{FromID: "a", ToID: "z", Kind: "CALLS"}}},
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "m", QualifiedName: "p.M", Kind: "function", Name: "M"},
			{ID: "n", QualifiedName: "p.N", Kind: "struct", Name: "N"},
		}, Relationships: []graph.Relationship{{FromID: "m", ToID: "n", Kind: "CALLS"}}},
		{Repo: "seg", Entities: []graph.Entity{
			{ID: "y", QualifiedName: "p.Y", Kind: "function", Name: "Y"},
			{ID: "z", QualifiedName: "p.Z", Kind: "struct", Name: "Z"},
		}, Relationships: []graph.Relationship{{FromID: "y", ToID: "a", Kind: "REFERENCES"}}},
	}
}

// TestGCStaleGens_RemovesStaleGenDir: GC rm -rf's a stale multi-segment gen dir
// while keeping the current + immediately-previous generations (test req v).
func TestGCStaleGens_RemovesStaleGenDir(t *testing.T) {
	dir := t.TempDir()
	// Three segmented generations: 1 (stale), 2 (previous), 3 (current).
	for gen := uint64(1); gen <= 3; gen++ {
		writeSegmentSet(t, dir, gen, threeSegDocs())
	}
	removed := graph.GCStaleGens(dir, 3)

	// gen 1 dir must be gone (rm -rf, including its segments + manifest).
	if _, err := os.Stat(filepath.Join(dir, graph.GenDirName(1))); !os.IsNotExist(err) {
		t.Fatalf("stale gen dir graph.1 not removed (err=%v)", err)
	}
	foundGen1 := false
	for _, r := range removed {
		if r == graph.GenDirName(1) {
			foundGen1 = true
		}
	}
	if !foundGen1 {
		t.Errorf("GCStaleGens removed=%v, want it to include %q", removed, graph.GenDirName(1))
	}
	// current (3) and previous (2) dirs must survive intact.
	for _, keep := range []uint64{2, 3} {
		p := filepath.Join(dir, graph.GenDirName(keep), graph.SegmentFileName(0))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("gen %d segment removed but should be kept: %v", keep, err)
		}
	}
}

// TestGCStaleGens_MixedFileAndDirKeepWindow: a history mixing single-file gens
// and segmented gen dirs still keeps exactly the two newest generations.
func TestGCStaleGens_MixedFileAndDirKeepWindow(t *testing.T) {
	dir := t.TempDir()
	// gen 1: single-file. gen 2: single-file. gen 3: segmented dir.
	for gen := uint64(1); gen <= 2; gen++ {
		if err := os.WriteFile(filepath.Join(dir, graph.GenFileName(gen)), []byte("filegen00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSegmentSet(t, dir, 3, threeSegDocs())

	graph.GCStaleGens(dir, 3)

	// gen 1 (file) stale → removed; gen 2 (file) + gen 3 (dir) kept.
	if _, err := os.Stat(filepath.Join(dir, graph.GenFileName(1))); !os.IsNotExist(err) {
		t.Errorf("stale single-file gen 1 not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, graph.GenFileName(2))); err != nil {
		t.Errorf("previous single-file gen 2 removed but should be kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, graph.GenDirName(3))); err != nil {
		t.Errorf("current segmented gen 3 removed but should be kept: %v", err)
	}
}

// TestGCStaleGens_FailSoftOnHeldSegment: GC never fails even when a stale gen
// dir's segment is still open (mapped). We open a reader over gen 1's segments,
// then GC — on Unix the RemoveAll succeeds (unlinked-but-open is fine), on
// Windows it fails soft. Either way GC returns without panicking and the
// current/previous gens survive; the held reader stays valid until Close.
func TestGCStaleGens_FailSoftOnHeldSegment(t *testing.T) {
	dir := t.TempDir()
	for gen := uint64(1); gen <= 3; gen++ {
		writeSegmentSet(t, dir, gen, threeSegDocs())
	}
	// Hold gen 1 open across the GC.
	v, err := graph.OpenSegmentReader(filepath.Join(dir, graph.GenDirName(1)))
	if err != nil {
		t.Fatal(err)
	}
	// The held reader must remain readable regardless of the GC outcome.
	defer func() {
		if v.EntityCount() != 6 {
			t.Errorf("held reader unreadable after GC: EntityCount=%d", v.EntityCount())
		}
		_ = v.Close()
	}()

	// Must not panic / propagate.
	graph.GCStaleGens(dir, 3)

	// current + previous survive irrespective of the held handle.
	for _, keep := range []uint64{2, 3} {
		if _, err := os.Stat(filepath.Join(dir, graph.GenDirName(keep))); err != nil {
			t.Errorf("gen %d removed but should be kept: %v", keep, err)
		}
	}
}

// TestReaderForDir_SegmentSet: ReaderForDir opens a segment-set and the unified
// view sees entities/relationships across ALL segments (EntityCount, lookups,
// iteration), including cross-segment relationship endpoints.
func TestReaderForDir_SegmentSet(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 3, threeSegDocs())

	v, err := graph.ReaderForDir(dir)
	if err != nil {
		t.Fatalf("ReaderForDir: %v", err)
	}
	// Windows teardown lesson: close the view before t.TempDir cleanup unmaps.
	t.Cleanup(func() { _ = v.Close() })

	if got := v.EntityCount(); got != 6 {
		t.Errorf("EntityCount across segments = %d, want 6", got)
	}
	if got := v.RelationshipCount(); got != 3 {
		t.Errorf("RelationshipCount across segments = %d, want 3", got)
	}
	for _, id := range []string{"a", "b", "m", "n", "y", "z"} {
		if e := v.LookupEntityByID(id); e == nil || string(e.Id()) != id {
			t.Errorf("LookupEntityByID(%q) failed across segments: %v", id, e)
		}
	}
	// Cross-segment relationship: y (seg2) -> a (seg0).
	seen := false
	v.IterateRelationships(func(rel *fbgraph.Relationship) bool {
		if string(rel.FromId()) == "y" && string(rel.ToId()) == "a" {
			seen = true
			return false
		}
		return true
	})
	if !seen {
		t.Error("cross-segment relationship y->a not iterated")
	}
}

// TestLoadGraphFromDir_SegmentSet: the full-Document loader materializes a
// segment-set (the serve/CLI read path) with all entities + relationships.
func TestLoadGraphFromDir_SegmentSet(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 4, threeSegDocs())

	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir(segment-set): %v", err)
	}
	if len(doc.Entities) != 6 {
		t.Errorf("Document.Entities = %d, want 6", len(doc.Entities))
	}
	if len(doc.Relationships) != 3 {
		t.Errorf("Document.Relationships = %d, want 3", len(doc.Relationships))
	}
	if doc.Stats.Entities != 6 || doc.Stats.Relationships != 3 {
		t.Errorf("Stats = %+v, want 6 ents / 3 rels", doc.Stats)
	}
}

// TestReaderForDir_SingleFileParity: ReaderForDir over a single-file gen graph
// returns exactly what today's fbreader.Open(CurrentGraphPath(dir)) does — same
// counts, same lookups. The common path is unchanged.
func TestReaderForDir_SingleFileParity(t *testing.T) {
	dir := t.TempDir()
	doc := &graph.Document{Repo: "single", Entities: []graph.Entity{
		{ID: "aaaa0001", QualifiedName: "p.A", Kind: "function", Name: "A"},
		{ID: "aaaa0002", QualifiedName: "p.B", Kind: "struct", Name: "B"},
	}, Relationships: []graph.Relationship{{FromID: "aaaa0001", ToID: "aaaa0002", Kind: "CALLS"}}}
	// Single-file gen: write graph.1.fb + point current at it.
	if err := fbwriter.WriteAtomic(filepath.Join(dir, graph.GenFileName(1)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(dir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}

	v, err := graph.ReaderForDir(dir)
	if err != nil {
		t.Fatalf("ReaderForDir(single-file): %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	if v.EntityCount() != 2 || v.RelationshipCount() != 1 {
		t.Fatalf("single-file counts = %d/%d, want 2/1", v.EntityCount(), v.RelationshipCount())
	}
	if e := v.LookupEntityByID("aaaa0002"); e == nil || string(e.Id()) != "aaaa0002" {
		t.Fatalf("single-file lookup failed: %v", e)
	}
}

// TestCurrentGraphDescriptor_SegmentSet: a gen-dir + manifest + current pointer
// resolves to GraphSegmentSet with the segment paths in manifest order.
func TestCurrentGraphDescriptor_SegmentSet(t *testing.T) {
	dir := t.TempDir()
	genDir := writeSegmentSet(t, dir, 3, threeSegDocs())

	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatalf("CurrentGraphDescriptor: %v", err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("Kind = %v, want GraphSegmentSet", desc.Kind)
	}
	if desc.GenDir != genDir {
		t.Errorf("GenDir = %q, want %q", desc.GenDir, genDir)
	}
	if len(desc.Segments) != 3 {
		t.Fatalf("Segments = %d, want 3", len(desc.Segments))
	}
	if desc.Manifest == nil || desc.Manifest.TotalEntityCount() != 6 {
		t.Fatalf("manifest missing or wrong entity total: %+v", desc.Manifest)
	}
}

// TestCurrentGraphDescriptor_ManifestPointer: current may name the manifest
// itself (graph.<gen>/manifest.json), not just the dir.
func TestCurrentGraphDescriptor_ManifestPointer(t *testing.T) {
	dir := t.TempDir()
	writeSegmentSet(t, dir, 5, threeSegDocs())
	// Re-point current at the manifest path form.
	if err := graph.WriteCurrentPointerRaw(dir, graph.GenDirName(5)+"/"+graph.ManifestFileName); err != nil {
		t.Fatal(err)
	}
	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("manifest-pointer form: Kind = %v, want GraphSegmentSet", desc.Kind)
	}
}

// TestCurrentGraphDescriptor_SingleFileAndLegacy: single-file gen + legacy flat
// both resolve to GraphSingleFile with the SAME path CurrentGraphPath returns
// (parity — the common path is unchanged).
func TestCurrentGraphDescriptor_SingleFileAndLegacy(t *testing.T) {
	// Single-file gen.
	dir1 := t.TempDir()
	genPath, err := graph.WriteGenGraph(dir1, []byte("00000000")) // 8+ bytes for fbreader open guard elsewhere
	if err != nil {
		t.Fatal(err)
	}
	d1, err := graph.CurrentGraphDescriptor(dir1)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Kind != graph.GraphSingleFile || d1.Path != genPath || d1.Path != graph.CurrentGraphPath(dir1) {
		t.Fatalf("single-file gen: %+v (CurrentGraphPath=%q)", d1, graph.CurrentGraphPath(dir1))
	}

	// Legacy flat.
	dir2 := t.TempDir()
	flat := filepath.Join(dir2, "graph.fb")
	if err := os.WriteFile(flat, []byte("legacybytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, err := graph.CurrentGraphDescriptor(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if d2.Kind != graph.GraphSingleFile || d2.Path != flat || d2.Path != graph.CurrentGraphPath(dir2) {
		t.Fatalf("legacy flat: %+v (CurrentGraphPath=%q)", d2, graph.CurrentGraphPath(dir2))
	}
}

// TestCurrentGraphDescriptor_Absent: a never-indexed dir resolves to
// GraphAbsent with Path == the flat path (parity with CurrentGraphPath).
func TestCurrentGraphDescriptor_Absent(t *testing.T) {
	dir := t.TempDir()
	d, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != graph.GraphAbsent {
		t.Fatalf("Kind = %v, want GraphAbsent", d.Kind)
	}
	if d.Path != graph.CurrentGraphPath(dir) {
		t.Errorf("absent Path = %q, want flat %q", d.Path, graph.CurrentGraphPath(dir))
	}
}

// TestCurrentGraphDescriptor_MalformedManifestErrors: a segment-set pointer to
// a gen dir whose manifest is corrupt surfaces an error (loud, not silent).
func TestCurrentGraphDescriptor_MalformedManifestErrors(t *testing.T) {
	dir := t.TempDir()
	genDir := filepath.Join(dir, graph.GenDirName(2))
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, graph.ManifestFileName), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointerRaw(dir, graph.GenDirName(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.CurrentGraphDescriptor(dir); err == nil {
		t.Fatal("expected error for corrupt segment-set manifest, got nil")
	}
}

// TestCurrentGraphDescriptor_MissingGenDirFallsBack: a pointer to a gen dir that
// does not exist falls back to the flat file (never an error), matching
// CurrentGraphPath's missing-gen fallback.
func TestCurrentGraphDescriptor_MissingGenDirFallsBack(t *testing.T) {
	dir := t.TempDir()
	flat := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(flat, []byte("flatbytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointerRaw(dir, graph.GenDirName(9)); err != nil {
		t.Fatal(err)
	}
	d, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != graph.GraphSingleFile || d.Path != flat {
		t.Fatalf("missing gen dir should fall back to flat: %+v", d)
	}
}
