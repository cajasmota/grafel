package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cajasmota/grafel/internal/agentpatterns"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/daemon/transport"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/daemon/watchreg"
	"github.com/cajasmota/grafel/internal/daemon/worktree"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/gitmeta"
)

// Config configures Run. Fields are required unless documented otherwise.
type Config struct {
	Layout       Layout           // on-disk paths (see DefaultLayout)
	Index        IndexFunc        // injected from cmd/grafel
	Rebuild      RebuildFunc      // injected from cmd/grafel
	QualityAudit QualityAuditFunc // injected from cmd/grafel (Phase E)
	// Logger is the *slog.Logger used by daemon-internal code and all sub-packages.
	// When nil, Run constructs a default stderr slog.Logger.
	Logger *slog.Logger

	// Phase B optional wiring. When all four are non-nil the daemon
	// starts the fsnotify watcher + scheduler and registers every
	// repo returned by ReposToWatch. The Index field above remains
	// the synchronous RPC entrypoint; the scheduler uses
	// SchedulerIndex for fast (algo-skipped) reactive reindexes.
	ReposToWatch  func() []string                // repos to subscribe at startup
	GroupsForRepo func(repoPath string) []string // for cross-repo link debounce

	// WorktreeParents, when non-nil, enables linked-worktree discovery
	// (#3353/#3354). It returns the set of registered repos whose group has
	// worktree tracking enabled (features.track_worktrees / watchers). The
	// daemon starts a worktree.Watcher that:
	//   - discovers each parent's linked worktrees and persists them to
	//     ~/.grafel/worktrees.json,
	//   - subscribes each worktree's WORKING TREE to the fsnotify watcher so
	//     uncommitted edits trigger a reindex of that worktree's ref tier,
	//   - watches each parent's .git/worktrees/ dir (event-driven onboarding)
	//     and runs a periodic reconciliation poll for removals/missed events.
	// Only started when the fsnotify watcher itself is up. Returns nil when
	// no group opts in (the Watcher is then not started).
	WorktreeParents func() []worktree.ParentRepo
	SchedulerIndex  func(ctx context.Context, repo string, ref string) error // fast reindex (skip algo pass); ref is the git branch captured at enqueue time
	SchedulerLinks  func(ctx context.Context, group string) error
	SchedulerAlgo   func(ctx context.Context, repo string) error

	// SchedulerIncremental, when non-nil, is wired as the S3 incremental
	// file-level reindex hook (issue #2153). It is attempted before
	// SchedulerIndex when the incremental toggle is active. When nil
	// the incremental path is never tried (default: full reindex always).
	SchedulerIncremental func(ctx context.Context, repo string, ref string) sched.IncrementalResult

	// ExtractorConfig, when non-nil, is passed to the scheduler so it can
	// consult IsIncrementalEnabled() instead of reading
	// GRAFEL_INCREMENTAL_REINDEX from the process env directly (issue
	// #2397). When nil the scheduler falls back to the env-var path, which
	// preserves backward compatibility.
	ExtractorConfig *extractor.ExtractorConfig

	// MaxRSSBudgetMB caps the total predicted RSS of concurrently
	// running index jobs. 0 disables admission control (legacy
	// behaviour). The CLI sets this via --max-rss-budget on the
	// daemon subcommand or the GRAFEL_MAX_RSS_BUDGET_MB env var.
	MaxRSSBudgetMB int64

	// RSSHistoryPath is where the scheduler persists per-repo observed
	// peak RSS for predictor calibration. Empty disables history.
	RSSHistoryPath string

	// PatternDecayInterval controls how often the confidence time-decay
	// pass runs. Default (zero value) → 6 hours. Set to a shorter interval
	// for testing.
	PatternDecayInterval time.Duration

	// PatternGroupDirs is a function that returns a map of group-name →
	// patterns directory (the dir that contains patterns.json). When nil,
	// the decay scheduler is not started. Populated by cmd/grafel.
	PatternGroupDirs func() map[string]string

	// Phase D — MCP RPC surface (ADR-0017 #832).
	// Both fields are optional; when nil, MCPToolList returns an empty
	// catalog and MCPToolCall returns a "not configured" error block.
	// Injected from cmd/grafel (which imports internal/mcp) to avoid
	// the import cycle that would arise from importing internal/mcp here.
	MCPListTools MCPListToolsFunc
	MCPCallTool  MCPCallToolFunc

	// DashboardServe is an optional hook that starts the embedded HTTP
	// dashboard alongside the daemon process (#929/#931). When non-nil,
	// Run calls it in a goroutine with the daemon's context so the
	// dashboard shuts down when the daemon shuts down.
	//
	// The hook is injected from cmd/grafel (which imports both
	// internal/daemon and internal/dashboard). Keeping it here as a
	// function value avoids the import cycle that would arise if
	// internal/daemon imported internal/dashboard directly.
	//
	// The hook receives the bind address and port to listen on, the daemon
	// logger, and an onListen callback. It should call onListen exactly once
	// with the listener's RESOLVED address (e.g. "127.0.0.1:54231") as soon
	// as net.Listen succeeds — this matters when port==0 (OS-assigned), so a
	// caller can learn the actual port without a pick-then-rebind race
	// (#5224). It should block until ctx is done.
	DashboardServe func(ctx context.Context, bind string, port int, logger *slog.Logger, onListen func(addr string)) error

	// DashboardPort is the TCP port for the embedded dashboard HTTP server
	// (#929/#931). When negative the dashboard is disabled. 0 means "let the
	// OS pick a free port at bind time" (used by `grafel selftest` to avoid a
	// pick-then-close-then-rebind race that flakes on Windows — #5224); the
	// resolved port is reported via OnDashboardListen. Default production
	// value is 47274. Configurable via GRAFEL_DASHBOARD_PORT env or
	// ~/.config/grafel/daemon.toml.
	DashboardPort int

	// DashboardBind is the bind address for the dashboard TCP listener.
	// Defaults to "127.0.0.1" (loopback-only).
	DashboardBind string

	// OnDashboardListen, when non-nil, is called with the dashboard
	// listener's RESOLVED address (host:port) once it binds. This lets a
	// caller that passed DashboardPort==0 learn the OS-assigned port without
	// pre-binding+closing+rebinding (#5224).
	OnDashboardListen func(addr string)

	// OnDashboardError, when non-nil, is called with any error returned by
	// the DashboardServe hook (e.g. a bind failure). The dashboard goroutine
	// is non-fatal by design, so this hook lets a caller surface the real
	// reason a readiness probe never sees the dashboard come up (#5224).
	OnDashboardError func(err error)

	// WatcherConfig tunes the file watcher. Zero value uses built-in
	// defaults (5 s debounce, 50-event bulk threshold, 30 s heartbeat).
	// Populated from daemon.toml or CLI flags (watcher_debounce_ms,
	// watcher_bulk_threshold). Added in #1270.
	WatcherConfig watch.Config

	// OnWatcherReady is called with the live watcher after it is
	// successfully created and repos are subscribed. Allows callers
	// (e.g. cmd/grafel) to wire the watcher into the dashboard
	// without creating an import cycle. Added in #1270.
	OnWatcherReady func(w *watch.Watcher)

	// WatcherMgrStats, when non-nil, is queried by the Status RPC to report
	// PH2a watcher pause/resume slot counts. Set by cmd/grafel after
	// onWatcherReady creates the DefaultManager. PH2a #2096.
	WatcherMgrStats watcherMgrStatsIface

	// MaxConcurrentGroups controls how many groups can be indexed in
	// parallel during a Rebuild RPC (cold start or forced rebuild).
	// 0 or 1 → serial (legacy behaviour). Default when unset: 2.
	// Configurable via --max-concurrent-groups on the daemon subcommand
	// or GRAFEL_MAX_CONCURRENT_GROUPS env var. Added in #1276.
	MaxConcurrentGroups int

	// DaemonMode is the operational mode the daemon was booted in (S7 #2157).
	// One of "background", "workstation", "readonly". Empty string means
	// the caller did not specify a mode (treated as background).
	// Surfaced in Status RPC so `grafel status` can display it.
	DaemonMode string

	// DocgenSweep, when non-nil, starts the background docgen cleanup
	// goroutine (issue #2216). The goroutine runs at startup and every 24 h,
	// removing stale staging runs and .previous-* backups older than MaxAge.
	// Set to nil (default) to disable. Disabled via --no-auto-cleanup on
	// `grafel start`.
	DocgenSweep *DocgenSweeperConfig

	// BranchSwitchSink, when non-nil, is called by the daemon's .git/HEAD
	// poller whenever a branch switch is detected for a watched repo. The
	// arguments are (repoPath, oldRef) — the same values carried by
	// watch.BranchSwitchEvent. The hook is called synchronously inside the
	// poller callback, before the scheduler enqueues the new ref.
	//
	// Injected from cmd/grafel to call mcp.State.NotifyRefSwitch, which
	// invalidates stale CrossLinkCache entries keyed to (repo, oldRef) — this
	// closes the stale-cache bug tracked in issue #2224.
	BranchSwitchSink func(repoPath, oldRef string)

	// ShutdownCleanup, when non-nil, is called during graceful shutdown to
	// perform cleanup operations (e.g. flushing metrics). Best-effort: errors
	// are logged but do not block shutdown. Injected from cmd/grafel to call
	// the MCP server's Stop method (issue #2530).
	ShutdownCleanup func()

	// DeadRefTier, when non-nil, is the tier Manager's per-ref forget hook
	// (#5236). The dead-ref sweeper calls it to drop the in-memory slot for a
	// reaped ref. Injected from cmd/grafel (which owns daemonTierMgr).
	DeadRefTier RefForgetter

	// DeadRefDropReader, when non-nil, releases the cached mmap'd fbreader for a
	// reaped (repoPath, ref) so the resident graph leaves memory (#5236).
	// Injected from cmd/grafel to call the MCP graph cache's per-ref
	// invalidation. nil leaves the on-disk delete to run without an explicit
	// reader drop (the cache ages out on its own).
	DeadRefDropReader func(repoPath, ref string)
}

// Run starts the daemon. It blocks until either:
//   - the Service receives Stop,
//   - the process receives SIGTERM/SIGINT, or
//   - the listener errors fatally.
//
// On exit it removes the socket file and pid file. The function is the
// daemon's entire public surface — cmd/grafel just imports daemon
// and calls Run.
func Run(ctx context.Context, cfg Config) error {
	// slogger is the structured logger used by the daemon itself (Run + Service)
	// and all sub-packages. Handler selection is based on GRAFEL_DAEMON_LOG_JSON
	// at startup — this encodes the choice in the handler so call sites never check
	// the env var.
	slogger := cfg.Logger
	if slogger == nil {
		slogger = buildSlogLogger(os.Stderr)
	}
	// Keep a short alias for readability in the long Run body below.
	logger := slogger

	// #3648: apply a conservative Go soft memory limit so the runtime GCs
	// harder as it approaches the cap, bounding the 10.2GB peak observed on a
	// 16GB host during concurrent reindex bursts. Combined with the
	// scheduler's idle FreeOSMemory trigger this attacks both the PEAK
	// (GOMEMLIMIT) and the idle RETAINED arena (FreeOSMemory).
	applyMemoryLimit(logger)

	// Layer 1 self-defense: refuse to start if a canonical (non-/tmp) daemon
	// is already running and this binary lives under /tmp. This prevents the
	// hot-loop runaway observed on 2026-05-20 where agent-spawned daemons were
	// adopted by launchd after the agent exited and spun at ~1000% CPU.
	if err := SelfDefenseCheck(logger); err != nil {
		return err
	}

	if err := EnsureLayout(cfg.Layout); err != nil {
		return fmt.Errorf("ensure layout: %w", err)
	}

	// PH1b: one-time migration of legacy flat-layout store slots into the
	// per-ref sub-directory layout introduced by PH1a (#2089). Called here
	// (after EnsureLayout, before accepting RPCs) so every read path sees
	// the new layout. Idempotent: already-migrated stores are skipped.
	if storeDir := StoreDir(); storeDir != "" {
		if err := MigrateToRefStore(storeDir); err != nil {
			// Non-fatal: log and continue; the daemon can still serve the
			// old layout (callers fall back gracefully).
			logger.Warn("startup: MigrateToRefStore (non-fatal)", "err", err)
		} else {
			logger.Info("startup: MigrateToRefStore complete", "store", storeDir)
		}

		// #2085: prune old repo-hash generations so ~/.grafel/store/ does not
		// grow unboundedly. Runs after MigrateToRefStore so the layout is
		// normalised before we inspect mtime order. Non-fatal.
		keepN := KeepGenerations()
		if removed, freed := PruneStaleGenerations(storeDir, keepN, logger); removed > 0 {
			logger.Info("startup: pruned stale store generations",
				"removed", removed, "freed_bytes", freed, "keep_n", keepN)
		}
	}

	releasePID, err := AcquirePIDFile(cfg.Layout.PIDPath)
	if err != nil {
		return err
	}
	defer releasePID()

	// Remove any stale socket file from a previous crash (Unix only; on
	// Windows named pipes are not filesystem objects and os.Remove is a no-op).
	_ = os.Remove(cfg.Layout.SocketPath)

	listener, err := transport.Listen(cfg.Layout.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Layout.SocketPath, err)
	}
	// On Unix, chmod 0600 makes the socket per-user. The transport package
	// sets an equivalent ACL on Windows named pipes so no explicit Chmod is
	// needed there. chmodSocket is a no-op on Windows.
	if err := chmodSocket(cfg.Layout.SocketPath); err != nil {
		_ = listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(cfg.Layout.SocketPath)
	}()

	stopReq := make(chan struct{})
	svc := newService(cfg.Index, cfg.Rebuild, cfg.QualityAudit, cfg.Layout.SocketPath, stopReq, logger, cfg.MaxConcurrentGroups)
	svc.mcpListTools = cfg.MCPListTools
	svc.mcpCallTool = cfg.MCPCallTool
	if cfg.DashboardPort > 0 {
		svc.dashboardPort = cfg.DashboardPort
	}
	// S7 (#2157): wire the operational mode so Status can surface it.
	svc.daemonMode = cfg.DaemonMode
	// PH2a (#2096): wire watcher pause/resume slot counts into Status RPC.
	if cfg.WatcherMgrStats != nil {
		svc.watcherMgrStats = cfg.WatcherMgrStats
	}

	// Layer 2 self-defense: start CPU watchdog for ephemeral /tmp daemons.
	// The watchdog passes the service's real inFlight counter so it can
	// distinguish hot-loops (no work) from legitimate sustained indexing.
	StartCPUWatchdog(&svc.inFlight, logger)

	// Phase B — bring up the scheduler + watcher when the caller
	// supplied the four hooks. They are optional so tests can exercise
	// the bare RPC surface without dragging the extractor into scope.
	if cfg.SchedulerIndex != nil {
		history := sched.LoadRSSHistory(cfg.RSSHistoryPath)
		scheduler := sched.New(sched.Config{
			Index:         cfg.SchedulerIndex,
			Links:         cfg.SchedulerLinks,
			Algorithms:    cfg.SchedulerAlgo,
			GroupsForRepo: cfg.GroupsForRepo,
			Logger:        logger,
			BudgetMB:      cfg.MaxRSSBudgetMB,
			Predict:       sched.PredictRSS,
			History:       history,
			// PH1b: capture the HEAD ref at enqueue time so debounced
			// batches index against the branch that was active when the
			// file-change event fired, not the branch at dispatch time.
			RefCapture: func(repoPath string) string {
				return gitmeta.Capture(repoPath).Ref
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
			// Issue #2397: single source of truth for the incremental toggle.
			// The scheduler calls ExtractorConfig.IsIncrementalEnabled()
			// rather than reading the env var directly.
			ExtractorConfig: cfg.ExtractorConfig,
		})
		if cfg.MaxRSSBudgetMB > 0 {
			logger.Info("scheduler: RSS-budget admission control enabled", "budget_mb", cfg.MaxRSSBudgetMB, "history", cfg.RSSHistoryPath)
		}
		scheduler.Start()
		svc.scheduler = scheduler
		defer scheduler.Stop()

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
			svc.watcher = watcher
			defer watcher.Stop()

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
				scheduler.EnqueueRef(ev.RepoPath, ev.NewRef)
			}, logger)
			headPoller.Start()
			defer headPoller.Stop()

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

				// On activation, subscribe the worktree's WORKING TREE to the
				// fsnotify watcher so uncommitted edits trigger a reactive
				// reindex, and enqueue one immediate reindex of its ref tier.
				// scheduler.Enqueue captures the worktree's checked-out ref via
				// RefCapture(worktreePath), so the graph lands in the correct
				// per-ref dir keyed by the worktree path (multi-ref model).
				wtWatcher.OnActivate = func(child *worktree.WorktreeChild) {
					if _, aerr := watcher.AddRepo(child.Path); aerr != nil {
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
				defer wtCancel()
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
			deadRefSweeper := NewDeadRefSweeper(DeadRefConfig{
				TrackedRepos:   trackedRepos,
				LiveRefs:       LiveGitRefs,
				PrimaryRef:     PrimaryGitRef,
				RefsDirForRepo: RefsDirForRepo,
				DropReader:     cfg.DeadRefDropReader,
				Tier:           cfg.DeadRefTier,
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
				DeadRefs:      deadRefSweeper,
				OrphanRoots:   orphanRootSweeper,
				Logger:        logger,
			})
			reaperStop := make(chan struct{})
			reaper.Start(reaperStop)
			defer close(reaperStop)
			logger.Info("reaper: vanished-repo store GC started", "interval", "5m")
		}
	}

	// Pattern confidence time-decay scheduler — runs every 6 hours (or a
	// caller-supplied interval for tests). Requires PatternGroupDirs to be
	// non-nil; skipped gracefully when the caller has not provided it.
	if cfg.PatternGroupDirs != nil {
		decayInterval := cfg.PatternDecayInterval
		if decayInterval <= 0 {
			decayInterval = 6 * time.Hour
		}
		decayJob := buildPatternDecayJob(cfg.PatternGroupDirs, logger)
		decaySched := agentpatterns.NewDecayScheduler(decayInterval, decayJob)
		decayCtx, decayCancel := context.WithCancel(ctx)
		go decaySched.Run(decayCtx)
		defer decayCancel()
		logger.Info("pattern decay scheduler started", "interval", decayInterval.String())
	}

	// Docgen background sweeper (issue #2216): removes stale staging runs and
	// .previous-* backups every 24 h. Opt-in: nil = disabled (--no-auto-cleanup).
	if cfg.DocgenSweep != nil {
		sweepCfg := *cfg.DocgenSweep
		sweepCfg.Logger = logger
		sweepStop := make(chan struct{})
		StartDocgenSweeper(sweepCfg, sweepStop)
		defer close(sweepStop)
		interval := sweepCfg.Interval
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		logger.Info("docgen sweeper started", "interval", interval.String())
	}

	// Dashboard HTTP server — started in a goroutine so it does not
	// block the RPC socket. Shuts down when the daemon context is done.
	// The DashboardServe hook is injected from cmd/grafel to avoid
	// the import cycle that would arise from importing internal/dashboard here.
	// DashboardPort==0 means "OS-pick a free port at bind time" (#5224);
	// only a NEGATIVE port disables the dashboard.
	if cfg.DashboardServe != nil && cfg.DashboardPort >= 0 {
		bind := cfg.DashboardBind
		if bind == "" {
			bind = "127.0.0.1"
		}
		dashCtx, dashCancel := context.WithCancel(ctx)
		defer dashCancel()
		go func() {
			if err := cfg.DashboardServe(dashCtx, bind, cfg.DashboardPort, logger, cfg.OnDashboardListen); err != nil {
				logger.Error("dashboard", "err", err)
				if cfg.OnDashboardError != nil {
					cfg.OnDashboardError(err)
				}
			}
		}()
		if cfg.DashboardPort > 0 {
			logger.Info("dashboard listening", "url", "http://"+bind+":"+fmt.Sprintf("%d", cfg.DashboardPort)+"/")
		} else {
			logger.Info("dashboard listening", "bind", bind, "port", "os-assigned")
		}
	}

	server := rpc.NewServer()
	if err := server.RegisterName(proto.ServiceName, svc); err != nil {
		return fmt.Errorf("register %s: %w", proto.ServiceName, err)
	}

	// Signals — we want SIGTERM (systemd, launchd's stop) and SIGINT
	// (Ctrl-C when running in the foreground for development).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	logger.Info("ready", "socket", cfg.Layout.SocketPath, "pid", os.Getpid())

	// Track accepted connections so we can wait for them to drain on
	// shutdown. The waitgroup is decremented when each conn loop returns.
	var connWG sync.WaitGroup
	acceptDone := make(chan struct{})
	go acceptLoop(listener, server, &connWG, logger, acceptDone)

	// Wait for any shutdown trigger.
	select {
	case <-stopReq:
		logger.Info("stop requested via RPC")
	case sig := <-sigCh:
		logger.Info("signal received", "signal", sig.String())
	case <-ctx.Done():
		logger.Info("context cancelled", "err", ctx.Err())
	case <-acceptDone:
		// acceptLoop only returns when the listener closes, which we
		// don't do until shutdown — but if the listener dies on its
		// own we should treat that as fatal and exit.
		logger.Error("listener closed unexpectedly")
		return errors.New("listener closed")
	}

	// Cleanup hook: best-effort shutdown operations (e.g. metric flush).
	// Does not block the shutdown path (issue #2530).
	if cfg.ShutdownCleanup != nil {
		cfg.ShutdownCleanup()
	}

	// Stop accepting new connections, then wait for in-flight ones.
	_ = listener.Close()
	<-acceptDone
	connWG.Wait()
	logger.Info("graceful shutdown complete")
	return nil
}

// acceptLoop pulls connections off the listener and hands each to
// jsonrpc.ServeConn under the registered server. The waitgroup tracks
// each conn so Run can join them on shutdown.
func acceptLoop(l net.Listener, srv *rpc.Server, wg *sync.WaitGroup, logger *slog.Logger, done chan<- struct{}) {
	defer close(done)
	for {
		conn, err := l.Accept()
		if err != nil {
			// Listener closed during shutdown — that's the happy path.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			logger.Error("accept", "err", err)
			return
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer c.Close()
			srv.ServeCodec(jsonrpc.NewServerCodec(&loggingConn{Conn: c, log: logger}))
		}(conn)
	}
}

// loggingConn wraps a net.Conn so EOF / read errors get a single log
// line. Without this, jsonrpc swallows the close silently and we have
// no way to confirm clients are actually disconnecting on demand.
type loggingConn struct {
	net.Conn
	log *slog.Logger
}

func (c *loggingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil && err != io.EOF {
		// EOF is the normal client disconnect; anything else is worth
		// noting. We don't return the wrapper here, so jsonrpc still
		// sees the original error.
		c.log.Error("conn read", "err", err)
	}
	return n, err
}

// buildPatternDecayJob constructs the DecayJob that the pattern decay scheduler
// calls on each tick (every 6 hours by default).
//
// The job performs two passes:
//
//  1. Confidence decay (per ADR-0018 + γ spec): for each pattern with
//     last_applied > 30 days ago AND confidence > 0.2, decrement by
//     DecayDeltaPer30Day (0.05) per tick, floored at ConfidenceFloor (0.2).
//  2. Candidate pruning (per ADR-0018 δ spec): for each candidate
//     (is_candidate=true) with last_validated older than the group's
//     `candidate_decay_days` (loaded from patterns-config.json; default
//     90), drop it from the store.
//
// The decay step and the candidate-pruning step share a single load+save
// cycle so the store mutates atomically.
func buildPatternDecayJob(groupDirs func() map[string]string, logger *slog.Logger) agentpatterns.DecayJob {
	return func(nowUnix int64) {
		dirs := groupDirs()
		for group, dir := range dirs {
			if dir == "" {
				continue
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				if logger != nil {
					logger.Error("pattern decay: load", "group", group, "err", err)
				}
				continue
			}
			cfg, cfgErr := agentpatterns.LoadConfig(dir)
			if cfgErr != nil && logger != nil {
				logger.Warn("pattern decay: load config (using defaults)", "group", group, "err", cfgErr)
				cfg = agentpatterns.DefaultConfig()
			}
			changed := false

			// Pass 1 — confidence decay.
			for i := range patterns {
				p := &patterns[i]
				if p.LastApplied == 0 {
					continue // never applied — skip decay
				}
				daysSince := float64(nowUnix-p.LastApplied) / 86400.0
				if daysSince <= 30 {
					continue // within the 30-day grace window
				}
				if p.Confidence <= agentpatterns.ConfidenceFloor {
					continue // already at floor
				}
				// Flat decrement per scheduler tick (not proportional to days).
				before := p.Confidence
				newConf := p.Confidence - agentpatterns.DecayDeltaPer30Day
				if newConf < agentpatterns.ConfidenceFloor {
					newConf = agentpatterns.ConfidenceFloor
				}
				p.Confidence = newConf
				if p.Confidence != before {
					changed = true
				}
			}

			// Pass 2 — candidate pruning. Operates only on patterns
			// with is_candidate=true and last_validated older than
			// the configured cutoff. Approved patterns are never
			// auto-pruned (per ADR-0018 Open Question 1).
			if cfg.CandidateDecayDays > 0 {
				cutoff := nowUnix - int64(cfg.CandidateDecayDays)*86400
				kept := patterns[:0]
				pruned := 0
				for _, p := range patterns {
					if p.IsCandidate && p.LastValidated > 0 && p.LastValidated < cutoff {
						pruned++
						continue
					}
					kept = append(kept, p)
				}
				if pruned > 0 {
					patterns = kept
					changed = true
					if logger != nil {
						logger.Info("pattern decay: pruned stale candidates", "count", pruned, "group", group)
					}
				}
			}

			if !changed {
				continue
			}
			if err := agentpatterns.Save(dir, patterns); err != nil {
				if logger != nil {
					logger.Error("pattern decay: save", "group", group, "err", err)
				}
			}
		}
	}
}

// buildSlogLogger constructs a *slog.Logger whose handler is selected by the
// GRAFEL_DAEMON_LOG_JSON env var:
//   - "1" or "true" → slog.NewJSONHandler (structured JSON lines, compatible
//     with log shippers)
//   - anything else → slog.NewTextHandler (human-readable logfmt)
//
// Handler selection at construction time eliminates the prefix-corruption
// failure mode that required the startup guard removed in #2375 — slog cannot
// be misconfigured this way.
func buildSlogLogger(w io.Writer) *slog.Logger {
	v := strings.TrimSpace(os.Getenv(EnvDaemonLogJSON))
	if v == "1" || strings.EqualFold(v, "true") {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}
