package worktree

import "time"

// ParseWorktreeListForTest exposes the package-internal parseWorktreeList
// function for white-box unit tests in the _test package.
func ParseWorktreeListForTest(s string) []RawWorktree {
	return parseWorktreeList(s)
}

// IntervalForTest exposes the resolved reconciliation interval for tests.
func (w *Watcher) IntervalForTest() time.Duration {
	return w.interval
}
