package main

import "testing"

// TestShouldSkipDirForStale_CIDirs_6946 pins the THIRD hand-maintained copy of
// the watcher skip list (shouldSkipDirForStale, whose doc comment says "keep in
// sync with internal/daemon/watch.ShouldSkipDir"). A copy that drifts here
// makes the daemon's stale-detection walk blind to a change under .github, so
// a workflow edit never marks the graph dirty even though the walker indexes
// it. See TestShouldSkipDir_CIDirsMatchWalker_6946 for the second copy.
func TestShouldSkipDirForStale_CIDirs_6946(t *testing.T) {
	for _, base := range []string{".github", ".gitlab", ".circleci"} {
		if shouldSkipDirForStale(base) {
			t.Errorf("shouldSkipDirForStale(%q) = true, want false (#6946) — the walker indexes "+
				"this directory, so stale detection must see writes into it", base)
		}
	}
	for _, base := range []string{".git", "node_modules", ".idea"} {
		if !shouldSkipDirForStale(base) {
			t.Errorf("shouldSkipDirForStale(%q) = false, want true — the #6946 un-skip reached "+
				"further than the three CI dirs", base)
		}
	}
}
