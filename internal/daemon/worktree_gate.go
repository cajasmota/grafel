// worktree_gate.go — daemon wiring for the #3680 worktree-churn fixes.
//
// Two small adapters connect the daemon's existing tracking surfaces to the
// linked-worktree classifier (internal/daemon/worktree) and the vanished-repo
// reaper (reaper.go):
//
//   - makeWorktreeEnqueueGate builds the scheduler SkipEnqueue predicate that
//     drops cold-index enqueues for linked worktrees of an already-indexed
//     primary repo.
//   - makeReaperTrackedRepos builds the reaper's tracked-repo provider from the
//     registered repos plus any active worktree children.
package daemon

import (
	"github.com/cajasmota/grafel/internal/daemon/worktree"
)

// makeWorktreeEnqueueGate returns a sched.SkipEnqueueFn-compatible predicate
// that returns true when repoPath is a linked git worktree whose primary
// checkout is one of the currently-registered repos (reposToWatch). Such a
// path must NOT be cold-indexed as an independent root repo (#3680).
//
// reposToWatch may be nil (no registered repos resolvable yet) — the gate then
// never fires, preserving legacy behaviour. The registered set is re-read on
// every call so repos registered after boot are covered.
func makeWorktreeEnqueueGate(reposToWatch func() []string) func(repoPath string) bool {
	if reposToWatch == nil {
		return nil
	}
	return func(repoPath string) bool {
		// Fast structural reject: only `.git`-is-a-file worktrees can be gated.
		// ClassifyRoot is a single Stat for the common standalone case, so this
		// stays cheap on the hot path.
		c := worktree.ClassifyRoot(repoPath)
		if c.Kind != worktree.RootKindLinkedWorktree {
			return false
		}
		return worktree.IsLinkedWorktreeOf(repoPath, reposToWatch())
	}
}

// makeReaperTrackedRepos returns the reaper's TrackedRepos provider: the union
// of the registered repos (reposToWatch) and the active worktree children, so
// the reaper GCs stores for any of them that vanish from disk. Either input may
// be nil. Duplicate paths are de-duplicated.
func makeReaperTrackedRepos(reposToWatch func() []string, wtStore *worktree.Store) func() []string {
	return func() []string {
		seen := map[string]bool{}
		var out []string
		add := func(p string) {
			if p == "" || seen[p] {
				return
			}
			seen[p] = true
			out = append(out, p)
		}
		if reposToWatch != nil {
			for _, r := range reposToWatch() {
				add(r)
			}
		}
		if wtStore != nil {
			for _, c := range wtStore.Active() {
				add(c.Path)
			}
		}
		return out
	}
}

// makeKnownSourcePaths returns the orphan-root sweep's KnownSourcePaths
// provider (#5263): the union of the registered repos (reposToWatch) and ALL
// worktree children — active AND expired/vanished. Unlike makeReaperTrackedRepos
// it deliberately includes vanished worktrees so their store roots can still be
// ATTRIBUTED to a (now-gone) source path and reaped; a root attributable to no
// known path is left undeterminable (fail-closed KEEP) by the sweeper. Either
// input may be nil. Duplicate paths are de-duplicated.
func makeKnownSourcePaths(reposToWatch func() []string, wtStore *worktree.Store) func() []string {
	return func() []string {
		seen := map[string]bool{}
		var out []string
		add := func(p string) {
			if p == "" || seen[p] {
				return
			}
			seen[p] = true
			out = append(out, p)
		}
		if reposToWatch != nil {
			for _, r := range reposToWatch() {
				add(r)
			}
		}
		if wtStore != nil {
			for _, c := range wtStore.All() {
				add(c.Path)
			}
		}
		return out
	}
}
