// reaper.go — vanished-repo store GC (issue #3680).
//
// # Problem
//
// grafel dogfoods itself, so the daemon's watcher catches every
// `git worktree add` and (pre-#3680) cold-indexes the worktree as a SEPARATE
// repo with its OWN ~100MB full graph store under hash(worktreePath). When the
// rewrite agent later deletes those worktrees, the directories vanish from disk
// but the daemon keeps tracking them: the tier Manager still holds their slots
// (counting toward memory pressure and attempting cold-wakes) and the on-disk
// store dir is never deleted. The live daemon's `~/.grafel/store/` grew to
// 7.2GB dominated by ~25 such orphaned worktree stores, with the tier Manager
// logging `stat repo: ... no such file or directory`.
//
// # Fix
//
// The Reaper periodically reconciles the tracked repo set against the
// filesystem. For every tracked repo path whose directory no longer exists it:
//
//  1. deletes the on-disk store directory (repoBaseDir), reclaiming its bytes,
//  2. forgets the repo's tier slots (decrementing the in-memory accounting),
//  3. lets the caller drop any other registry/worktree-store tracking via the
//     Untrack callback.
//
// An EXISTING repo is never reaped. The reaper is conservative: a path that
// cannot be stat'd for any reason OTHER than "does not exist" (e.g. a transient
// permission error) is left untouched so a flaky filesystem can never cause the
// daemon to nuke a live repo's graph.
package daemon

import (
	"errors"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/watchreg"
	"github.com/cajasmota/grafel/internal/process"
)

// TierForgetter is the narrow slice of tier.Manager the reaper needs: drop all
// slots for a repo path and report how many were dropped. Implemented by
// *tier.Manager.Forget.
type TierForgetter interface {
	Forget(repoPath string) int
}

// ReaperConfig wires the vanished-repo reaper. All hooks are optional except
// TrackedRepos; a nil hook is simply skipped.
type ReaperConfig struct {
	// TrackedRepos returns the current set of absolute repo paths the daemon
	// tracks (registered repos + active worktree children). Called on every
	// sweep so newly-tracked repos are covered without restarting the reaper.
	// Required; a nil value makes Sweep a no-op.
	TrackedRepos func() []string

	// StoreDirForRepo returns the absolute on-disk store directory for a repo
	// path (the top-level slot, e.g. repoBaseDir). The reaper RemoveAll's this
	// directory when the repo has vanished. When nil, store deletion is skipped
	// (slots are still forgotten).
	StoreDirForRepo func(repoPath string) string

	// Tier, when non-nil, has Forget(repoPath) called for every vanished repo
	// so its slots leave the in-memory accounting.
	Tier TierForgetter

	// Untrack, when non-nil, is called once per vanished repo so the caller can
	// drop the path from any other tracking surface (worktree store, head
	// poller, fsnotify subscription). Invoked after the store is removed.
	Untrack func(repoPath string)

	// WatchRegistry, when non-nil, is the daemon-owned `grafel watch` PID
	// registry (#5142). On every sweep the reaper reconciles it: entries whose
	// process is dead are dropped, and live-but-orphaned watchers (owned by a
	// previous daemon generation) are SIGTERM'd and dropped. nil disables
	// watcher reaping (e.g. modes without standalone watchers).
	WatchRegistry *watchreg.Registry

	// LiveDaemonPID returns the PID of the currently-live daemon, used to detect
	// orphaned watchers. nil → os.Getpid (the running daemon owns the sweep).
	LiveDaemonPID func() int

	// DeadRefs, when non-nil, is the dead-ref / dead-worktree store sweep
	// (#5236). It reclaims store dirs + resident graphs for refs that git no
	// longer knows about, within still-present repos. The Reaper drives it on
	// the same cadence as the vanished-repo GC. nil disables it.
	DeadRefs *DeadRefSweeper

	// OrphanRoots, when non-nil, is the orphan top-level store-root sweep
	// (#5263). It reaps whole `<store>/<slug>-<hash>/` roots that map to a
	// vanished source path and to no live group/primary — the gap left by the
	// vanished-repo GC (which only covers CURRENTLY-tracked repos) and the
	// dead-ref GC (which only covers refs within still-tracked repos). Driven
	// on the shared cadence. nil disables it. Conservative + fail-closed: an
	// undeterminable root is always KEPT.
	OrphanRoots *OrphanRootSweeper

	// Interval between sweeps. Default (zero value): 5 minutes.
	Interval time.Duration

	// Logger for sweep diagnostics. nil → a default stderr logger.
	Logger *slog.Logger
}

// ReapResult summarises one Sweep.
type ReapResult struct {
	// Vanished is the number of tracked repos found missing on disk.
	Vanished int
	// StoresRemoved is the number of on-disk store directories deleted.
	StoresRemoved int
	// SlotsForgotten is the total tier slots dropped from accounting.
	SlotsForgotten int
	// FreedBytes is the total bytes reclaimed from deleted store dirs.
	FreedBytes int64
	// WatchersReaped is the number of stale/orphaned `grafel watch` PID
	// registry entries reaped this sweep (#5142).
	WatchersReaped int
	// DeadRefs summarises the dead-ref / dead-worktree sub-sweep (#5236).
	DeadRefs DeadRefResult
	// OrphanRoots summarises the orphan top-level store-root sub-sweep (#5263).
	OrphanRoots OrphanRootResult
}

// Reaper periodically GCs stores for repos that no longer exist on disk.
type Reaper struct {
	cfg    ReaperConfig
	logger *slog.Logger
}

// NewReaper constructs a Reaper. Call Start to run it in the background, or
// call Sweep directly (tests / one-shot).
func NewReaper(cfg ReaperConfig) *Reaper {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "reaper")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	return &Reaper{cfg: cfg, logger: logger}
}

// Start runs the reaper loop until stopCh is closed. It performs one immediate
// sweep after a short startup delay, then sweeps on the configured interval.
func (r *Reaper) Start(stopCh <-chan struct{}) {
	go func() {
		// Brief startup delay so boot-time indexing settles first.
		select {
		case <-stopCh:
			return
		case <-time.After(30 * time.Second):
		}
		r.Sweep()
		t := time.NewTicker(r.cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-t.C:
				r.Sweep()
			}
		}
	}()
}

// Sweep runs one reconciliation pass synchronously and returns what it reaped.
// Safe to call directly from tests.
func (r *Reaper) Sweep() ReapResult {
	var res ReapResult
	// #5142: reap stale/orphaned `grafel watch` PIDs. Independent of the
	// vanished-repo GC below, so it runs even in configs without TrackedRepos.
	res.WatchersReaped = r.sweepWatchers()
	// #5236: dead-ref / dead-worktree sweep. Independent of the vanished-repo
	// GC below; runs on the same cadence so a deleted branch's store + resident
	// graph are reclaimed without a separate scheduler.
	if r.cfg.DeadRefs != nil {
		res.DeadRefs = r.cfg.DeadRefs.Sweep()
	}
	// #5263: orphan top-level store-root sweep. Independent of the
	// vanished-repo GC below; runs on the shared cadence so a root tracked by
	// nothing (repo de-registered / worktree deleted) is reclaimed wholesale.
	// Conservative + fail-closed: undeterminable roots are always kept.
	if r.cfg.OrphanRoots != nil {
		res.OrphanRoots = r.cfg.OrphanRoots.Sweep()
	}
	if r.cfg.TrackedRepos == nil {
		return res
	}
	for _, repo := range r.cfg.TrackedRepos() {
		if repo == "" {
			continue
		}
		if repoExists(repo) {
			continue // live repo — never reaped.
		}
		res.Vanished++
		r.logger.Info("reaper: tracked repo vanished from disk — GCing store", "repo", repo)

		// 1. Delete the on-disk store.
		if r.cfg.StoreDirForRepo != nil {
			storeDir := r.cfg.StoreDirForRepo(repo)
			if storeDir != "" {
				if sz, err := r.removeStore(storeDir); err != nil {
					r.logger.Warn("reaper: store removal failed (non-fatal)", "repo", repo, "store", storeDir, "err", err)
				} else if sz >= 0 {
					res.StoresRemoved++
					res.FreedBytes += sz
					r.logger.Info("reaper: store removed", "repo", repo, "store", storeDir, "freed_bytes", sz)
				}
			}
		}

		// 2. Forget tier slots (decrement in-memory accounting).
		if r.cfg.Tier != nil {
			res.SlotsForgotten += r.cfg.Tier.Forget(repo)
		}

		// 3. Drop any other tracking.
		if r.cfg.Untrack != nil {
			r.cfg.Untrack(repo)
		}
	}
	if res.Vanished > 0 {
		r.logger.Info("reaper: sweep complete",
			"vanished", res.Vanished,
			"stores_removed", res.StoresRemoved,
			"slots_forgotten", res.SlotsForgotten,
			"freed_bytes", res.FreedBytes)
	}
	return res
}

// sweepWatchers reconciles the daemon-owned `grafel watch` PID registry
// (#5142): it drops entries whose process is dead and SIGTERMs + drops
// live-but-orphaned watchers (owned by a previous daemon generation). Returns
// the number of entries reaped. A nil WatchRegistry disables the sweep.
//
// Liveness uses the same signal-0 probe as the daemon pidfile; the kill is a
// SIGTERM (graceful — the watcher's signal handler exits cleanly). Both are
// injected as functions only in tests; production uses the real syscalls.
func (r *Reaper) sweepWatchers() int {
	if r.cfg.WatchRegistry == nil {
		return 0
	}
	liveDaemonPID := r.cfg.LiveDaemonPID
	if liveDaemonPID == nil {
		liveDaemonPID = os.Getpid
	}
	res, err := r.cfg.WatchRegistry.Sweep(watchreg.SweepDeps{
		Alive:         pidAliveProbe,
		Kill:          sigtermPID,
		LiveDaemonPID: liveDaemonPID,
	})
	if err != nil {
		r.logger.Warn("reaper: watcher PID registry sweep failed (non-fatal)", "err", err)
		return 0
	}
	if res.Reaped() > 0 {
		r.logger.Info("reaper: reaped stale grafel-watch PIDs",
			"dead", res.Dead, "orphaned", res.Orphaned, "kill_errors", len(res.KillErrors))
	}
	return res.Reaped()
}

// pidAliveProbe reports whether pid names a live process (signal-0 existence
// probe; portable on darwin/linux).
func pidAliveProbe(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// sigtermPID sends SIGTERM to pid via the process package's portable Kill.
func sigtermPID(pid int) error {
	return process.Kill(pid)
}

// removeStore deletes storeDir and returns the bytes it freed. A non-existent
// store dir is not an error (returns 0 freed). Returns -1 freed when the dir
// did not exist so the caller can distinguish "nothing to remove" from "removed
// 0-byte dir".
func (r *Reaper) removeStore(storeDir string) (int64, error) {
	sz, err := dirSizeHygiene(storeDir)
	if err != nil {
		// Most likely the store dir never existed — nothing to free.
		if errors.Is(err, os.ErrNotExist) {
			return -1, nil
		}
		// Other walk errors: still attempt removal but report 0 freed.
		sz = 0
	}
	if rmErr := os.RemoveAll(storeDir); rmErr != nil {
		return 0, rmErr
	}
	return sz, nil
}

// repoExists reports whether repoPath is an existing directory on disk. It
// returns true on any stat error OTHER than "not exist" so a transient
// permission/IO error never causes a live repo to be reaped (fail-safe).
func repoExists(repoPath string) bool {
	fi, err := os.Stat(repoPath)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	return fi.IsDir()
}
