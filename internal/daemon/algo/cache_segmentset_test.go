package algo

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// makeSegmentSetStateDir hand-builds a 1-segment gen dir (graph.<gen>/ with
// seg-0000.fb + manifest.json) under a fresh temp state dir and points
// `current` at it — mirroring the fixture builders at
// internal/daemon/state_path_graphfbexists_test.go /
// internal/graph/segmentset_test.go. No flat graph.fb is ever written.
func makeSegmentSetStateDir(t *testing.T, gen uint64) string {
	t.Helper()
	dir := t.TempDir()
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	genDir := filepath.Join(dir, graph.GenDirName(gen))
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
	if err := graph.WriteCurrentPointerRaw(dir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
	return dir
}

// TestCacheMissComputesAndPersists_SegmentSet is the RED test for #5915 J2
// slice-3: a segment-set state dir (graph.<gen>/ dir + manifest.json, no flat
// graph.fb) must still persist algo_results.fb after a compute. Before the
// fix, writeToDisk's os.Stat(graph.CurrentGraphPath(stateDir)) always failed
// for a segment-set dir (no flat .fb to stat), so the cache was silently
// never written to disk.
func TestCacheMissComputesAndPersists_SegmentSet(t *testing.T) {
	dir := makeSegmentSetStateDir(t, 3)

	var calls atomic.Int32
	c := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})

	r, err := c.Get(context.Background(), dir, "/repo", "main")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 compute call, got %d", calls.Load())
	}
	if r.PageRank["a"] != 0.5 {
		t.Errorf("unexpected PageRank: %v", r.PageRank)
	}
	if _, err := os.Stat(filepath.Join(dir, cacheFileName)); err != nil {
		t.Errorf("algo_results.fb not written for segment-set state dir: %v", err)
	}
}

// TestCacheHitServesFromDisk_SegmentSet is the RED test guarding that a
// segment-set state dir's disk cache is actually served on a second Get
// (not recomputed every time). Before the fix, readFromDisk's
// os.Stat(graph.CurrentGraphPath(stateDir)) always failed for a segment-set
// dir, so every call recomputed.
func TestCacheHitServesFromDisk_SegmentSet(t *testing.T) {
	dir := makeSegmentSetStateDir(t, 3)

	var calls atomic.Int32
	c := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})

	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	// New Cache instance simulates a fresh in-memory state — must still hit
	// the disk cache.
	c2 := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})
	if _, err := c2.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 total compute call (second should be disk hit) for segment-set state dir, got %d", calls.Load())
	}
}

// TestStaleCacheRecomputesWhenSegmentSetGenAdvances guards that a fresh
// segment-set generation (a reindex that flips `current` to a new gen dir,
// advancing the manifest.json mtime) invalidates the disk cache, mirroring
// TestStaleCacheRecomputesWhenGraphFBUpdated for the single-file case.
func TestStaleCacheRecomputesWhenSegmentSetGenAdvances(t *testing.T) {
	dir := makeSegmentSetStateDir(t, 3)

	var calls atomic.Int32
	c := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})

	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	// Simulate a reindex: write a NEW generation and flip `current` to it.
	// Sleep to ensure the manifest.json mtime advances beyond the staleness
	// tolerance (1s).
	time.Sleep(1100 * time.Millisecond)
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "bb2", QualifiedName: "p.B", Kind: "function", Name: "B"},
	}}
	newGenDir := filepath.Join(dir, graph.GenDirName(4))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(newGenDir, name), doc); err != nil {
		t.Fatalf("write new gen %s: %v", name, err)
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities, EntityCount: 1,
		MinKey: "bb2", MaxKey: "bb2",
	}}}
	if err := graph.WriteManifest(newGenDir, m); err != nil {
		t.Fatalf("write new manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(dir, graph.GenDirName(4)); err != nil {
		t.Fatalf("flip current: %v", err)
	}

	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected recompute after segment-set gen advance, got %d compute calls", calls.Load())
	}
}
