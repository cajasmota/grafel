package dashboard

// graphstate_s8_test.go — Tests for S8 mmap reader lifecycle in DashRepo
// (issue #2159).
//
// These tests verify:
//  1. closeDashGroupReaders closes Reader fields and sets them nil.
//  2. Invalidate closes readers before dropping the cache entry.

import (
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
	"github.com/cajasmota/archigraph/internal/graph"
	"path/filepath"
)

// openTestReader writes a minimal graph.fb and opens an fbreader.Reader.
func openTestReader(t *testing.T) *fbreader.Reader {
	t.Helper()
	doc := &graph.Document{
		Repo: "s8-test",
		Entities: []graph.Entity{
			{ID: "x1", Kind: "function", Name: "Foo"},
		},
	}
	path := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := fbreader.Open(path)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	return r
}

// TestCloseDashGroupReaders verifies that closeDashGroupReaders closes
// every Reader in a DashGroup and sets them to nil.
func TestCloseDashGroupReaders(t *testing.T) {
	r1 := openTestReader(t)
	r2 := openTestReader(t)

	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"repo1": {Slug: "repo1", Reader: r1},
			"repo2": {Slug: "repo2", Reader: r2},
			"repo3": {Slug: "repo3"}, // no reader — must not panic
		},
	}

	closeDashGroupReaders(grp)

	for slug, dr := range grp.Repos {
		if dr.Reader != nil {
			t.Errorf("repo %s: Reader not nil after closeDashGroupReaders", slug)
		}
	}

	// Calling again is idempotent (no panic on nil Reader).
	closeDashGroupReaders(grp)

	// Nil grp is safe.
	closeDashGroupReaders(nil)
}

// TestInvalidateClosesReaders verifies that GraphCache.Invalidate closes
// the mmap readers held by the evicted entry.
func TestInvalidateClosesReaders(t *testing.T) {
	r := openTestReader(t)

	c := NewGraphCache(60 * time.Second)
	grp := &DashGroup{
		Name: "mygroup",
		Repos: map[string]*DashRepo{
			"repo1": {Slug: "repo1", Reader: r},
		},
	}
	c.mu.Lock()
	c.entries["mygroup"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	c.mu.Unlock()

	// Reader is open; Close should succeed (no error on a valid fd).
	// We can't easily verify from outside the package that the fd is gone
	// (that would require lsof), but we verify the Reader field is nil
	// after Invalidate calls closeDashGroupReaders.
	c.Invalidate("mygroup")

	if grp.Repos["repo1"].Reader != nil {
		t.Error("Reader still set after Invalidate — mmap not released")
	}
}
