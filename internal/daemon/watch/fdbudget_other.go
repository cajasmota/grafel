//go:build !darwin

package watch

// fdDescriptorPerFile records that on this platform a watched directory does
// NOT cost an OS file descriptor per file inside it. Linux inotify spends one
// watch descriptor per directory out of the /proc/sys/fs/inotify/max_user_watches
// budget — a different resource with different arithmetic — and the Windows
// ReadDirectoryChangesW backend holds one handle per watched tree. Neither is
// the macOS failure, so grafel does not impose the macOS ceiling here.
const fdDescriptorPerFile = false

// defaultCostModel returns the per-watch arithmetic: dirs only.
func defaultCostModel() fdCostModel { return inotifyCostModel }

// defaultFDBudgetLimit reports 0 — descriptor accounting is disabled — unless
// an operator explicitly opts in via GRAFEL_WATCH_FD_BUDGET. The pre-existing
// walk.WatchDirCap directory ceiling is the relevant bound on these platforms.
func defaultFDBudgetLimit() int {
	if n, ok := envFDBudget(); ok {
		return n
	}
	return 0
}
