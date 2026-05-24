package walk_test

// TestWalkRepo_SparseFilter exercises Layer 5 (P4) sparse-checkout filtering.
// It creates a fixture directory tree simulating a sparse checkout where only
// "services/payments" is in the sparse pattern set.  The walker must:
//   - Return files under services/payments/ without error.
//   - Silently skip files under services/orders/ (not in sparse set).
//   - Behave identically to a full walk when IsSparse=false.
//
// Issue #2181 / M4 of epic #2175.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/walk"
	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// mkTree creates the fixture file tree and returns the root.
//
//	root/
//	  services/
//	    payments/
//	      handler.go       ← in sparse set
//	      service.go       ← in sparse set
//	    orders/
//	      handler.go       ← NOT in sparse set
//	  cmd/main.go          ← NOT in sparse set
func mkSparseFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := []string{
		"services/payments/handler.go",
		"services/payments/service.go",
		"services/orders/handler.go",
		"cmd/main.go",
	}
	for _, f := range files {
		abs := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestWalkRepo_SparseFilter_ConeMode verifies that cone-mode sparse filtering
// returns only the files under the sparse pattern prefix and silently skips the
// rest.
func TestWalkRepo_SparseFilter_ConeMode(t *testing.T) {
	root := mkSparseFixtureTree(t)

	si := &gitmeta.SparseInfo{
		IsSparse: true,
		ConeMode: true,
		Patterns: []string{"services/payments"},
	}
	opts := &walk.Options{Sparse: si}

	files, _, err := walk.WalkRepo(root, opts)
	if err != nil {
		t.Fatalf("WalkRepo: unexpected error: %v", err)
	}

	sort.Strings(files)
	want := []string{
		"services/payments/handler.go",
		"services/payments/service.go",
	}
	if len(files) != len(want) {
		t.Fatalf("WalkRepo sparse: got %d files %v, want %d %v", len(files), files, len(want), want)
	}
	for i, f := range files {
		if f != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, f, want[i])
		}
	}

	// Verify excluded paths are not present.
	for _, f := range files {
		if strings.HasPrefix(f, "services/orders") || strings.HasPrefix(f, "cmd/") {
			t.Errorf("unexpected file in sparse walk: %q", f)
		}
	}
}

// TestWalkRepo_SparseFilter_NonSparse verifies that when IsSparse=false (or
// Sparse is nil) the walker returns all source files regardless of what the
// pattern set contains.
func TestWalkRepo_SparseFilter_NonSparse(t *testing.T) {
	root := mkSparseFixtureTree(t)

	// nil Sparse → no filtering.
	files, _, err := walk.WalkRepo(root, nil)
	if err != nil {
		t.Fatalf("WalkRepo(nil opts): unexpected error: %v", err)
	}

	sort.Strings(files)
	wantMin := []string{
		"cmd/main.go",
		"services/orders/handler.go",
		"services/payments/handler.go",
		"services/payments/service.go",
	}
	for _, w := range wantMin {
		found := false
		for _, f := range files {
			if f == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in full walk result, got %v", w, files)
		}
	}
}

// TestWalkRepo_SparseFilter_EmptyPatterns verifies that a sparse repo with an
// empty pattern list returns zero files (nothing is checked out locally).
func TestWalkRepo_SparseFilter_EmptyPatterns(t *testing.T) {
	root := mkSparseFixtureTree(t)

	si := &gitmeta.SparseInfo{
		IsSparse: true,
		ConeMode: true,
		Patterns: nil, // no patterns → nothing included
	}
	opts := &walk.Options{Sparse: si}

	files, _, err := walk.WalkRepo(root, opts)
	if err != nil {
		t.Fatalf("WalkRepo: unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files for sparse repo with empty patterns, got %d: %v", len(files), files)
	}
}
