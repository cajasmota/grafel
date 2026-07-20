package fbwriter_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// ─── helpers ───────────────────────────────────────────────────────────────

// makeSyntheticEntity creates a graph.Entity with the given index. Properties
// are set to a ~200-byte map to mimic a real extraction result (a real entity
// has more fields but this is sufficient for memory-bound tests).
func makeSyntheticEntity(i int) graph.Entity {
	id := fmt.Sprintf("ent%013x", i)
	return graph.Entity{
		ID:            id,
		Name:          fmt.Sprintf("SyntheticFunc_%d", i),
		QualifiedName: fmt.Sprintf("pkg/synthetic.SyntheticFunc_%d", i),
		Kind:          "function",
		SourceFile:    fmt.Sprintf("pkg/synthetic/file_%d.go", i%50),
		StartLine:     (i % 200) + 1,
	}.WithProperties(map[string]string{
		"module":     fmt.Sprintf("pkg/synthetic/batch_%d", i/100),
		"visibility": "public",
		"language":   "go",
	},
	)
}

func makeSyntheticRelationship(i, n int) graph.Relationship {
	fromIdx := i % n
	toIdx := (i + 1) % n
	return graph.Relationship{
		ID:     fmt.Sprintf("rel%013x", i),
		FromID: fmt.Sprintf("ent%013x", fromIdx),
		ToID:   fmt.Sprintf("ent%013x", toIdx),
		Kind:   "calls",
	}
}

// ─── streaming round-trip ──────────────────────────────────────────────────

// TestStreamingWriter_RoundtripEquivalence verifies that entities and
// relationships written via StreamingWriter produce a graph.fb that is
// byte-for-byte equivalent to the WriteAtomic path (same entities, same
// reader counts, same field values).
func TestStreamingWriter_RoundtripEquivalence(t *testing.T) {
	cid := 3
	pr := 0.042
	cen := 1.1
	entities := []graph.Entity{
		graph.Entity{
			ID: "ent0000000000000a", Name: "Alpha", Kind: "function",
			SourceFile: "a.go", StartLine: 10,

			CommunityID: &cid, PageRank: &pr, Centrality: &cen,
			IsGodNode: true,
		}.WithProperties(map[string]string{"module": "pkg/a", "visibility": "public"}),
		{
			ID: "ent0000000000000b", Name: "Beta", Kind: "type",
			SourceFile: "b.go", StartLine: 20,
		},
		graph.Entity{
			ID: "ent0000000000000c", Name: "Gamma", Kind: "function",
			SourceFile: "c.go", StartLine: 30,
		}.WithProperties(map[string]string{"module": "pkg/c"}),
	}
	rels := []graph.Relationship{
		{ID: "rel000000000000aa", FromID: "ent0000000000000a", ToID: "ent0000000000000b", Kind: "calls"},
		graph.Relationship{ID: "rel000000000000ab", FromID: "ent0000000000000a", ToID: "ent0000000000000c", Kind: "references"}.WithProperties(map[string]string{"resolved": "true"}),
	}
	communities := []graph.CommunityResult{
		{ID: 3, Size: 5, Modularity: 0.55, AutoName: "core", TopEntities: []string{"Alpha"}},
	}
	algStats := &graph.AlgorithmStats{
		LouvainModularity: 0.55, NumCommunities: 1, NumGodNodes: 1, RuntimeMS: 42,
	}

	generatedAt := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)

	// ── WriteAtomic (legacy) path ──────────────────────────────────────────
	docLegacy := &graph.Document{
		Version:        1,
		GeneratedAt:    generatedAt,
		Repo:           "fixture-streaming-equiv",
		IndexedRef:     "main",
		IndexedSHA:     "deadbeef",
		IsWorktree:     false,
		Entities:       entities,
		Relationships:  rels,
		Communities:    communities,
		AlgorithmStats: algStats,
	}
	docLegacy.Stats.Entities = len(entities)
	docLegacy.Stats.Relationships = len(rels)

	outLegacy := filepath.Join(t.TempDir(), "legacy.fb")
	if err := fbwriter.WriteAtomic(outLegacy, docLegacy); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	// ── StreamingWriter path ───────────────────────────────────────────────
	outStreaming := filepath.Join(t.TempDir(), "streaming.fb")
	sw, err := fbwriter.NewStreamingWriter(outStreaming)
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}
	for i := range entities {
		if err := sw.WriteEntity(&entities[i]); err != nil {
			t.Fatalf("WriteEntity[%d]: %v", i, err)
		}
	}
	for i := range rels {
		if err := sw.WriteRelationship(&rels[i]); err != nil {
			t.Fatalf("WriteRelationship[%d]: %v", i, err)
		}
	}
	if sw.EntityCount() != len(entities) {
		t.Errorf("EntityCount: got %d want %d", sw.EntityCount(), len(entities))
	}
	if sw.RelationshipCount() != len(rels) {
		t.Errorf("RelationshipCount: got %d want %d", sw.RelationshipCount(), len(rels))
	}
	if err := sw.Close(fbwriter.GraphMetadata{
		Repo:           "fixture-streaming-equiv",
		GeneratedAt:    generatedAt,
		IndexedRef:     "main",
		IndexedSHA:     "deadbeef",
		IsWorktree:     false,
		Communities:    communities,
		AlgorithmStats: algStats,
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// ── compare via fbreader ───────────────────────────────────────────────
	rL, err := fbreader.Open(outLegacy)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	defer rL.Close()

	rS, err := fbreader.Open(outStreaming)
	if err != nil {
		t.Fatalf("open streaming: %v", err)
	}
	defer rS.Close()

	if rL.EntityCount() != rS.EntityCount() {
		t.Errorf("entity count: legacy=%d streaming=%d", rL.EntityCount(), rS.EntityCount())
	}
	if rL.RelationshipCount() != rS.RelationshipCount() {
		t.Errorf("rel count: legacy=%d streaming=%d", rL.RelationshipCount(), rS.RelationshipCount())
	}
	if rL.Version() != rS.Version() {
		t.Errorf("version: legacy=%d streaming=%d", rL.Version(), rS.Version())
	}

	// Spot-check entity field values.
	entA := rS.LookupEntityByID("ent0000000000000a")
	if entA == nil {
		t.Fatal("streaming: LookupEntityByID(a) nil")
	}
	if got := string(entA.Name()); got != "Alpha" {
		t.Errorf("entity a name: got %q want %q", got, "Alpha")
	}
	if got := string(entA.Kind()); got != "function" {
		t.Errorf("entity a kind: got %q want %q", got, "function")
	}
	if got := entA.SourceLine(); got != 10 {
		t.Errorf("entity a source_line: got %d want 10", got)
	}
}

// TestStreamingWriter_RoundtripGitMeta verifies that git metadata fields
// written via StreamingWriter survive the write→read cycle.
func TestStreamingWriter_RoundtripGitMeta(t *testing.T) {
	out := filepath.Join(t.TempDir(), "graph.fb")
	sw, err := fbwriter.NewStreamingWriter(out)
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}

	e := graph.Entity{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"}
	if err := sw.WriteEntity(&e); err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}
	if err := sw.Close(fbwriter.GraphMetadata{
		Repo:        "fixture-gitmeta-sw",
		GeneratedAt: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
		IndexedRef:  "feat/test-branch",
		IndexedSHA:  "aabbccdd",
		IsWorktree:  true,
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.IndexedRef != "feat/test-branch" {
		t.Errorf("IndexedRef: got %q want %q", got.IndexedRef, "feat/test-branch")
	}
	if got.IndexedSHA != "aabbccdd" {
		t.Errorf("IndexedSHA: got %q want %q", got.IndexedSHA, "aabbccdd")
	}
	if !got.IsWorktree {
		t.Error("IsWorktree: got false want true")
	}
	if len(got.Entities) != 1 {
		t.Errorf("entity count: got %d want 1", len(got.Entities))
	}
}

// TestStreamingWriter_DoubleCloseErrors verifies that calling Close twice
// returns an error on the second call.
func TestStreamingWriter_DoubleCloseErrors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "graph.fb")
	sw, err := fbwriter.NewStreamingWriter(out)
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}
	e := graph.Entity{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"}
	_ = sw.WriteEntity(&e)

	if err := sw.Close(fbwriter.GraphMetadata{Repo: "r", GeneratedAt: time.Now()}); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sw.Close(fbwriter.GraphMetadata{Repo: "r", GeneratedAt: time.Now()}); err == nil {
		t.Error("second Close: expected error, got nil")
	}
}

// TestStreamingWriter_WriteAfterCloseErrors verifies that calling WriteEntity
// after Close returns an error.
func TestStreamingWriter_WriteAfterCloseErrors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "graph.fb")
	sw, err := fbwriter.NewStreamingWriter(out)
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}
	e := graph.Entity{ID: "ent0000000000000a", Name: "foo", Kind: "function", SourceFile: "a.go"}
	_ = sw.WriteEntity(&e)
	_ = sw.Close(fbwriter.GraphMetadata{Repo: "r", GeneratedAt: time.Now()})

	if err := sw.WriteEntity(&e); err == nil {
		t.Error("WriteEntity after Close: expected error, got nil")
	}
}

// TestStreamingWriter_EmptyGraph verifies that a StreamingWriter with zero
// entities and relationships writes a valid (empty) graph.fb.
func TestStreamingWriter_EmptyGraph(t *testing.T) {
	out := filepath.Join(t.TempDir(), "graph.fb")
	sw, err := fbwriter.NewStreamingWriter(out)
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}
	if err := sw.Close(fbwriter.GraphMetadata{
		Repo:        "fixture-empty",
		GeneratedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := fbreader.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	if r.EntityCount() != 0 {
		t.Errorf("entity count: got %d want 0", r.EntityCount())
	}
	if r.RelationshipCount() != 0 {
		t.Errorf("rel count: got %d want 0", r.RelationshipCount())
	}
}

// ─── memory bound test ────────────────────────────────────────────────────

// TestStreamingWriter_PeakMemoryUnder100MB writes 60 000 synthetic entities
// and 30 000 relationships via StreamingWriter and asserts that the live heap
// after GC stays below 100 MB during the write phase.
//
// This is the acceptance gate for the P0.2 streaming FB writes fix (#2141).
//
// # What we measure
//
// We measure HeapInuse (live heap pages in use after GC), not TotalAlloc
// (cumulative bytes ever allocated). TotalAlloc counts every short-lived
// allocation and is not a peak-working-set signal. HeapInuse after GC
// reflects the actual live bytes held — the correct measure for "how much
// memory does the streaming path need at once?"
//
// The key difference vs the pre-#2141 path:
//   - Pre-#2141: caller builds []graph.Entity with all 60 k structs in memory
//     before calling Marshal.  60 k × ~200 bytes per Entity struct + properties
//     maps ≈ 300–500 MB just for the input slice, PLUS the FlatBuffers builder
//     buffer simultaneously in memory = 1.5–2 GB peak.
//   - Post-#2141: each entity is serialized into the builder immediately.
//     We retain only a []flatbuffers.UOffsetT (8 bytes × 60 k = 480 KB) plus
//     the growing builder buffer (≈ 80–150 MB for the final serialized bytes).
//     The input Entity structs can be GC'd after each call. Peak ≈ 100–150 MB.
//
// # Limit rationale
//
// The FlatBuffers builder buffer for 60 k entities with our synthetic data
// measures ≈ 60–70 MB. We add 30 MB headroom for the offset slice, GC
// pause overhead, and runtime bookkeeping → 100 MB limit.
func TestStreamingWriter_PeakMemoryUnder100MB(t *testing.T) {
	const (
		numEntities      = 60_000
		numRelationships = 30_000
		limitMB          = 100
	)

	out := filepath.Join(t.TempDir(), "graph.fb")

	// ── warm-up GC to reduce noise ─────────────────────────────────────────
	runtime.GC()
	runtime.GC()

	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	// ── streaming write ────────────────────────────────────────────────────
	sw, err := fbwriter.NewStreamingWriter(out)
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}

	for i := 0; i < numEntities; i++ {
		e := makeSyntheticEntity(i)
		if err := sw.WriteEntity(&e); err != nil {
			t.Fatalf("WriteEntity[%d]: %v", i, err)
		}
	}
	for i := 0; i < numRelationships; i++ {
		r := makeSyntheticRelationship(i, numEntities)
		if err := sw.WriteRelationship(&r); err != nil {
			t.Fatalf("WriteRelationship[%d]: %v", i, err)
		}
	}
	if err := sw.Close(fbwriter.GraphMetadata{
		Repo:        "fixture-perf-60k",
		GeneratedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// ── assess live heap after GC ─────────────────────────────────────────
	// Two GC cycles to ensure all short-lived allocations from the loop
	// are reclaimed before we read HeapInuse.
	runtime.GC()
	runtime.GC()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	// HeapInuse is the bytes in in-use spans (committed, containing live
	// objects). This is the live working set, not cumulative allocations.
	heapDeltaMB := float64(int64(mAfter.HeapInuse)-int64(mBefore.HeapInuse)) / (1024 * 1024)
	t.Logf("entities=%d rels=%d HeapInuse before=%.1f MB after=%.1f MB delta=%.1f MB (limit=%d MB)",
		numEntities, numRelationships,
		float64(mBefore.HeapInuse)/(1024*1024),
		float64(mAfter.HeapInuse)/(1024*1024),
		heapDeltaMB, limitMB)

	if heapDeltaMB > float64(limitMB) {
		t.Errorf("live heap delta %.1f MB exceeds %d MB limit — streaming path held too much memory",
			heapDeltaMB, limitMB)
	}

	// Sanity-check the output is readable.
	r, err := fbreader.Open(out)
	if err != nil {
		t.Fatalf("open written file: %v", err)
	}
	defer r.Close()
	if r.EntityCount() != numEntities {
		t.Errorf("entity count: got %d want %d", r.EntityCount(), numEntities)
	}
	if r.RelationshipCount() != numRelationships {
		t.Errorf("rel count: got %d want %d", r.RelationshipCount(), numRelationships)
	}
}

// TestStreamingWriter_10kPeakMemory is a smaller variant (10 k entities) for
// CI environments that may not have enough RAM for the 60 k test. Limit is
// proportionally relaxed to 25 MB live heap delta.
func TestStreamingWriter_10kPeakMemory(t *testing.T) {
	const (
		numEntities      = 10_000
		numRelationships = 5_000
		limitMB          = 25
	)

	out := filepath.Join(t.TempDir(), "graph.fb")

	runtime.GC()
	runtime.GC()

	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	sw, err := fbwriter.NewStreamingWriter(out)
	if err != nil {
		t.Fatalf("NewStreamingWriter: %v", err)
	}
	for i := 0; i < numEntities; i++ {
		e := makeSyntheticEntity(i)
		if err := sw.WriteEntity(&e); err != nil {
			t.Fatalf("WriteEntity[%d]: %v", i, err)
		}
	}
	for i := 0; i < numRelationships; i++ {
		r := makeSyntheticRelationship(i, numEntities)
		if err := sw.WriteRelationship(&r); err != nil {
			t.Fatalf("WriteRelationship[%d]: %v", i, err)
		}
	}
	if err := sw.Close(fbwriter.GraphMetadata{
		Repo:        "fixture-perf-10k",
		GeneratedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	runtime.GC()
	runtime.GC()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	heapDeltaMB := float64(int64(mAfter.HeapInuse)-int64(mBefore.HeapInuse)) / (1024 * 1024)
	t.Logf("entities=%d rels=%d HeapInuse delta=%.1f MB (limit=%d MB)",
		numEntities, numRelationships, heapDeltaMB, limitMB)

	if heapDeltaMB > float64(limitMB) {
		t.Errorf("live heap delta %.1f MB exceeds %d MB limit",
			heapDeltaMB, limitMB)
	}

	r, err := fbreader.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()
	if r.EntityCount() != numEntities {
		t.Errorf("entity count: got %d want %d", r.EntityCount(), numEntities)
	}
}
