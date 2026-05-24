package algo

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeResults() *Results {
	return &Results{
		PageRank:           map[string]float64{"a": 0.5, "b": 0.3},
		Centrality:         map[string]float64{"a": 1.0},
		CommunityID:        map[string]int{"a": 0, "b": 1},
		GodNodes:           map[string]bool{"a": true},
		ArticulationPoints: map[string]bool{},
		SurpriseEndpoints:  map[string]bool{},
	}
}

// makeStateDir creates a temp directory with a graph.fb sentinel file.
func makeStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Write a minimal graph.fb placeholder so mtime checks work.
	if err := os.WriteFile(filepath.Join(dir, "graph.fb"), []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	return dir
}

// TestCacheMissComputesAndPersists verifies that a cold cache triggers ComputeFn
// exactly once and writes algo_results.fb to disk.
func TestCacheMissComputesAndPersists(t *testing.T) {
	dir := makeStateDir(t)

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
	// algo_results.fb must exist on disk after the first call.
	if _, err := os.Stat(filepath.Join(dir, cacheFileName)); err != nil {
		t.Errorf("algo_results.fb not written: %v", err)
	}
}

// TestCacheHitServesFromDisk verifies that a second Get does NOT invoke ComputeFn.
func TestCacheHitServesFromDisk(t *testing.T) {
	dir := makeStateDir(t)

	var calls atomic.Int32
	c := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})

	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	// New Cache instance simulates a different goroutine / request with no
	// in-memory state — it must still hit the disk cache.
	c2 := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})
	if _, err := c2.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 total compute call (second should be disk hit), got %d", calls.Load())
	}
}

// TestInvalidateClearsCache verifies that after Invalidate the next Get
// recomputes (disk file removed).
func TestInvalidateClearsCache(t *testing.T) {
	dir := makeStateDir(t)

	var calls atomic.Int32
	c := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})

	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	if err := Invalidate(dir); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("second Get after invalidate: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 compute calls after invalidate, got %d", calls.Load())
	}
}

// TestStaleCacheRecomputesWhenGraphFBUpdated verifies that a graph.fb update
// (mtime advances) causes a cache miss even if algo_results.fb exists.
func TestStaleCacheRecomputesWhenGraphFBUpdated(t *testing.T) {
	dir := makeStateDir(t)

	var calls atomic.Int32
	c := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		return fakeResults(), nil
	})

	// Populate the cache.
	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	// Simulate a reindex by writing a newer graph.fb. Sleep a bit to ensure
	// mtime advances beyond the staleness tolerance (1s).
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "graph.fb"), []byte("updated"), 0o644); err != nil {
		t.Fatalf("update graph.fb: %v", err)
	}

	if _, err := c.Get(context.Background(), dir, "/repo", "main"); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected recompute after graph.fb update, got %d compute calls", calls.Load())
	}
}

// TestConcurrentGetsCoalesce verifies that many concurrent Gets for the same
// stateDir result in exactly one ComputeFn invocation (thundering-herd prevention).
func TestConcurrentGetsCoalesce(t *testing.T) {
	dir := makeStateDir(t)

	var calls atomic.Int32
	gate := make(chan struct{})
	c := New(func(_ context.Context, _, _ string) (*Results, error) {
		calls.Add(1)
		<-gate // hold until all goroutines have started
		return fakeResults(), nil
	})

	const N = 20
	var wg sync.WaitGroup
	errs := make([]error, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.Get(context.Background(), dir, "/repo", "main")
		}(i)
	}
	// Give all goroutines time to reach Get before unblocking compute.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: %v", i, e)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 compute call from %d concurrent Gets, got %d", N, calls.Load())
	}
}

// TestInvalidateIdempotent verifies that calling Invalidate twice does not error.
func TestInvalidateIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := Invalidate(dir); err != nil {
		t.Fatalf("first Invalidate on empty dir: %v", err)
	}
	if err := Invalidate(dir); err != nil {
		t.Fatalf("second Invalidate on empty dir: %v", err)
	}
}
