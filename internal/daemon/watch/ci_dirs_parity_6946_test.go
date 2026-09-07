package watch

import "testing"

// TestShouldSkipDir_CIDirsMatchWalker_6946 pins the walker/watcher parity that
// #6946 relies on and that nothing else grades.
//
// SkipDirs above is a genuine SECOND hand-maintained copy of the skip names
// (it duplicates .git, .claude, node_modules, .idea, ...), consulted BEFORE
// the delegation to walk.IsHardcodedSkip. Adding the three CI dirs to it
// reproduces #6934 exactly — the walker indexes the directory and the watcher
// never re-indexes it on change — and stays green across walk, watch, sched
// and engine. The property held only by luck until this test.
//
// A third copy lives in cmd/grafel/daemon_tier.go (shouldSkipDirForStale,
// "keep in sync with internal/daemon/watch.ShouldSkipDir"); it is covered by
// TestShouldSkipDirForStale_CIDirs_6946 in that package.
func TestShouldSkipDir_CIDirsMatchWalker_6946(t *testing.T) {
	for _, base := range []string{".github", ".gitlab", ".circleci"} {
		if ShouldSkipDir(base) {
			t.Errorf("ShouldSkipDir(%q) = true, want false (#6946) — the walker indexes this "+
				"directory, so a watcher that drops its events leaves it stale forever", base)
		}
		if ShouldSkipPath(base + "/workflows/ci.yml") {
			t.Errorf("ShouldSkipPath(%q/workflows/ci.yml) = true, want false (#6946)", base)
		}
	}
	// Negative controls: the tool-agent dirs that stayed on the list must
	// still be dropped, or this test would pass against a watcher that
	// skips nothing at all.
	for _, base := range []string{".claude", "node_modules", ".idea"} {
		if !ShouldSkipDir(base) {
			t.Errorf("ShouldSkipDir(%q) = false, want true — the #6946 un-skip reached further "+
				"than the three CI dirs", base)
		}
	}
}
