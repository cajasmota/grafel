package watch

// overflow.go — recovery from an fsnotify queue overflow (#6921).
//
// fsnotify reports ErrEventOverflow on its Errors channel when the backend's
// queue could not hold everything the kernel produced:
//
//	inotify — IN_Q_OVERFLOW, raised once the per-instance queue exceeds
//	          fs.inotify.max_queued_events (kernel default 16384)
//	          (backend_inotify.go:397-400).
//	windows — the ReadDirectoryChangesW buffer was too small to hold the
//	          batch (backend_windows.go:580-583).
//	kqueue, fen — never raised (fsnotify.go:262-270).
//
// It means an UNKNOWN set of events was dropped. fsnotify is edge-triggered, so
// nothing redelivers them: without a rescan those files stay stale until some
// unrelated event (a branch switch, a manual reindex, a daemon restart) happens
// to re-cover them, and the watcher goes on reporting healthy the whole time.
// That is the same silent outcome as the descriptor-exhaustion case in
// fdbudget.go, arriving by a different road.
//
// SCOPE — why the rescan is every repo and not one. The queue that overflows
// belongs to the fsnotify INSTANCE. grafel builds exactly one Watcher per
// daemon process (internal/daemon/engineplane.go:203) and a Watcher owns one
// *fsnotify.Watcher (w.fs) that every subscribed repo shares, so one overflow
// invalidates every one of them. The event that overflowed the queue carries no
// path — IN_Q_OVERFLOW has none — so there is no narrower scope available even
// in principle.
//
// The recovery therefore reuses the hand-off restartBackend already uses for
// the identical "we lost events" condition: ForceRescan, i.e. the
// EventSink(repoPath, bulk=true) contract, which asks the scheduler for a full
// repo reindex rather than a file-level diff. No second reindex path exists.

import (
	"sync/atomic"
	"time"
)

// overflowRescanCooldown bounds the recovery's fan-out: at most one full rescan
// of every subscribed repo per window, however many overflows arrive.
//
// An overflow storm must not become a reindex storm. inotify sets
// IN_Q_OVERFLOW on the queue, not on one event, so a single burst — an
// `npm install`, a codegen run, an agent rewriting a worktree — commonly
// produces several in a row; one rescan each would reindex every repo in the
// daemon several times over and generate exactly the churn that overflowed the
// queue. Coalescing is safe because the rescans are IDENTICAL: a rescan covers
// every path a later overflow in the same burst could have dropped, so the
// second is not merely expensive, it is redundant.
//
// This is the same bound #5675's activation semaphore and quarantine.go put on
// their fan-out — a ceiling on concurrent recovery work, not a retry budget.
// A coalesced overflow is still COUNTED and still reported (OverflowStats), so
// the user sees the burst even though the daemon acts on it once.
const overflowRescanCooldown = 30 * time.Second

// handleOverflow is the recovery. It runs on the loop goroutine, so it must not
// block: that goroutine is the sole receiver on the backend's Events, and
// fsnotify's Windows backend cannot complete an Add or a Remove while nobody is
// draining (watcher.go:172-185). The sink fan-out is therefore dispatched to
// its own goroutine, exactly as RescanRepo does.
func (w *Watcher) handleOverflow() {
	atomic.AddUint64(&w.overflows, 1)
	now := w.clk.Now()

	w.mu.Lock()
	w.lastOverflowAt = now
	if !w.lastOverflowRescanAt.IsZero() && now.Sub(w.lastOverflowRescanAt) < overflowRescanCooldown {
		w.mu.Unlock()
		atomic.AddUint64(&w.overflowsCoalesced, 1)
		w.logger.Warn("watcher: fsnotify queue overflow — events were DROPPED; "+
			"a full rescan is already in flight for this burst, coalescing",
			"overflows", atomic.LoadUint64(&w.overflows))
		return
	}
	w.lastOverflowRescanAt = now
	repos := len(w.repos)
	w.mu.Unlock()
	atomic.AddUint64(&w.overflowRescans, 1)

	w.logger.Warn("watcher: fsnotify queue overflow — an unknown set of file events was DROPPED "+
		"and will never be redelivered; forcing a full reindex of every subscribed repo",
		"repos", repos, "overflows", atomic.LoadUint64(&w.overflows))
	go w.ForceRescan()
}

// OverflowStats reports the queue-overflow ledger for `grafel status` and
// /diagnostics (#6921): how many overflows the backend reported, how many full
// rescans they triggered, how many were coalesced into a rescan already in
// flight, and when the last one arrived.
//
// It is surfaced rather than merely logged because the failure being reported
// is silent staleness — a user staring at a graph that does not match their
// tree has no way to tell an overflow from a healthy watcher, and a daemon log
// nobody reads is not an answer. A non-zero count means "some window of edits
// was dropped and re-covered by a rescan"; the last-overflow time is what
// makes it correlatable with what the user was doing.
func (w *Watcher) OverflowStats() (overflows, rescans, coalesced uint64, last time.Time) {
	w.mu.Lock()
	last = w.lastOverflowAt
	w.mu.Unlock()
	return atomic.LoadUint64(&w.overflows),
		atomic.LoadUint64(&w.overflowRescans),
		atomic.LoadUint64(&w.overflowsCoalesced),
		last
}
