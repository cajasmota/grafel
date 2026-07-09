package daemon

import "testing"

// TestWorktreeActivateConcurrency verifies the OnActivate fan-out bound
// resolver (#5675): env override wins when strictly positive, otherwise the
// default applies. The bound caps how many worktree working-tree fsnotify
// subscriptions may open concurrently so a burst cannot exhaust fds.
func TestWorktreeActivateConcurrency(t *testing.T) {
	if got := worktreeActivateConcurrency(); got != defaultActivateConcurrency {
		t.Errorf("default = %d, want %d", got, defaultActivateConcurrency)
	}

	t.Setenv("GRAFEL_WORKTREE_ACTIVATE_CONCURRENCY", "3")
	if got := worktreeActivateConcurrency(); got != 3 {
		t.Errorf("override = %d, want 3", got)
	}

	// Non-positive / garbage falls back to the default.
	for _, bad := range []string{"0", "-1", "abc", ""} {
		t.Setenv("GRAFEL_WORKTREE_ACTIVATE_CONCURRENCY", bad)
		if got := worktreeActivateConcurrency(); got != defaultActivateConcurrency {
			t.Errorf("value %q: got %d, want default %d", bad, got, defaultActivateConcurrency)
		}
	}
}
