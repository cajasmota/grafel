package mcp

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

// writeFixtureGraph emits a tiny but valid graph.fb under dir and
// returns its path. Two entities, one relationship — enough to exercise
// lookup + iteration without hauling in test corpora.
func writeFixtureGraph(t *testing.T, dir, tag string, modTime time.Time) string {
	t.Helper()
	doc := &graph.Document{
		Repo:        tag,
		GeneratedAt: time.Unix(0, 0).UTC(),
		Entities: []graph.Entity{
			{ID: tag + "::a", QualifiedName: "pkg.A", Kind: "function", Name: "A"},
			{ID: tag + "::b", QualifiedName: "pkg.B", Kind: "struct", Name: "B"},
		},
		Relationships: []graph.Relationship{
			{FromID: tag + "::a", ToID: tag + "::b", Kind: "CALLS"},
		},
	}
	path := filepath.Join(dir, tag+".fb")
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatalf("fbwriter: %v", err)
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	return path
}

func TestCacheHitMiss(t *testing.T) {
	dir := t.TempDir()
	p := writeFixtureGraph(t, dir, "x", time.Time{})
	c := NewCache(4)
	defer c.Close()

	r1, rel1, err := c.Get(p)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if r1.EntityCount() != 2 {
		t.Fatalf("entity count = %d, want 2", r1.EntityCount())
	}
	rel1()

	if s := c.Stats(); s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("after miss: %+v", s)
	}

	r2, rel2, err := c.Get(p)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if r2 != r1 {
		t.Fatalf("expected same reader on cache hit")
	}
	rel2()
	if s := c.Stats(); s.Hits != 1 {
		t.Fatalf("after hit: %+v", s)
	}
}

func TestCacheLRUEviction(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		writeFixtureGraph(t, dir, "a", time.Time{}),
		writeFixtureGraph(t, dir, "b", time.Time{}),
		writeFixtureGraph(t, dir, "c", time.Time{}),
	}
	c := NewCache(2)
	defer c.Close()

	for _, p := range paths {
		_, rel, err := c.Get(p)
		if err != nil {
			t.Fatalf("Get %s: %v", p, err)
		}
		rel()
	}
	if got := c.Len(); got != 2 {
		t.Fatalf("len = %d, want 2", got)
	}
	if s := c.Stats(); s.Evictions != 1 {
		t.Fatalf("evictions = %d, want 1", s.Evictions)
	}
}

func TestCacheInvalidate(t *testing.T) {
	dir := t.TempDir()
	p := writeFixtureGraph(t, dir, "x", time.Time{})
	c := NewCache(4)
	defer c.Close()

	_, rel, _ := c.Get(p)
	rel()
	if !c.Invalidate(p) {
		t.Fatalf("Invalidate returned false")
	}
	if c.Len() != 0 {
		t.Fatalf("len = %d after invalidate", c.Len())
	}
	if s := c.Stats(); s.Invalidates != 1 {
		t.Fatalf("invalidates = %d", s.Invalidates)
	}
}

func TestCacheMtimeRefresh(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-1 * time.Hour)
	p := writeFixtureGraph(t, dir, "x", old)
	c := NewCache(4)
	defer c.Close()

	_, rel, err := c.Get(p)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	rel()

	// Rewrite under the same path with a newer mtime.
	now := time.Now()
	writeFixtureGraph(t, dir, "x", now)

	_, rel2, err := c.Get(p)
	if err != nil {
		t.Fatalf("Get after rewrite: %v", err)
	}
	rel2()
	if s := c.Stats(); s.Invalidates < 1 {
		t.Fatalf("expected mtime-driven invalidate, stats=%+v", s)
	}
}

func TestCacheConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	p := writeFixtureGraph(t, dir, "x", time.Time{})
	c := NewCache(4)
	defer c.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, rel, err := c.Get(p)
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			defer rel()
			if r.LookupEntityByID("x::a") == nil {
				t.Errorf("lookup miss")
			}
		}()
	}
	wg.Wait()
}

func TestCacheClosedRejects(t *testing.T) {
	c := NewCache(2)
	c.Close()
	if _, _, err := c.Get("nonexistent"); err != ErrCacheClosed {
		t.Fatalf("expected ErrCacheClosed, got %v", err)
	}
}

// TestGetForRepoRef_UnknownRefRefused verifies that GetForRepoRef returns
// ErrUnknownRef when the resolved state-dir falls under refs/_unknown/ (#2141
// root-cause D). The _unknown artifact should stay on disk but never be loaded
// into heap through the ref-aware entry point.
func TestGetForRepoRef_UnknownRefRefused(t *testing.T) {
	t.Setenv("ARCHIGRAPH_HOME", t.TempDir())
	// Calling with ref="" causes StateDirForRepoRef to return …/refs/_unknown/
	c := NewCache(4)
	defer c.Close()

	_, _, err := c.GetForRepoRef("/some/repo", "")
	if err != ErrUnknownRef {
		t.Fatalf("GetForRepoRef with empty ref: want ErrUnknownRef, got %v", err)
	}
}

// TestGetForRepoRef_KnownRefLoads verifies that GetForRepoRef with a known ref
// proceeds to the normal Get path (returns ErrCacheMiss / open error rather
// than ErrUnknownRef). This ensures the guard does not block legitimate refs.
func TestGetForRepoRef_KnownRefLoads(t *testing.T) {
	t.Setenv("ARCHIGRAPH_HOME", t.TempDir())
	c := NewCache(4)
	defer c.Close()

	_, _, err := c.GetForRepoRef("/some/repo", "main")
	// We expect either a stat/open error (file doesn't exist in the temp store)
	// but NOT ErrUnknownRef and NOT ErrCacheClosed.
	if err == ErrUnknownRef {
		t.Fatalf("GetForRepoRef with ref=main must not return ErrUnknownRef")
	}
	if err == ErrCacheClosed {
		t.Fatalf("GetForRepoRef with ref=main must not return ErrCacheClosed")
	}
	// An open/stat error is expected since the file doesn't exist.
	if err == nil {
		t.Fatalf("GetForRepoRef with non-existent path should return an error")
	}
}
