package main

// daemon_tier.go wires the tiered hibernation state machine (PH2 of epic
// #2087 / issue #2090, extended by PH3 #2091, PH2a #2096, PH6 #2094,
// S1 #2151, P0.3 #2141, M2 #2179), and the watcher pause/resume integration
// into the daemon process.
//
// Process-global daemonTierMgr tracks HOT/WARM/COLD/EXPIRED state for every
// indexed (repoPath, ref) pair.  Integrations:
//
//   - tierAfterIndex: called after every successful index pass; registers the
//     slot as HOT (or re-activates it) and detects the default branch.
//
//   - S1 (#2151) lazy hydration: registerKnownGroupsCold walks the registry
//     at daemon startup and calls RegisterCold for every known (repoPath, ref)
//     that has a graph.fb on disk. This sets each slot to COLD without opening
//     graph.fb, so idle RSS at startup with 5 registered groups is <100 MB.
//     The first MCP query on a cold group triggers Touch → cold-wake.
//
//   - M2 (#2179) lazy fsnotify subscription: the daemon boots with ZERO
//     fsnotify subscriptions. onWatcherReady does NOT eagerly subscribe any
//     repos. The first MCP query for a group triggers SubscribeGroupWatcher
//     which calls watch.DefaultManager.SubscribeGroup → watcher.AddRepo.
//     The tier WARM→COLD path already calls wh.Pause → watcher.RemoveRepo,
//     making the idle-unsubscribe half automatic. Re-query after idle pause
//     calls wh.Resume → watcher.AddRepo (lazy re-subscribe).
//
//   - MCP graph-cache AccessHook: wired in startDaemonTierManager; every
//     GetForRepoRef call updates lastAccessedAt via tierTouchRepoRef so
//     actively-queried graphs don't get prematurely evicted.
//
//   - Eviction (WARM→COLD): daemonMCPCache.Invalidate releases the mmap'd
//     fbreader.Reader; the dashboard cache ages out via its own TTL.
//     PH2a: watcher subscription is also paused for the repo.
//
//   - Cold wake (COLD→HOT): the reload callback re-mmap's graph.fb by
//     calling daemonMCPCache.Get; the dashboard cache reloads lazily on the
//     next HTTP request. PH2a/M2: watcher subscription is resumed before
//     reload (Resume lazily calls AddRepo if refCount was 0).
//
//   - Disk eviction (COLD→EXPIRED, PH6): tierDiskEvictCallback deletes the
//     refs/<ref>/ sub-directory for the expired slot and logs freed bytes.
//
//   - P0.3 (#2141) pressure-driven eviction: when heap usage exceeds
//     GRAFEL_HEAP_MAX_PCT% of system memory (default 60%), the scanner
//     immediately evicts the oldest HOT/WARM slots to COLD regardless of TTL.
//     Pinned-main slots are exempt.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/tier"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// daemonTierMgr is the process-wide tiered hibernation state machine.
// Nil before startDaemonTierManager is called.
var daemonTierMgr *tier.Manager

// daemonWatcherMgr is the PH2a watcher pause/resume manager.
// Non-nil only after the fsnotify watcher is ready (set via OnWatcherReady).
var daemonWatcherMgr *watch.DefaultManager

// daemonSchedulerEnqueue is the PH2a cold-wake stale-detection enqueue hook.
// Set by onWatcherReady when the scheduler is available. Calls
// sched.Scheduler.Enqueue under the hood.
var daemonSchedulerEnqueue func(repoPath string)

// startDaemonTierManager constructs and starts the tier manager. Must be
// called once from runDaemon before the daemon begins serving requests.
//
// S1 (#2151): after the manager is running, walks the registry and calls
// RegisterCold for every repo/ref pair that has a graph.fb on disk. This
// avoids eager-loading all graphs at startup — idle RSS with 5 registered
// groups should be <100 MB.
//
// P0.3 (#2141): injects the real system memory size into TTLConfig so
// the pressure-eviction threshold is computed against physical RAM.
func startDaemonTierManager(ctx context.Context, logger *slog.Logger) {
	ttl := tier.EnvTTLConfig()

	// P0.3: populate SystemMemoryBytes from the process package so the
	// pressure threshold is calibrated against actual physical RAM rather
	// than runtime.Sys (which under-counts on many systems).
	if sysMB := systemTotalMemoryMB(); sysMB > 0 {
		ttl.SystemMemoryBytes = uint64(sysMB) * 1024 * 1024
	}

	daemonTierMgr = tier.NewManager(ctx, ttl, tierEvictCallback, tierReloadCallback, tierDiskEvictCallback, logger)

	// Wire the MCP graph-cache access hook so every GetForRepoRef call
	// updates lastAccessedAt in the tier manager without extra call-sites.
	daemonMCPCache.SetAccessHook(func(repoPath, ref string) {
		_ = tierTouchRepoRef(repoPath, ref)
	})

	// S1 (#2151): lazy hydration — register all known groups as COLD so
	// the tier manager is aware of them without loading any graph into memory.
	registerKnownGroupsCold(logger)
}

// registerKnownGroupsCold walks every registered group and calls RegisterCold
// for each (repoPath, ref) pair that has a graph.fb on disk. This is the S1
// boot-time lazy-hydration path: the tier manager knows about each slot (so
// cold-wake and pressure-evict accounting are correct) but no graph.fb is
// opened until the first MCP query for that group.
//
// Refs are discovered by scanning the refs/ subdirectory inside the per-repo
// state directory. Any ref directory that contains a graph.fb is registered.
// If no refs/ dir exists, the _unknown sentinel is skipped (it would be
// refused by GetForRepoRef anyway).
func registerKnownGroupsCold(logger *slog.Logger) {
	if daemonTierMgr == nil {
		return
	}
	groups, err := registry.Groups()
	if err != nil {
		logger.Warn("tier: lazy-hydration: registry.Groups failed (skipping cold-register)", "err", err)
		return
	}

	var registered int
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			repoPath := r.Path
			// Walk the refs/ subdirectory to find every indexed ref.
			refsDir := filepath.Join(daemon.StateDirForRepo(repoPath), "refs")
			entries, err := os.ReadDir(refsDir)
			if err != nil {
				// No refs/ dir or unreadable — not an error; repo hasn't been
				// indexed yet or uses the legacy flat layout.
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				ref := e.Name()
				if ref == "_unknown" {
					continue // sentinel — skip per ErrUnknownRef semantics
				}
				// #5915 J1 FIX-3: segment-aware existence probe. os.Stat of the
				// flat CurrentGraphPath is absent for a segment-set (the pointer
				// names a graph.<gen>/ dir), so the old check would skip — never
				// cold-register, hence never warm — a segmented repo at boot.
				// Route existence through the descriptor: GraphAbsent (or a corrupt
				// manifest) → skip; single-file/legacy/segment-set → register.
				refStateDir := filepath.Join(refsDir, ref)
				if desc, dErr := graph.CurrentGraphDescriptor(refStateDir); dErr != nil || desc.Kind == graph.GraphAbsent {
					continue // no graph yet (gen file, gen dir, or legacy flat)
				}
				isPinned := tier.IsDefaultBranch(repoPath, ref)
				kind := tier.SlotKindBranchFeature
				if isPinned {
					kind = tier.SlotKindBranchMain
				}
				// Register as branch kind for now; worktree slots are
				// re-registered by tierAfterIndexWorktree on the first index pass.
				daemonTierMgr.RegisterCold(tier.SlotKey{RepoPath: repoPath, Ref: ref}, isPinned, kind)
				registered++
			}
		}
	}
	if registered > 0 {
		logger.Info("tier: S1 lazy-hydration: cold-registered slots from registry (no graph.fb opened)", "count", registered)
	}
}

// onWatcherReady is called by daemon.Run once the fsnotify watcher is up.
// It creates the DefaultManager and wires it into the tier state machine.
//
// M2 (#2179): unlike the PH2a boot policy, onWatcherReady does NOT eagerly
// register repos or call AddRepo. The daemon boots with ZERO fsnotify
// subscriptions. The first MCP query for a group calls SubscribeGroupWatcher
// which lazily subscribes that group's repos. This means the watcher manager
// starts with an empty slots map and a zero subscription count.
func onWatcherReady(w *watch.Watcher, logger *slog.Logger) {
	mgr := watch.NewDefaultManager(w, logger)
	daemonWatcherMgr = mgr

	// M2: do NOT register or subscribe any repos here. The watcher is created
	// idle. Repos are subscribed on-demand by SubscribeGroupWatcher (called on
	// first MCP query per group) or by the Resume path on COLD→HOT transitions.

	if daemonTierMgr != nil {
		daemonTierMgr.SetWatcherHook(mgr)
		logger.Info("tier: watcher pause/resume hook wired (M2 lazy-subscribe — 0 subscriptions at boot)")
	}

	// Q3 (#5618): wire the index-quarantine auto-recover hook into the MCP
	// server. The watcher owns the QuarantineTracker; the MCP query path calls
	// Recover when it resolves an entity, so a dir quarantined as index trash
	// that later proves real (its content is queried) is un-quarantined
	// immediately rather than waiting out the quiet-window self-heal. Best-effort:
	// if the MCP server is not yet initialised, the hook is simply not wired (the
	// quiet-window Sweep still recovers genuinely-idle dirs).
	if qt := w.Quarantine(); qt != nil {
		if srv, err := mcpServerInstance(); err == nil && srv != nil {
			srv.SetQuarantineRecoverer(qt)
			logger.Info("quarantine: auto-recover-on-query hook wired into MCP server (#5618)")
		}
	}
}

// SubscribeGroupWatcher lazily subscribes the fsnotify watcher for all repos
// in a named group. This is the M2 entry point called on the first MCP query
// that touches a group. It is idempotent: re-calling for an already-subscribed
// group is a no-op (refCounts prevent double-subscription).
//
// groupName is the registry group name (e.g. "myapp").
// repoPaths is the list of absolute repo paths in the group.
//
// Returns the total number of directories added to fsnotify.
func SubscribeGroupWatcher(groupName string, repoPaths []string) int {
	if daemonWatcherMgr == nil {
		return 0
	}
	n := daemonWatcherMgr.SubscribeGroup(groupName, repoPaths)
	if n > 0 {
		// Also register each repo/ref slot so Pause/Resume reference counts are
		// accurate. Use the sentinel ref "" so the accounting is per-repo.
		for _, rp := range repoPaths {
			daemonWatcherMgr.Register(rp, "")
		}
	}
	return n
}

// tierAfterIndex is called after every successful index pass to register
// (or re-activate) the slot as HOT. Detects default branch for isPinnedMain.
// PH3 (#2091): slots are now annotated with SlotKind so the tier manager can
// apply the correct TTL policy.  Worktree slots are registered separately via
// tierAfterIndexWorktree.
//
// M2 (#2179): after a fresh index we also ensure the repo's fsnotify
// subscription is active so future file-change events are captured. We call
// Resume rather than Register so that the lazy-subscribe logic in
// DefaultManager.Resume triggers watcher.AddRepo if the repo was not yet
// subscribed (first index ever) or was previously unsubscribed (idle eviction).
func tierAfterIndex(repoPath, ref string) {
	if daemonTierMgr == nil {
		return
	}
	isPinned := tier.IsDefaultBranch(repoPath, ref)
	kind := tier.SlotKindBranchFeature
	if isPinned {
		kind = tier.SlotKindBranchMain
	}
	daemonTierMgr.Register(tier.SlotKey{RepoPath: repoPath, Ref: ref}, isPinned, kind)

	// M2 (#2179): use Resume instead of Register so the watcher subscription is
	// established (or re-established) on the first index. Resume is idempotent
	// when the slot is already active.
	if daemonWatcherMgr != nil {
		daemonWatcherMgr.Resume(repoPath, ref)
	}
}

// tierAfterIndexWorktree is like tierAfterIndex but uses SlotKindWorktree
// so the tier manager applies the aggressive 30-min WARM→COLD window.
// Called after indexing a linked worktree (discovered by PH3).
func tierAfterIndexWorktree(repoPath, ref string) {
	if daemonTierMgr == nil {
		return
	}
	daemonTierMgr.Register(tier.SlotKey{RepoPath: repoPath, Ref: ref}, false, tier.SlotKindWorktree)

	// M2 (#2179): use Resume to lazily subscribe the watcher (same reasoning as
	// tierAfterIndex above).
	if daemonWatcherMgr != nil {
		daemonWatcherMgr.Resume(repoPath, ref)
	}
}

// tierTouchRepoRef records an access for (repoPath, ref). If the slot is
// COLD, this triggers an in-place reload (via tierReloadCallback) and
// transitions the slot back to HOT.
func tierTouchRepoRef(repoPath, ref string) error {
	if daemonTierMgr == nil {
		return nil
	}
	return daemonTierMgr.Touch(tier.SlotKey{RepoPath: repoPath, Ref: ref})
}

// deadRefTierForgetter adapts daemonTierMgr.ForgetRef to the
// daemon.RefForgetter interface used by the dead-ref sweeper (#5236). It is a
// zero-value struct so it can be wired before daemonTierMgr is constructed;
// the nil check happens at call time.
type deadRefTierForgetter struct{}

func (deadRefTierForgetter) ForgetRef(repoPath, ref string) bool {
	if daemonTierMgr == nil {
		return false
	}
	return daemonTierMgr.ForgetRef(repoPath, ref)
}

// deadRefDropReader releases the cached mmap'd fbreader for a reaped
// (repoPath, ref) so its resident graph leaves memory (#5236). Wired as the
// dead-ref sweeper's DropReader hook.
func deadRefDropReader(repoPath, ref string) {
	daemonMCPCache.InvalidateForRepoRef(repoPath, ref)
}

// dashboardGroupInvalidator is the dashboard GraphCache per-group eviction hook
// (#5238). Set by setDashboardGroupInvalidator when the embedded dashboard
// server is constructed; nil before that (and in non-dashboard daemon test
// paths), in which case tierEvictCallback skips the dashboard eviction.
// Guarded by dashboardGroupInvalidatorMu so the construct-time set and the
// scanner-goroutine read never race.
var (
	dashboardGroupInvalidatorMu sync.RWMutex
	dashboardGroupInvalidator   func(group string)
)

// setDashboardGroupInvalidator registers (or replaces) the dashboard GraphCache
// per-group invalidator used by tierEvictCallback on WARM→COLD demotion.
func setDashboardGroupInvalidator(fn func(group string)) {
	dashboardGroupInvalidatorMu.Lock()
	dashboardGroupInvalidator = fn
	dashboardGroupInvalidatorMu.Unlock()
}

// groupsForRepoPath returns the registry group name(s) that contain repoPath.
// A repo can in principle appear in more than one group, so all matches are
// returned. Best-effort: registry/config read errors yield an empty slice
// (the caller simply skips dashboard eviction for the unmappable repo).
func groupsForRepoPath(repoPath string) []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	var out []string
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			if r.Path == repoPath {
				out = append(out, g.Name)
				break
			}
		}
	}
	return out
}

// tierEvictCallback releases the in-memory graph for a WARM→COLD transition.
//
// #5238: in addition to dropping the cheap mmap'd fbreader in the MCP cache,
// it now also evicts the dashboard GraphCache's heavy materialised graph state
// (the full *graph.Document slices, re-derived Pass-4 algorithm results, and
// the per-group search index) for every group containing the demoted repo.
// These derived structures are the dominant remaining idle-heap consumer; the
// mmap'd graph.fb on disk is the source of truth, so dropping them is safe and
// they rebuild lazily on the next dashboard request for the group.
func tierEvictCallback(key tier.SlotKey) {
	// Invalidate the mmap'd fbreader in the MCP graph cache. #5891: evict by
	// state dir so whichever graph.<gen>.fb (or legacy flat graph.fb) is
	// resident for this slot is dropped, regardless of generation number.
	stateDir := daemon.StateDirForRepoRef(key.RepoPath, key.Ref)
	daemonMCPCache.InvalidateDir(stateDir)

	// #5238: drop the dashboard GraphCache's materialised state for this repo's
	// group(s) so its derived heap is reclaimed promptly on idle rather than
	// only when the group is next re-requested past its TTL.
	dashboardGroupInvalidatorMu.RLock()
	inv := dashboardGroupInvalidator
	dashboardGroupInvalidatorMu.RUnlock()
	if inv != nil {
		for _, g := range groupsForRepoPath(key.RepoPath) {
			inv(g)
		}
	}
}

// tierReloadCallback reloads the mmap'd fbreader into the MCP graph cache
// when a COLD slot receives a query (cold wake).
//
// PH2a (#2096): after reloading the graph, compare the graph.fb mtime against
// the newest source-file mtime in the repo. If the repo has changed since the
// graph was last indexed, enqueue a reactive reindex so the query is served
// from the most up-to-date graph on the next request.
//
// #2645: also ensure the fsnotify subscription is live after a cold wake.
// In the normal path the subscription is kept through WARM→COLD (the Pause
// is now deferred to COLD→EXPIRED), but an EXPIRED slot that got re-indexed
// will have had its subscription removed. Resume is idempotent, so this call
// is safe even when the subscription is already active.
func tierReloadCallback(key tier.SlotKey) error {
	stateDir := daemon.StateDirForRepoRef(key.RepoPath, key.Ref)
	// #5915 J1 FIX-3: prime the cache via the segment-aware ref entry point.
	// The old code called Get(CurrentGraphPath(stateDir)); for a segment-set
	// (graph.<gen>/ dir + manifest) the flat .fb path is absent → Get's stat
	// errors → the cold-wake reload fails → a cold-evicted segmented repo could
	// never be re-warmed (monolith serving break). GetForRepoRef resolves the
	// descriptor and keys a segment-set on its gen dir (opening a MultiReader),
	// while single-file/legacy keys on the resolved .fb path exactly as before.
	_, release, err := daemonMCPCache.GetForRepoRef(key.RepoPath, key.Ref)
	if err != nil {
		return err
	}
	release()

	// Re-establish the fsnotify subscription on cold wake (#2645).
	if daemonWatcherMgr != nil {
		daemonWatcherMgr.Resume(key.RepoPath, key.Ref)
	}

	// Stale-detection freshness reference: the resolved gen file, or — for a
	// segment-set — the manifest.json (a real file whose mtime advances on each
	// rebuild). The flat CurrentGraphPath would be absent for a segment-set, so
	// isRepoDirtyAfter would read "graph missing → never dirty" and silently skip
	// the reactive reindex.
	fbPath := graph.CurrentGraphPath(stateDir)
	if desc, dErr := graph.CurrentGraphDescriptor(stateDir); dErr == nil && desc.Kind == graph.GraphSegmentSet {
		fbPath = filepath.Join(desc.GenDir, graph.ManifestFileName)
	}
	if isRepoDirtyAfter(key.RepoPath, fbPath) {
		if daemonSchedulerEnqueue != nil {
			daemonSchedulerEnqueue(key.RepoPath)
		}
	}
	return nil
}

// tierDiskEvictCallback is the PH6 COLD→EXPIRED disk deletion hook.
// It deletes the refs/<ref-safe>/ directory for the expired slot and returns
// the bytes freed. Pinned-main slots never reach EXPIRED, so no guard needed
// here — the tier Manager already suppresses transitions for isPinnedMain slots.
func tierDiskEvictCallback(key tier.SlotKey) (int64, error) {
	stateDir := daemon.StateDirForRepoRef(key.RepoPath, key.Ref)
	freed, err := dirSize(stateDir)
	if err != nil {
		// Directory may not exist — not an error worth surfacing.
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.RemoveAll(stateDir); err != nil {
		return 0, err
	}
	return freed, nil
}

// dirSize returns the total byte size of all files under dir.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}

// isRepoDirtyAfter returns true when any non-skipped file under repoPath has a
// mtime newer than refPath. This is the cold-wake stale-detection check for
// PH2a (#2096). It caps its walk at 50,000 files to bound latency.
func isRepoDirtyAfter(repoPath, refPath string) bool {
	fi, err := os.Stat(refPath)
	if err != nil {
		return false // graph.fb missing — let the reload fail first
	}
	graphMtime := fi.ModTime()

	const maxWalk = 50_000
	n := 0
	dirty := false
	_ = filepath.WalkDir(repoPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := filepath.Base(p)
			// Skip the same directories the watcher skips.
			if p != repoPath && shouldSkipDirForStale(base) {
				return filepath.SkipDir
			}
			return nil
		}
		n++
		if n > maxWalk {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(graphMtime) {
			dirty = true
			return filepath.SkipAll
		}
		return nil
	})
	return dirty
}

// shouldSkipDirForStale reuses the watcher skip list for the stale-detection
// walk. Keep in sync with internal/daemon/watch.ShouldSkipDir.
func shouldSkipDirForStale(base string) bool {
	switch base {
	case ".git", "node_modules", ".grafel", "target", "dist",
		".gradle", ".idea", "vendor", "__pycache__", ".tox", ".venv",
		".mypy_cache", ".pytest_cache", ".eggs", "*.egg-info",
		"build", "out", "bin", "obj", ".next", ".nuxt", ".cache":
		return true
	}
	return false
}

// lazyWatcherMgrStats implements daemon.watcherMgrStatsIface (via structural
// interface matching) by delegating to daemonWatcherMgr. Safe to pass before
// daemonWatcherMgr is set — returns 0 while nil. PH2a (#2096).
type lazyWatcherMgrStats struct{}

func (l *lazyWatcherMgrStats) ActiveCount() int {
	if daemonWatcherMgr == nil {
		return 0
	}
	return daemonWatcherMgr.ActiveCount()
}

func (l *lazyWatcherMgrStats) PausedCount() int {
	if daemonWatcherMgr == nil {
		return 0
	}
	return daemonWatcherMgr.PausedCount()
}
