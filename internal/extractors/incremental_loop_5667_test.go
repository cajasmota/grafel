package extractors_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// TestTryIncremental_FlushesStaleManifest_NoLoop is the end-to-end regression
// test for #5667: a manifest carrying entries for files no longer in the walk
// (e.g. build artifacts excluded by the gitignore-aware walk in 0.1.7.1) must
// NOT loop the reindex forever.
//
// Before the fix, those absent entries were detected as deletedFiles on every
// pass → too-many-changed → fallback that DISCARDED the manifest GC → next pass
// saw the same absent entries again → infinite full-reindex loop at a static
// HEAD. The fix persists the GC'd manifest on the fallback (and UpdateManifest
// now prunes), so the stale entries are flushed once and a second pass settles.
func TestTryIncremental_FlushesStaleManifest_NoLoop(t *testing.T) {
	t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")

	repo := t.TempDir()
	stateDir := t.TempDir()
	writeFile(t, repo, "a.go", "package p\n\nfunc A() {}\n")
	writeFile(t, repo, "b.go", "package p\n\nfunc B() {}\n")

	// Seed a manifest with correct hashes for the two real files plus 60 stale
	// entries for files that are NOT on disk (above even the main-branch limit
	// of 50, so the fallback fires regardless of branch heuristics).
	m := &diff.Manifest{Version: 1, Files: map[string]diff.FileEntry{}}
	diff.UpdateManifest(repo, []string{"a.go", "b.go"}, m)
	for i := 0; i < 60; i++ {
		m.Files[fmt.Sprintf("build/ghost_%d.txt", i)] = diff.FileEntry{SHA256: "stale"}
	}
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatal(err)
	}

	logger := log.New(io.Discard, "", 0)

	// Cycle 1: the 60 absent entries trip too-many-changed → fallback. The fix
	// must PERSIST the manifest GC before returning.
	r1 := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	if r1.Done {
		t.Fatalf("cycle 1 should fall back (too-many-changed), got Done")
	}
	if !strings.Contains(r1.FallbackReason, "too-many-changed") {
		t.Fatalf("cycle 1 expected a too-many-changed fallback, got %q", r1.FallbackReason)
	}

	// The persisted manifest must be free of the stale entries now.
	got := diff.LoadManifest(stateDir)
	for k := range got.Files {
		if strings.HasPrefix(k, "build/") {
			t.Fatalf("stale entry %q survived the fallback — the reindex would loop forever (#5667)", k)
		}
	}

	// Cycle 2: with a flushed manifest and a static tree, nothing is deleted or
	// changed → totalChanged==0 → Done, NOT another fallback. This is the proof
	// that the loop terminates.
	r2 := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	if !r2.Done {
		t.Fatalf("cycle 2 must be Done (loop terminated), got fallback=%q", r2.FallbackReason)
	}
}
