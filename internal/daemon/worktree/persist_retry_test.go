package worktree

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreSave_RetriesTransientTmpFailure verifies the persist path recovers
// from a transient `.tmp` write failure by retrying once (#5675), rather than
// dropping the reconcile. We simulate the transient failure by pre-creating the
// `.tmp` path as a directory: the first os.WriteFile fails ("is a directory"),
// the retry removes the stale entry and succeeds.
//
// It also asserts the path is NEVER fatal — save() only ever returns an error;
// nothing here exits the process.
func TestStoreSave_RetriesTransientTmpFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worktrees.json")

	s := NewStore(path)
	s.children = []*WorktreeChild{{
		ParentSlug:   "r",
		GroupName:    "g",
		Path:         "/some/worktree",
		Branch:       "main",
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
		Status:       StatusActive,
	}}

	// Inject a transient failure: the staging path exists as a directory, so
	// the first WriteFile fails; the retry's os.Remove clears it.
	tmp := path + ".tmp"
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatalf("seed tmp dir: %v", err)
	}

	if err := s.save(); err != nil {
		t.Fatalf("save() should recover via retry, got error: %v", err)
	}

	// The real store file must now exist with valid JSON content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("store file missing after save: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("store file is empty after save")
	}
	// The staging file must have been renamed away.
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("tmp staging path still present after save: %v", err)
	}
}

// TestStoreSave_SucceedsNormally guards the common (no-failure) path: a single
// WriteFile + rename with no retry needed.
func TestStoreSave_SucceedsNormally(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worktrees.json")
	s := NewStore(path)
	s.children = []*WorktreeChild{{ParentSlug: "r", GroupName: "g", Path: "/w", Status: StatusActive}}
	if err := s.save(); err != nil {
		t.Fatalf("save(): %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file missing: %v", err)
	}
}
