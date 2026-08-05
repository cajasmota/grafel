package watchers

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWatchStartsPath_IsUnderRepoStateDir pins where the watcher's start
// history lives. It has to be somewhere BOTH the watcher (internal/cli) and the
// registration paths (internal/install) can name, which is why the path helper
// lives here rather than next to the detector.
func TestWatchStartsPath_IsUnderRepoStateDir(t *testing.T) {
	got := WatchStartsPath("/tmp/some/repo")
	want := filepath.Join("/tmp/some/repo", ".grafel", "watch-starts.json")
	if got != want {
		t.Fatalf("WatchStartsPath = %q, want %q", got, want)
	}
}

// TestResetWatchStarts_ClearsAndIsIdempotent pins #6179 F4-a's remedy.
//
// The crash-loop detector counts STARTS, and a launchctl bootstrap is a start.
// So registering a unit — `grafel install`, `group add`, the reconcile — must
// clear the history, or a watcher that has given up can never be brought back
// within the window: the very act of re-registering it counts against it.
func TestResetWatchStarts_ClearsAndIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	path := WatchStartsPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"starts":["2026-01-01T00:00:00Z"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ResetWatchStarts(repo); err != nil {
		t.Fatalf("ResetWatchStarts: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("start history still present after reset (stat err = %v)", err)
	}
	// Idempotent: registration paths call this unconditionally, including for
	// repos that never had a record.
	if err := ResetWatchStarts(repo); err != nil {
		t.Fatalf("ResetWatchStarts on a missing record must succeed, got %v", err)
	}
	if err := ResetWatchStarts(filepath.Join(repo, "does", "not", "exist")); err != nil {
		t.Fatalf("ResetWatchStarts on a missing repo must succeed, got %v", err)
	}
}
