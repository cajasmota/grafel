package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/agentpatterns"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/daemon/watchreg"
	"github.com/cajasmota/grafel/internal/daemon/worktree"
	"github.com/cajasmota/grafel/internal/gitmeta"
)

// enginePlane holds the engine-plane background runtime (scheduler, watcher,
// git-HEAD poller, worktree discovery, reapers/sweepers, the status writer,
// pattern-decay, and the docgen sweeper) together with its ordered teardown.
//
// ADR-0024 Phase 1 (epic #5729) carved this plane out of the monolithic
// daemon.Run body so it can be started in exactly two places with identical
// wiring: in-process (the flag-off monolith / today's daemon) and standalone
// (RunEngine, the split-mode `grafel engine` child). The extraction is
// behavior-preserving for the monolith: the same constructors run in the same
// order, and shutdown() unwinds them LIFO — matching the defer-stack order the
// inline code used before.
type enginePlane struct {
	stops []func()
}

// add registers a teardown closure. Closures are unwound LIFO by shutdown(),
// so call add in the SAME order the original inline code used `defer`.
func (e *enginePlane) add(stop func()) { e.stops = append(e.stops, stop) }

// shutdown unwinds every registered teardown closure in LIFO order — the exact
// order Go's defer stack used when these lived inline in Run.
func (e *enginePlane) shutdown() {
	for i := len(e.stops) - 1; i >= 0; i-- {
		e.stops[i]()
	}
}

// startEnginePlane brings up the engine plane and returns it (call shutdown()
// to unwind). svc may be nil: the standalone engine (RunEngine) has no MCP
// Service, so the scheduler/watcher are not published onto one; the monolith
// passes its live *Service so Status can report scheduler/watcher state.
//
// This is a verbatim carve of the engine-plane blocks that used to live inline
// in Run (ADR-0024 Phase 1). The only adaptations are: (a) `defer X` became
// ep.add(X) so teardown is owned by the returned enginePlane rather than Run's
// stack, and (b) the two svc field writes are nil-guarded.
func startEnginePlane(ctx context.Context, cfg Config, svc *Service, logger *slog.Logger) *enginePlane {
	ep := &enginePlane{}

	// #5729 PR3: the engine-global liveness/warming heartbeat now starts here
	// — UNCONDITIONALLY, before the scheduler-gated block below — so it runs
	// identically in the monolith (escape-hatch GRAFEL_SPLIT_MODE=0) AND the standalone
	// engine (RunEngine), giving serve ONE code path to read regardless of
	// split mode (the equivalence property this PR exists to guarantee).
	// Previously this heartbeat was only started by RunEngine, so a monolith
	// daemon never published engine-global fields (busy/parsing/concurrency/
	// warming) to the status plane at all.
	//
	// schedulerPtr is populated (if a scheduler is configured) once Start()
	// below returns; warmingFn reads it lock-free and degrades to the zero
	// WarmingSnapshot (not warming) before the scheduler is up or when none is
	// configured (e.g. a bare-RPC test harness) — never a crash or stale read.
	var schedulerPtr atomic.Pointer[sched.Scheduler]
	warmingFn := func() WarmingSnapshot {
		sc := schedulerPtr.Load()
		if sc == nil {
			return WarmingSnapshot{}
		}
		snap := sc.Snapshot()
		return WarmingSnapshot{
			IndexInFlight: len(snap.InFlight) > 0,
			PendingAlgo:   len(snap.PendingAlgo),
			PendingLinks:  len(snap.PendingLinks),
		}
	}
	stopEngineLiveness := startEngineLivenessHeartbeat(cfg.Layout.Root, statusHeartbeatInterval(), warmingFn, logger)
	ep.add(stopEngineLiveness)

	// Phase B — bring up the scheduler + watcher when the caller
	// supplied the four hooks. They are optional so tests can exercise
	// the bare RPC surface without dragging the extractor into scope.
	logger.Info("startup: scheduler-watcher begin", "enabled", cfg.SchedulerIndex != nil)
	if cfg.SchedulerIndex != nil {
		history := sched.LoadRSSHistory(cfg.RSSHistoryPath)
		scheduler := sched.New(sched.Config{
			Index:         cfg.SchedulerIndex,
			Links:         cfg.SchedulerLinks,
			GroupAlgo:     cfg.SchedulerGroupAlgo,
			GroupsForRepo: cfg.GroupsForRepo,
			// #5403: settled-group overlay-freshness sweep. Enabled when the
			// caller wires SchedulerStaleGroups; the interval defaults from
			// GRAFEL_OVERLAY_SWEEP_INTERVAL (10m; "0" disables).
			StaleGroups: cfg.SchedulerStaleGroups,
			Logger:      logger,
			BudgetMB:    cfg.MaxRSSBudgetMB,
			Predict:     sched.PredictRSS,
			History:     history,
			// PH1b: capture the HEAD ref at enqueue time so debounced
			// batches index against the branch that was active when the
			// file-change event fired, not the branch at dispatch time.
			// #5726: also capture the commit SHA so the reindex circuit
			// breaker can key on the commit.
			RefCapture: func(repoPath string) (ref, commit string) {
				info := gitmeta.Capture(repoPath)
				return info.Ref, info.SHA
			},
			// #3680: drop enqueues for linked git worktrees of an
			// already-indexed primary repo so they never become independent
			// root index jobs (each spawning its own ~100MB full graph store
			// and pressuring the RSS admission budget). The worktree subsystem
			// still tracks such paths as ephemeral children with aggressive
			// TTLs. The indexed-primary set is the boot-time ReposToWatch list.
			SkipEnqueue: makeWorktreeEnqueueGate(cfg.ReposToWatch),
			// S3 incremental file-level reindex (issue #2153). When nil
			// the scheduler falls through to full reindex on every tick.
			Incremental: cfg.SchedulerIncremental,
			// #5710 follow-up: entities=N on the completion log.
			EntityCount: cfg.SchedulerEntityCount,
			// Issue #2397: single source of truth for the incremental toggle.
			// The scheduler calls ExtractorConfig.IsIncrementalEnabled()
			// rather than reading the env var directly.
			ExtractorConfig: cfg.ExtractorConfig,
		})
		if cfg.MaxRSSBudgetMB > 0 {
			logger.Info("scheduler: RSS-budget admission control enabled", "budget_mb", cfg.MaxRSSBudgetMB, "history", cfg.RSSHistoryPath)
		}
		scheduler.Start()
		schedulerPtr.Store(scheduler)
		if svc != nil {
			svc.scheduler = scheduler
		}
		ep.add(scheduler.Stop)

		// ADR-0024 PR4 (epic #5729): the serve→engine request-file queue
		// consumer. Only meaningful in split mode — the monolith (flag off,
		// the default) never has anything to drain because Service.Index's
		// async fast path calls scheduler.Enqueue directly there (see
		// service.go). Gating on SplitModeEnabled() keeps monolith behavior
		// byte-identical: no extra goroutine, no directory globbing, when the
		// flag is off.
		if SplitModeEnabled() {
			// PR6 prerequisite (epic #5729): cfg.Rebuild is threaded through
			// so a drained KindRebuild request invokes the SAME RebuildFunc
			// the monolith/engine calls in-process from Service.Rebuild —
			// see requests_drain.go's applyRequest and service.go's
			// Rebuild split-mode branch.
			ep.add(startRequestsDrainLoop(scheduler, cfg.Rebuild, logger))
		}

		// #5725/#5729-W1: status-plane heartbeat file. startStatusWriter runs a
		// SINGLE serialized writer goroutine that refreshes every known repo's
		// on-disk status sidecar (internal/statusfile): promptly on each
		// scheduler state transition (via a coalescing notify hook) and
		// periodically on a heartbeat tick, so a reader can detect a
		// wedged/crashed engine via a stale heartbeat. Serializing through one
		// goroutine makes concurrent same-repo writes impossible (review #5734)
		// and coalesces bursts. The returned stop func unregisters the hook and
		// joins the goroutine; it unwinds alongside scheduler.Stop() above.
		//
		// Deliberately NOT cfg.ReposToWatch: that callback is invoked exactly
		// once by the boot-path watcher-subscription goroutine below, and some
		// callers (e.g. TestBoot_WatcherSubscriptionDoesNotBlockBind) construct
		// it with one-shot side effects. knownRepoPathsForStatus is a
		// side-effect-free repo lister safe to call on every tick/refresh.
		statusRepos := func() []string { return knownRepoPathsForStatus(logger) }
		stopStatusWriter := startStatusWriter(statusRepos, statusHeartbeatInterval(), logger)
		ep.add(stopStatusWriter)

		// #5690: hand a read-only warming accessor to the wiring layer so the
		// MCP surface can report warming state. Reuses the SAME warmingFn the
		// engine-liveness heartbeat above publishes to the status plane
		// (#5729 PR3), so an in-process consumer (monolith) and a status-file
		// consumer (serve, split mode) observe identical warming data. Closes
		// over the live scheduler via schedulerPtr; no scheduling authority.
		if cfg.OnSchedulerReady != nil {
			cfg.OnSchedulerReady(warmingFn)
		}

		wcfg := cfg.WatcherConfig
		watcher, werr := watch.NewWatcherConfig(wcfg, func(repo string, bulk bool) {
			if bulk {
				logger.Info("watcher: bulk trigger — enqueuing full reindex", "repo", repo)
			}
			scheduler.Enqueue(repo)
		}, logger)
		if werr != nil {
			logger.Warn("watcher: disabled", "err", werr)
		} else {
			if svc != nil {
				svc.watcher = watcher
			}
			ep.add(watcher.Stop)

			// PH1b (Option B): start the .git/HEAD poller alongside the
			// fsnotify watcher. .git/ remains in SkipDirs (no fsnotify
			// noise from git internal object/pack writes), and the poller
			// detects branch switches by reading gitmeta.Capture every 2s.
			//
			// When a branch switch is detected:
			//   1. A synthetic EnqueueRef is sent to the scheduler with the
			//      new ref captured at detection time.
			//   2. The scheduler writes the new index into refs/<new-ref>/,
			//      leaving the old ref's graph untouched on disk.
			headPoller := watch.NewGitHeadPoller(0, func(ev watch.BranchSwitchEvent) {
				logger.Info("branch-switch detected",
					"repo", ev.RepoPath, "old_ref", ev.OldRef, "old_sha", ev.OldSHA, "new_ref", ev.NewRef, "new_sha", ev.NewSHA)
				// Notify the MCP cross-link cache so stale (repo, oldRef)
				// entries are evicted before the new-ref graph lands (#2224).
				if cfg.BranchSwitchSink != nil {
					cfg.BranchSwitchSink(ev.RepoPath, ev.OldRef)
				}
				// #5726: carry the new commit SHA so the reindex circuit breaker
				// keys on the commit (branch switches always change the SHA →
				// the breaker resets and the new ref gets a real attempt).
				scheduler.EnqueueRefCommit(ev.RepoPath, ev.NewRef, ev.NewSHA)
			}, logger)
			headPoller.Start()
			ep.add(headPoller.Stop)

			// M2 (#2179): lazy fsnotify subscription — do NOT call watcher.AddRepo
			// at boot. The daemon starts with zero fsnotify subscriptions. Repos
			// are subscribed on the first MCP query for their group (via
			// SubscribeGroupWatcher → watch.DefaultManager.SubscribeGroup →
			// watcher.AddRepo). This eliminates per-repo directory-tree walks at
			// startup, saving ~O(dirs×groups) inotify watch descriptors on idle daemons.
			//
			// We still call ReposToWatch (exactly once) to:
			//   (a) register repos with the HEAD poller (branch-switch detection; no fd cost)
			//   (b) run the case-collision store audit (#2086)
			// Both happen off the critical boot path in a goroutine, same as before.
			if cfg.ReposToWatch != nil {
				capturedStore := StoreDir()
				go func() {
					t0 := time.Now()
					repos := cfg.ReposToWatch()
					for _, r := range repos {
						// M2: skip watcher.AddRepo here — subscriptions are lazy.
						// Register with the HEAD poller only (reads .git/HEAD; no
						// fsnotify watch descriptors consumed).
						headPoller.AddRepo(r)
					}
					logger.Info("watcher: boot-path registered with HEAD poller (fsnotify lazy — 0 AddRepo calls)",
						"repos", len(repos), "took", time.Since(t0).Truncate(time.Millisecond).String())

					// #2086: case-collision audit — unchanged.
					if capturedStore != "" {
						if dups := WarnCaseCollisions(capturedStore, repos); len(dups) > 0 {
							for _, pair := range dups {
								logger.Warn("store: detected case-collision dup — remove stale dir to avoid confusion (grafel cleanup --case-merge)",
									"stale", pair[0], "canonical", pair[1])
							}
						}
					}
				}()
			}
			if cfg.OnWatcherReady != nil {
				cfg.OnWatcherReady(watcher)
			}

			// #3353/#3354: linked-worktree discovery + working-tree watching.
			// Gated on the fsnotify watcher being up (we reuse it to watch each
			// worktree's working tree) and on a caller-supplied parents provider
			// (non-nil only when some group opts into worktree tracking).
			var wtStore *worktree.Store
			if cfg.WorktreeParents != nil {
				wtStorePath := filepath.Join(cfg.Layout.Root, "worktrees.json")
				wtStore = worktree.NewStore(wtStorePath)
				if err := wtStore.Load(); err != nil {
					logger.Warn("worktree: failed to load store; starting empty", "path", wtStorePath, "err", err)
				}
				wtWatcher := worktree.NewWatcher(wtStore, cfg.WorktreeParents, logger)

				// #5675: bound the fsnotify-subscription fan-out. Each
				// watcher.AddRepo(child.Path) subscribes the worktree's ENTIRE
				// working tree, costing ~1 fd per directory on Linux inotify —
				// UNBOUNDED by the number of activated worktrees. A burst of
				// activations could open a flood of fds at once and crash the
				// daemon into a KeepAlive/Restart relaunch loop. This semaphore
				// caps how many subscriptions can be opening concurrently. For
				// the common case (activations are dispatched sequentially by
				// poll, one at a time) the semaphore is always immediately
				// available — zero behavior change. Overridable via
				// GRAFEL_WORKTREE_ACTIVATE_CONCURRENCY.
				activateSem := make(chan struct{}, worktreeActivateConcurrency())

				// On activation, subscribe the worktree's WORKING TREE to the
				// fsnotify watcher so uncommitted edits trigger a reactive
				// reindex, and enqueue one immediate reindex of its ref tier.
				// scheduler.Enqueue captures the worktree's checked-out ref via
				// RefCapture(worktreePath), so the graph lands in the correct
				// per-ref dir keyed by the worktree path (multi-ref model).
				wtWatcher.OnActivate = func(child *worktree.WorktreeChild) {
					activateSem <- struct{}{}
					_, aerr := watcher.AddRepo(child.Path)
					<-activateSem
					if aerr != nil {
						logger.Warn("worktree: failed to watch working tree", "path", child.Path, "err", aerr)
					}
					scheduler.Enqueue(child.Path)
					logger.Info("worktree: watching working tree + enqueued initial reindex",
						"path", child.Path, "branch", child.Branch, "group", child.GroupName, "slug", child.ParentSlug, "locked", child.Locked)
				}
				// On expiry, unsubscribe the working tree from the watcher.
				wtWatcher.OnExpire = func(child *worktree.WorktreeChild) {
					watcher.RemoveRepo(child.Path)
					logger.Info("worktree: unwatched expired working tree", "path", child.Path)
				}

				wtCtx, wtCancel := context.WithCancel(ctx)
				go wtWatcher.Start(wtCtx)
				ep.add(wtCancel)
				logger.Info("worktree: discovery started",
					"store", wtStorePath, "reconcile_env", "GRAFEL_WORKTREE_POLL_SECONDS")
			}

			// #3680: vanished-repo store reaper. Tracked repos (registered +
			// active worktree children) whose directory no longer exists on
			// disk have their store dir deleted and their fsnotify
			// subscription dropped, reclaiming the orphaned ~100MB worktree
			// stores that accumulated under ~/.grafel/store/.
			trackedRepos := makeReaperTrackedRepos(cfg.ReposToWatch, wtStore)
			// #5236: dead-ref / dead-worktree sweep. Reclaims store dirs +
			// resident graphs for refs git no longer knows about, within
			// still-present repos. Driven by the reaper on the shared cadence.
			// Retention cap is env-tunable (GRAFEL_REF_RETENTION_CAP) so an
			// operator can shrink the dead-ref footprint on a machine with
			// heavy transient-ref churn (e.g. set it to 4). Resolved here so the
			// effective value is logged; NewDeadRefSweeper would resolve the same
			// value from a zero RetentionCap on its own.
			refRetentionCap := EnvRefRetentionCap()
			logger.Info("deadref: retention cap configured",
				"cap", refRetentionCap, "env", RefRetentionCapEnv)
			deadRefSweeper := NewDeadRefSweeper(DeadRefConfig{
				TrackedRepos:   trackedRepos,
				LiveRefs:       LiveGitRefs,
				PrimaryRef:     PrimaryGitRef,
				RefsDirForRepo: RefsDirForRepo,
				DropReader:     cfg.DeadRefDropReader,
				Tier:           cfg.DeadRefTier,
				RetentionCap:   refRetentionCap,
				Logger:         logger,
			})
			// #5263: orphan top-level store-root sweep. Reaps whole
			// `<store>/<slug>-<hash>/` roots that map to a vanished source path
			// and to no live group/primary — the gap between the vanished-repo
			// GC (currently-tracked repos only) and the dead-ref GC (refs within
			// still-tracked repos only). KnownSourcePaths includes EXPIRED
			// worktrees so a now-gone path's root can still be attributed; roots
			// attributable to no known path are kept (fail-closed).
			orphanRootSweeper := NewOrphanRootSweeper(OrphanRootConfig{
				KnownSourcePaths: makeKnownSourcePaths(cfg.ReposToWatch, wtStore),
				// Tier / DropReaderForRoot are per-repo-path hooks; the daemon
				// does not currently expose a whole-repo Forget here, and a
				// reaped orphan root's source path is already GONE (no live
				// slot to wake), so on-disk reclamation is the load-bearing
				// effect. Any residual in-mem slot is dropped by the
				// vanished-repo reaper / memory-pressure eviction.
				Logger: logger,
			})
			reaper := NewReaper(ReaperConfig{
				TrackedRepos:    trackedRepos,
				StoreDirForRepo: repoBaseDir,
				Untrack: func(repoPath string) {
					watcher.RemoveRepo(repoPath)
				},
				// #5142: also reap stale/orphaned `grafel watch` PIDs that
				// registered in the daemon-owned registry under the daemon root.
				WatchRegistry: watchreg.New(watchreg.DefaultPath(cfg.Layout.Root)),
				// #5632: also reap live `grafel watch` processes for managed
				// repos that run from a STALE/foreign binary (version skew) or
				// duplicate an existing watcher. ManagedRepo gates the sweep to
				// the daemon's tracked repos only; the watcher's repo arg is
				// matched (cleaned-absolute) against this set so unrelated
				// processes are never touched.
				ManagedRepo: makeManagedRepoPredicate(trackedRepos),
				// #5933: this reaper runs inside the ENGINE process, not the
				// daemon/serve process that stamps watcher entries'
				// OwnerDaemonPID (internal/cli/watch.go's liveDaemonPID), so
				// os.Getpid() here is never the right comparison. Resolve the
				// daemon/serve pidfile instead so orphan detection compares
				// against the right process.
				LiveDaemonPID: func() int { return ReadPIDFile(cfg.Layout.PIDPath) },
				DeadRefs:      deadRefSweeper,
				OrphanRoots:   orphanRootSweeper,
				Logger:        logger,
			})
			reaperStop := make(chan struct{})
			reaper.Start(reaperStop)
			ep.add(func() { close(reaperStop) })
			logger.Info("reaper: vanished-repo store GC started", "interval", "5m")
		}
	}
	logger.Info("startup: scheduler-watcher done")

	// Pattern confidence time-decay scheduler — runs every 6 hours (or a
	// caller-supplied interval for tests). Requires PatternGroupDirs to be
	// non-nil; skipped gracefully when the caller has not provided it.
	logger.Info("startup: pattern-decay begin", "enabled", cfg.PatternGroupDirs != nil)
	if cfg.PatternGroupDirs != nil {
		decayInterval := cfg.PatternDecayInterval
		if decayInterval <= 0 {
			decayInterval = 6 * time.Hour
		}
		decayJob := buildPatternDecayJob(cfg.PatternGroupDirs, logger)
		decaySched := agentpatterns.NewDecayScheduler(decayInterval, decayJob)
		decayCtx, decayCancel := context.WithCancel(ctx)
		go decaySched.Run(decayCtx)
		ep.add(decayCancel)
		logger.Info("pattern decay scheduler started", "interval", decayInterval.String())
	}
	logger.Info("startup: pattern-decay done")

	// Docgen background sweeper (issue #2216): removes stale staging runs and
	// .previous-* backups every 24 h. Opt-in: nil = disabled (--no-auto-cleanup).
	logger.Info("startup: docgen-sweeper begin", "enabled", cfg.DocgenSweep != nil)
	if cfg.DocgenSweep != nil {
		sweepCfg := *cfg.DocgenSweep
		sweepCfg.Logger = logger
		sweepStop := make(chan struct{})
		StartDocgenSweeper(sweepCfg, sweepStop)
		ep.add(func() { close(sweepStop) })
		interval := sweepCfg.Interval
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		logger.Info("docgen sweeper started", "interval", interval.String())
	}
	logger.Info("startup: docgen-sweeper done")

	return ep
}
