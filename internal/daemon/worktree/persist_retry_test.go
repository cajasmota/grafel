package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreSave_RetriesTransientTmpFailure verifies the persist path recovers
// from a transient atomic-write failure by retrying once (#5675), rather than
// dropping the reconcile.
//
// The transient failure is injected through the writeStoreFile seam: the first
// call fails, the second delegates to the real atomicfile.WriteFile. Before
// #6018 the test seeded `path+".tmp"` as a directory to make the first
// os.WriteFile fail — that only worked because the staging name was
// deterministic, which is the bug #6018 removed.
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

	real := writeStoreFile
	t.Cleanup(func() { writeStoreFile = real })
	calls := 0
	writeStoreFile = func(p string, b []byte, perm os.FileMode) error {
		calls++
		if calls == 1 {
			return errors.New("transient I/O blip")
		}
		return real(p, b, perm)
	}

	if err := s.save(); err != nil {
		t.Fatalf("save() should recover via retry, got error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("writeStoreFile called %d times, want 2 (one failure + one retry)", calls)
	}

	// The real store file must now exist with valid JSON content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("store file missing after save: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("store file is empty after save")
	}
	// No staging file may survive in the destination directory.
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != filepath.Base(path) {
			t.Errorf("leftover staging entry after save: %s", e.Name())
		}
	}
}

// TestStoreSave_ReturnsErrorWhenBothAttemptsFail pins that the retry is only
// ONE retry: a persistent failure still surfaces as an error rather than
// looping or being swallowed.
func TestStoreSave_ReturnsErrorWhenBothAttemptsFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worktrees.json")
	s := NewStore(path)
	s.children = []*WorktreeChild{{ParentSlug: "r", GroupName: "g", Path: "/w", Status: StatusActive}}

	real := writeStoreFile
	t.Cleanup(func() { writeStoreFile = real })
	calls := 0
	writeStoreFile = func(string, []byte, os.FileMode) error {
		calls++
		return errors.New("persistent failure")
	}

	if err := s.save(); err == nil {
		t.Fatal("save() with a persistent write failure: want error, got nil")
	}
	if calls != 2 {
		t.Fatalf("writeStoreFile called %d times, want exactly 2", calls)
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
