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
	"github.com/cajasmota/grafel/internal/daemon/watchscan"
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

	// ManagedRepo, when non-nil, reports whether a repo path is one the daemon
	// manages. It gates the foreign/orphan-watcher sweep (#5632): only watchers
	// for managed repos are ever reaped. nil disables that sweep — the
	// watchreg-based sweep above still runs.
	ManagedRepo func(repoPath string) bool

	// SelfExe returns the daemon's own executable path (os.Executable() in
	// production). The foreign-watcher sweep (#5632) reaps `grafel watch`
	// processes whose executable differs from this. nil → os.Executable.
	SelfExe func() (string, error)

	// ListWatchProcs enumerates live `grafel watch <repo>` processes for the
	// foreign-watcher sweep (#5632). nil → process.ListWatchProcesses. A lister
	// error (or an unsupported platform) makes the sweep a best-effort no-op.
	ListWatchProcs func() ([]process.WatchProc, error)

	// KillWatchProc terminates the foreign/duplicate watcher with the given pid
	// (#5632). nil → sigtermPID (a graceful SIGTERM, matching the watchreg
	// sweep). Injectable for tests so the sweep can be exercised without
	// touching real processes.
	KillWatchProc func(pid int) error

	// LiveDaemonPID returns the PID of the currently-live daemon/serve process,
	// used to detect orphaned watchers (their OwnerDaemonPID is stamped from
	// that same pidfile — see internal/cli/watch.go's liveDaemonPID). nil →
	// the orphan-kill branch is skipped entirely (fail-closed): the reaper
	// that owns this sweep does not necessarily run in the daemon/serve
	// process (in split mode, ADR-0024, it runs in the ENGINE process), so
	// os.Getpid() here is never a safe stand-in — comparing against it would
	// misclassify every live watcher as orphaned and kill it (#5933). Dead-PID
	// reaping is unaffected by this field either way.
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
	// ForeignWatchersReaped is the number of live `grafel watch` processes
	// reaped this sweep because their executable differed from the daemon's own
	// (version skew / orphan) or they duplicated a managed repo's watcher
	// (#5632). Independent of the watchreg-based count above.
	ForeignWatchersReaped int
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
	// #5632: reap live `grafel watch` processes for MANAGED repos whose
	// executable differs from the daemon's own (version skew / init-reparented
	// orphans the watchreg sweep misses), and collapse duplicate watchers to one
	// per repo. Best-effort: an unsupported platform / enumeration error is a
	// no-op. Independent of the watchreg sweep above.
	res.ForeignWatchersReaped = r.sweepForeignWatchers()
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
	// #5933: an unset LiveDaemonPID must fail CLOSED, not default to
	// os.Getpid(). In split mode (ADR-0024) this sweep runs in the ENGINE
	// process, which is never the daemon/serve process that stamps watcher
	// entries — defaulting to os.Getpid() here made every live watcher look
	// orphaned and killed it every cycle. Leaving LiveDaemonPID nil disables
	// the orphan-kill branch entirely (watchreg.Sweep treats live==0 as "no
	// ownership check"); dead-PID reaping is unaffected.
	//
	// Resolve (and pin) it once up front so a single sweep is internally
	// consistent, and so a resolved-but-empty pidfile (live == 0 — the
	// daemon/serve process is still starting, or its pidfile was just removed
	// by shutdown) is visible to an operator instead of silently degrading
	// the sweep with no signal (#5933).
	liveDaemonPID := r.cfg.LiveDaemonPID
	if liveDaemonPID != nil {
		live := liveDaemonPID()
		if live == 0 {
			r.logger.Debug("reaper: LiveDaemonPID resolved to 0 this sweep — orphan-kill branch skipped (daemon/serve pidfile absent: startup race or shutdown truncation?)")
		}
		liveDaemonPID = func() int { return live }
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

// sweepForeignWatchers reaps live standalone `grafel watch <repo>` processes
// for repos the daemon MANAGES whose executable differs from the daemon's own
// (#5632) — the stale-`$GOPATH/bin`-version watcher and the init-reparented
// orphan that the watchreg sweep cannot see (it only knows self-registered
// PIDs, and only flags owner-PID mismatches, never executable skew). It also
// collapses duplicate watchers down to one per managed repo. Returns the number
// of processes reaped.
//
// Strictly scoped + fail-safe:
//   - only processes targeting a MANAGED repo are ever considered;
//   - the decision (watchscan.Compute) is pure and unit-tested;
//   - enumeration is best-effort — a lister error or an unsupported platform
//     yields an empty plan and the daemon is undisturbed;
//   - a nil ManagedRepo disables the sweep entirely.
func (r *Reaper) sweepForeignWatchers() int {
	if r.cfg.ManagedRepo == nil {
		return 0
	}
	list := r.cfg.ListWatchProcs
	if list == nil {
		list = process.ListWatchProcesses
	}
	selfExeFn := r.cfg.SelfExe
	if selfExeFn == nil {
		selfExeFn = os.Executable
	}
	selfExe, _ := selfExeFn() // empty self-exe → watchscan never declares a mismatch.

	kill := r.cfg.KillWatchProc
	if kill == nil {
		kill = sigtermPID
	}

	plan := watchscan.Compute(watchscan.Deps{
		SelfExe: selfExe,
		Managed: r.cfg.ManagedRepo,
		List: func() ([]watchscan.Proc, error) {
			procs, err := list()
			if err != nil {
				return nil, err
			}
			out := make([]watchscan.Proc, 0, len(procs))
			for _, p := range procs {
				out = append(out, watchscan.Proc{PID: p.PID, Exe: p.Exe, Repo: p.Repo})
			}
			return out, nil
		},
	})

	pids := plan.PIDs()
	reaped := 0
	for _, pid := range pids {
		if pid == os.Getpid() {
			continue // never signal the daemon itself.
		}
		if err := kill(pid); err != nil {
			r.logger.Warn("reaper: foreign-watcher SIGTERM failed (non-fatal)", "pid", pid, "err", err)
			continue
		}
		reaped++
	}
	if reaped > 0 {
		r.logger.Info("reaper: reaped foreign/duplicate grafel-watch processes",
			"reaped", reaped, "foreign", len(plan.Foreign), "duplicate", len(plan.Duplicate))
	}
	return reaped
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
