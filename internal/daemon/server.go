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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cajasmota/grafel/internal/agentpatterns"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/daemon/transport"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/daemon/worktree"
	"github.com/cajasmota/grafel/internal/extractor"
)

// defaultActivateConcurrency bounds how many worktree working-tree fsnotify
// subscriptions (watcher.AddRepo) may be opening at once (#5675). Kept small:
// activations are normally dispatched one-at-a-time by the reconciliation poll,
// so this only ever engages under an abnormal burst.
const defaultActivateConcurrency = 8

// worktreeActivateConcurrency resolves the OnActivate fan-out bound. It honours
// GRAFEL_WORKTREE_ACTIVATE_CONCURRENCY (strictly-positive integer) and falls
// back to defaultActivateConcurrency otherwise.
func worktreeActivateConcurrency() int {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_WORKTREE_ACTIVATE_CONCURRENCY")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return defaultActivateConcurrency
}

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
	// SchedulerGroupAlgo runs the group-scope algorithm pass over a group's
	// assembled union and writes the <group>-algo.json overlay (#5349 A3). The
	// scheduler chains it off the link-pass success path; it replaces the old
	// per-repo SchedulerAlgo.
	SchedulerGroupAlgo func(ctx context.Context, group string) error

	// SchedulerStaleGroups, when non-nil, powers the periodic overlay-freshness
	// sweep (#5403): it returns the groups whose on-disk group-algo overlay has
	// gone STALE relative to the current per-repo graph.fb mtimes, so the
	// scheduler can proactively re-arm a (debounced + CPU-capped) group-algo pass
	// for a SETTLED group that would otherwise keep serving a stale overlay until
	// its next reindex. It MUST exclude groups with no overlay yet (those take
	// the normal first-compute link-pass chain). nil disables the sweep.
	SchedulerStaleGroups func() []string

	// SchedulerIncremental, when non-nil, is wired as the S3 incremental
	// file-level reindex hook (issue #2153). It is attempted before
	// SchedulerIndex when the incremental toggle is active. When nil
	// the incremental path is never tried (default: full reindex always).
	SchedulerIncremental func(ctx context.Context, repo string, ref string) sched.IncrementalResult

	// SchedulerEntityCount, when non-nil, returns the entity count of the
	// materialized graph for (repo, ref). Wired into the scheduler so the
	// "indexer: completed" log carries entities=N, making a silent 0-entity
	// completion visible (#5710 follow-up). nil → entities omitted.
	SchedulerEntityCount func(repo string, ref string) int

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

	// OnSchedulerReady, when non-nil, is invoked once the scheduler is started
	// with a read-only warming-state accessor closing over the live scheduler
	// (#5690). cmd/grafel wires the accessor into the MCP server's State so
	// grafel_whoami / grafel_status can report whether a post-index enrichment
	// pass is still in flight (a "warming" group) rather than leaving agents to
	// mistake enrichment-induced slowness for a slow query. The accessor is
	// read-only and has NO effect on scheduling. Not called for a watcher-less
	// daemon (no scheduler is created).
	OnSchedulerReady func(warming func() WarmingSnapshot)
}

// SplitModeEnvVar names the capability flag that gates the serve/engine
// process split (ADR-0024, epic #5729). As of PR6 (epic #5729) the split is
// ON BY DEFAULT: RunServe spawns and supervises a separate `grafel engine`
// child process for the scheduler/watcher/extraction/fbwriter, while serve
// keeps the MCP dispatch socket, dashboard, and graph_cache mmap reads
// in-process. This is the escape hatch: set GRAFEL_SPLIT_MODE=0 (or
// "false"/"off"/"no", case-insensitive) to force single-process monolith
// mode — the entire daemon (serve + engine plane) runs in one process, byte-
// for-byte identical to the pre-split `grafel daemon`, with no engine child
// spawned.
const SplitModeEnvVar = "GRAFEL_SPLIT_MODE"

// SplitModeEnabled reports whether the serve/engine process-split
// capability flag is turned on. See SplitModeEnvVar.
//
// Defaults to true (split ON) as of PR6/epic #5729: unset, empty, or any
// value NOT recognized as an explicit disable is treated as split-mode ON.
// It returns false ONLY when GRAFEL_SPLIT_MODE is explicitly set to one of
// "0", "false", "off", or "no" (case-insensitive, whitespace-trimmed) — the
// documented escape hatch back to single-process monolith mode.
func SplitModeEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(SplitModeEnvVar)))
	switch v {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// ServeConfig is the serve-plane configuration (ADR-0024 Phase 1): the MCP
// dispatch socket, the dashboard, and graph_cache mmap reads. It currently
// composes the entire Config rather than a disjoint field set because, when
// split mode is explicitly disabled (GRAFEL_SPLIT_MODE=0, the escape hatch;
// see SplitModeEnabled), RunServe must start the engine plane IN-PROCESS
// exactly as Run does today — it needs every engine-plane field (scheduler,
// watcher, extraction, fbwriter hooks) to do so.
type ServeConfig struct {
	Config
}

// EngineConfig is the engine-plane configuration (ADR-0024 Phase 1): the
// scheduler, file watcher, extraction, and fbwriter. See ServeConfig's doc
// for why it currently composes the full Config rather than a disjoint
// subset.
type EngineConfig struct {
	Config
}

// daemonPlaneMode selects which planes the shared run() body starts. It is an
// internal (unexported) implementation detail of the ADR-0024 carve; the
// public entrypoints (Run, RunServe, RunEngine) each pick a mode.
type daemonPlaneMode int

const (
	// planeMonolith is the single-process daemon: the serve plane (socket,
	// dashboard, MCP dispatch) AND the engine plane (scheduler, watcher,
	// fbwriter) both run in ONE process. This is the escape-hatch mode
	// (GRAFEL_SPLIT_MODE=0/false/off/no) and is byte-for-byte behavior-
	// identical to the pre-split daemon.
	planeMonolith daemonPlaneMode = iota
	// planeServeOnly starts ONLY the serve plane; the engine plane is skipped
	// because a supervised `grafel engine` child runs it in a separate process
	// (split-mode ON, the default as of PR6/epic #5729). Used by RunServe
	// when SplitModeEnabled().
	planeServeOnly
)

// RunServe starts the serve plane: the MCP dispatch socket, the dashboard, and
// the zero-copy graph_cache mmap reads.
//
// As of PR6 (epic #5729), split mode is ON BY DEFAULT (SplitModeEnabled()==
// true): RunServe starts ONLY the serve plane in-process and spawns +
// supervises a separate `grafel engine` child for the
// scheduler/watcher/extraction/fbwriter. The supervisor keeps the engine
// child alive across crashes with exponential backoff, exposes a status-file
// health gate, and gracefully drains (SIGTERM → bounded wait → SIGKILL,
// reaped) the child when serve shuts down. serve never exits for a
// degraded/dead engine — it keeps answering reads from the last-good
// graph.fb — and returns non-zero only when the engine is unkeepable
// (repeated crash-loop at the backoff ceiling), so the OS unit recycles the
// whole thing.
//
// The escape hatch: set GRAFEL_SPLIT_MODE=0 (or "false"/"off"/"no") to force
// SplitModeEnabled()==false, in which case RunServe runs the ENTIRE daemon in
// one process — byte-for-byte identical to Run — so an existing OS unit that
// execs `serve` (or the back-compat `daemon` shim) behaves exactly like the
// pre-split daemon, with no engine child spawned.
func RunServe(ctx context.Context, cfg ServeConfig) error {
	if !SplitModeEnabled() {
		// Escape hatch (GRAFEL_SPLIT_MODE=0/false/off/no): everything in one
		// process, exactly as before.
		return run(ctx, cfg.Config, planeMonolith)
	}

	// Flag ON: serve plane in-process + a supervised engine child process.
	logger := cfg.Logger
	if logger == nil {
		logger = buildSlogLogger(os.Stderr)
	}

	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sup := newEngineSupervisor(cfg.Layout, logger)
	if err := sup.start(serveCtx); err != nil {
		return fmt.Errorf("serve: start engine supervisor: %w", err)
	}
	// Graceful drain of the engine child on serve shutdown (reaped, no orphan).
	defer sup.stop()

	// Bridge a supervisor "engine unkeepable" fatal into serve shutdown: cancel
	// serveCtx so run() unwinds, then RunServe returns the fatal below.
	go func() {
		select {
		case <-serveCtx.Done():
		case <-sup.fatal():
			logger.Error("serve: engine child unkeepable — shutting down serve so the OS unit recycles it")
			cancel()
		}
	}()

	err := run(serveCtx, cfg.Config, planeServeOnly)
	if ferr := sup.fatalError(); ferr != nil {
		return ferr
	}
	return err
}

// RunEngine starts the engine plane standalone (the split-mode `grafel engine`
// child): the scheduler, file watcher, git-HEAD poller, worktree discovery,
// reapers/sweepers, the status writer, pattern-decay, and the docgen sweeper —
// with NO MCP socket, NO dashboard, and NO MCP dispatch (those are the serve
// plane, owned by the supervising serve process).
//
// It writes its own engine.pid, publishes an engine-global liveness heartbeat
// (statusfile keyed on the daemon root) so the supervising serve's health gate
// can tell HEALTHY from DEGRADED, brings up the engine plane, then blocks until
// ctx is cancelled or a SIGTERM/SIGINT arrives — at which point its defers
// unwind the scheduler/watcher gracefully (ADR-0024, epic #5729, PR2).
func RunEngine(ctx context.Context, cfg EngineConfig) error {
	logger := cfg.Logger
	if logger == nil {
		logger = buildSlogLogger(os.Stderr)
	}

	// #3648: match Run's conservative Go soft memory limit — the engine is the
	// heavy plane (extraction/reindex/fbwriter), exactly the workload the cap
	// bounds.
	applyMemoryLimit(logger)

	if err := EnsureLayout(cfg.Layout); err != nil {
		return fmt.Errorf("engine: ensure layout: %w", err)
	}

	// Engine is the write plane, so it owns the one-time state-migration that
	// normalises the on-disk store layout (mirrors Run). Non-fatal.
	if storeDir := StoreDir(); storeDir != "" {
		if err := MigrateToRefStore(storeDir); err != nil {
			logger.Warn("engine startup: MigrateToRefStore (non-fatal)", "err", err)
		}
	}

	// Engine pidfile (engine.pid) — distinct from serve's daemon.pid so both
	// planes coexist under one daemon root without contending for a pidfile.
	pidPath := EnginePIDPath(cfg.Layout.Root)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return fmt.Errorf("engine: write pid file %s: %w", pidPath, err)
	}
	defer func() { _ = os.Remove(pidPath) }()

	// Engine-global liveness heartbeat: a statusfile keyed on the daemon root
	// (NOT any single repo) that stamps EnginePID + a fresh HeartbeatAt every
	// tick, plus the engine-global busy/parsing/concurrency/warming fields
	// (#5729 PR3). This is the signal serve's supervisor health gate reads to
	// decide HEALTHY vs DEGRADED — independent of whether any fleet repo is
	// registered yet. #5729 PR3 moved the startEngineLivenessHeartbeat call
	// into startEnginePlane (engineplane.go) itself so it ALSO runs in the
	// monolith (escape-hatch GRAFEL_SPLIT_MODE=0), giving serve one status-file code path
	// that behaves identically in both modes — see startEnginePlane.

	// Parent-death watchdog (ADR-0024 orphan-engine hardening, epic #5729,
	// PRIMARY layer): record the parent pid we were started under, then poll
	// for reparenting (the original serve parent died uncleanly — SIGKILL,
	// crash, OOM — and this process was adopted by init) so the engine
	// self-terminates GRACEFULLY instead of lingering as an orphan that keeps
	// writing the store alongside a freshly spawned replacement engine. See
	// startParentDeathWatchdog's doc for the full design and per-platform
	// notes (engine_parentwatch.go / _unix.go / _windows.go).
	//
	// engineCtx derives from ctx so a normal shutdown (ctx cancelled, or the
	// SIGTERM/SIGINT case below) ALSO stops the watchdog goroutine (no leak);
	// the watchdog itself cancels engineCtx on reparenting, which the select
	// below observes as a normal graceful-shutdown trigger.
	originalParent := os.Getppid()
	engineCtx, cancelEngine := context.WithCancel(ctx)
	defer cancelEngine()
	watchdogDone := startParentDeathWatchdog(engineCtx, originalParent, parentWatchGetppid(), defaultParentWatchInterval, cancelEngine, logger)
	defer func() { <-watchdogDone }()

	// Bring up the engine plane (no *Service — engine has no MCP surface).
	// Uses engineCtx (not ctx) so the watchdog's self-cancel also propagates
	// into the scheduler/watcher/etc., unwinding them the same way a real
	// SIGTERM would.
	ep := startEnginePlane(engineCtx, cfg.Config, nil, logger)
	defer ep.shutdown()

	logger.Info("engine: ready", "pid", os.Getpid(), "ppid", originalParent, "root", cfg.Layout.Root)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case <-engineCtx.Done():
		logger.Info("engine: context cancelled — shutting down", "err", engineCtx.Err())
	case sig := <-sigCh:
		logger.Info("engine: signal received — shutting down", "signal", sig.String())
	}
	// Explicitly cancel (idempotent alongside the deferred cancelEngine())
	// BEFORE the deferred ep.shutdown()/watchdogDone-wait unwind below: defers
	// run LIFO, so without this explicit call ep.shutdown() would fire before
	// cancelEngine() on the SIGTERM/SIGINT path, leaving engineCtx (and any
	// child contexts derived from it) live during teardown.
	cancelEngine()
	return nil
}

// Run starts the daemon. It blocks until either:
//   - the Service receives Stop,
//   - the process receives SIGTERM/SIGINT, or
//   - the listener errors fatally.
//
// On exit it removes the socket file and pid file. The function is the
// daemon's entire public surface — cmd/grafel just imports daemon
// and calls Run.
//
// Run is the single-process monolith: it delegates to the shared run() body in
// planeMonolith mode (serve plane + engine plane in one process). This is
// byte-for-byte behavior-identical to the pre-ADR-0024 daemon.
func Run(ctx context.Context, cfg Config) error {
	return run(ctx, cfg, planeMonolith)
}

// run is the shared daemon body behind Run / RunServe / RunEngine. The plane
// argument selects which planes start: planeMonolith runs everything (Run's
// classic behavior), planeServeOnly skips the engine plane (split-mode serve,
// whose engine runs in a supervised child). The engine-plane assembly itself
// lives in startEnginePlane (engineplane.go).
func run(ctx context.Context, cfg Config, plane daemonPlaneMode) error {
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
	// #5330: log BEFORE the startup state migration. canonicalizePath (called
	// transitively from MigrateToRefStore and the per-repo store-root mapping)
	// does a per-segment os.ReadDir that, on a stuck FS, used to deadlock
	// startup before ANY log line was emitted — the hang was only visible via a
	// SIGQUIT goroutine dump. This line makes a wedge here diagnosable; a slow
	// canonicalize now also logs a WARN with the offending path.
	logger.Info("startup: state-migration begin")
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
	logger.Info("startup: state-migration done")

	// #5264: unconditional INFO-level startup tracing between
	// `MigrateToRefStore complete` and the dashboard goroutine launch. On the
	// next Windows CI run the LAST `begin` with no matching `done` pinpoints the
	// wedged startup step (the isolated selftest daemon hangs here). Cheap +
	// helps all platforms; does NOT change startup order/behavior.
	logger.Info("startup: pidfile-acquire begin")
	releasePID, err := AcquirePIDFile(cfg.Layout.PIDPath, cfg.Layout.SocketPath)
	if err != nil {
		return err
	}
	defer releasePID()
	logger.Info("startup: pidfile-acquire done")

	// Remove any stale socket file from a previous crash (Unix only; on
	// Windows named pipes are not filesystem objects and os.Remove is a no-op).
	_ = os.Remove(cfg.Layout.SocketPath)

	logger.Info("startup: socket-listen begin", "socket", cfg.Layout.SocketPath)
	listener, err := transport.Listen(cfg.Layout.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Layout.SocketPath, err)
	}
	logger.Info("startup: socket-listen done")
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

	logger.Info("startup: service-init begin")
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

	logger.Info("startup: service-init done")

	// Layer 2 self-defense: start CPU watchdog for ephemeral /tmp daemons.
	// The watchdog passes the service's real inFlight counter so it can
	// distinguish hot-loops (no work) from legitimate sustained indexing.
	logger.Info("startup: cpu-watchdog begin")
	StartCPUWatchdog(&svc.inFlight, logger)
	logger.Info("startup: cpu-watchdog done")

	// ADR-0024 Phase 1 / PR2 (epic #5729): the engine plane — scheduler,
	// watcher, git-HEAD poller, worktree discovery, reapers/sweepers, the
	// status writer, pattern-decay, and the docgen sweeper. In split-mode
	// serve (planeServeOnly) a separate supervised `grafel engine` child runs
	// this, so we skip it here; in the monolith (escape-hatch GRAFEL_SPLIT_MODE=0) and the
	// standalone engine it runs in-process. The assembly lives in
	// startEnginePlane (engineplane.go); the extraction is behavior-preserving
	// for the monolith (same constructors, same order, LIFO teardown).
	if plane != planeServeOnly {
		ep := startEnginePlane(ctx, cfg, svc, logger)
		defer ep.shutdown()
	}

	// Dashboard HTTP server — started in a goroutine so it does not
	// block the RPC socket. Shuts down when the daemon context is done.
	// The DashboardServe hook is injected from cmd/grafel to avoid
	// the import cycle that would arise from importing internal/dashboard here.
	// DashboardPort==0 means "OS-pick a free port at bind time" (#5224);
	// only a NEGATIVE port disables the dashboard.
	logger.Info("startup: dashboard-launch begin",
		"serve_hook", cfg.DashboardServe != nil, "port", cfg.DashboardPort)
	if cfg.DashboardServe != nil && cfg.DashboardPort >= 0 {
		bind := cfg.DashboardBind
		if bind == "" {
			bind = "127.0.0.1"
		}
		dashCtx, dashCancel := context.WithCancel(ctx)
		defer dashCancel()
		go func() {
			// #5264: bracket the actual DashboardServe call (which does the
			// net.Listen + heavy wiring) so a hang inside the goroutine is
			// distinguishable from the goroutine never being scheduled.
			logger.Info("startup: dashboard-serve-goroutine begin")
			if err := cfg.DashboardServe(dashCtx, bind, cfg.DashboardPort, logger, cfg.OnDashboardListen); err != nil {
				logger.Error("dashboard", "err", err)
				if cfg.OnDashboardError != nil {
					cfg.OnDashboardError(err)
				}
			}
			logger.Info("startup: dashboard-serve-goroutine done")
		}()
		if cfg.DashboardPort > 0 {
			logger.Info("dashboard listening", "url", "http://"+bind+":"+fmt.Sprintf("%d", cfg.DashboardPort)+"/")
		} else {
			logger.Info("dashboard listening", "bind", bind, "port", "os-assigned")
		}
	}
	logger.Info("startup: dashboard-launch done")

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

	// #5710: hard-exit watchdog. mcpDrainTimeout below bounds ONLY the MCP
	// drain — it has no effect on connWG.Wait() near the end of this
	// function, which joins every accepted connection including ones blocked
	// inside a non-MCP RPC handler (e.g. Service.Rebuild, whose own timeout
	// is rebuildRPCTimeout = 2h and which never observes shutdown). Without a
	// backstop, a single stalled Rebuild call wedges Run() forever: it never
	// returns, the deferred releasePID() never runs, and the pidfile is left
	// pointing at a process that is alive but can never again serve a
	// request. watchdogCtx bounds the ENTIRE graceful tail from here to the
	// `return nil` below (not just connWG.Wait()) so it also covers a slow
	// MCP drain; if it fires before the tail completes, force-exit rather
	// than hang past launchd/systemd's SIGTERM→SIGKILL grace window.
	watchdogCtx, watchdogCancel := context.WithTimeout(context.Background(), shutdownWatchdogTimeout())
	defer watchdogCancel()

	// #5633: graceful MCP drain. Mark the service as draining so any MCP RPC
	// that arrives from here on gets a clean retryable error (the bridge
	// reconnects to the replacement daemon) instead of being half-served and
	// then killed when the socket closes. Then wait — bounded — for already
	// in-flight MCP calls to finish so a clean restart does not drop them with
	// "connection is shut down to the client". The bound keeps a wedged handler
	// from blocking launchd/systemd stop indefinitely; whatever is still
	// running past the deadline is the bridge's retry-on-shutdown to mop up.
	svc.beginDrain()
	if drained := svc.waitDrain(mcpDrainTimeout); drained {
		logger.Info("graceful shutdown: in-flight MCP calls drained")
	} else {
		logger.Warn("graceful shutdown: MCP drain timed out — closing socket with calls still in flight",
			"timeout", mcpDrainTimeout)
	}

	// Cleanup hook: best-effort shutdown operations (e.g. metric flush).
	// Does not block the shutdown path (issue #2530).
	if cfg.ShutdownCleanup != nil {
		cfg.ShutdownCleanup()
	}

	// Stop accepting new connections, then wait for in-flight ones — bounded
	// by watchdogCtx (#5710). This is exactly the step that used to hang
	// forever behind a stalled Rebuild RPC: connWG.Wait() blocks until every
	// accepted connection's handler returns, and Rebuild has no shutdown/
	// ctx.Done case in its own select.
	_ = listener.Close()
	<-acceptDone
	connDone := make(chan struct{})
	go func() {
		connWG.Wait()
		close(connDone)
	}()
	select {
	case <-connDone:
		logger.Info("graceful shutdown complete")
		return nil
	case <-watchdogCtx.Done():
		// os.Exit skips every deferred cleanup in this function (releasePID,
		// socket unlink), so we repeat the pidfile/socket removal explicitly
		// before exiting — otherwise the force-killed process would leave
		// the exact stale-pidfile wedge this change exists to prevent.
		logger.Error("force-exit: graceful shutdown exceeded watchdog timeout",
			"timeout", shutdownWatchdogTimeout())
		releasePID()
		_ = os.Remove(cfg.Layout.SocketPath)
		osExit(1)
		// Reached only when osExit has been overridden (tests): production
		// os.Exit never returns, so this line only runs when a test stub
		// swapped osExit for a no-op — proving Run() unblocks from the
		// stalled connWG.Wait() instead of hanging forever (#5710).
		return errors.New("graceful shutdown watchdog exceeded; force-exited")
	}
}

// osExit is os.Exit, indirected through a package variable so the #5710
// hard-exit watchdog is testable in-process: production always force-exits
// the whole process, but tests substitute a no-op and assert Run() still
// returns (via the fallback return just below the osExit call) instead of
// hanging on connWG.Wait() forever — without killing the test binary itself.
var osExit = os.Exit

// SetShutdownExitFuncForTest overrides the #5710 hard-exit watchdog's exit
// function for the duration of a test and returns a restore closure. Tests
// live in package daemon_test (external), so this exported hook is the only
// way to reach the unexported osExit var without creating an import cycle
// (internal/daemon/client, needed to drive the daemon from a test, itself
// imports package daemon).
//
// Production code must never call this. The suffix is dead-stripped by
// nothing at build time — it is an ordinary exported function — but the name
// mirrors this repo's established ForTest convention (see internal/docgen)
// for hooks that exist solely to let tests reach otherwise-unexported state.
func SetShutdownExitFuncForTest(f func(int)) (restore func()) {
	prev := osExit
	osExit = f
	return func() { osExit = prev }
}

// mcpDrainTimeout bounds how long graceful shutdown waits for in-flight MCP
// RPCs to finish before closing the socket (#5633). It is generous enough for
// a typical graph query to complete (most return in well under a second) yet
// short enough to stay inside the host service manager's stop grace period
// (launchd's default SIGTERM→SIGKILL window and systemd's TimeoutStopSec are
// both ~5 s+; we deliberately stay under that).
const mcpDrainTimeout = 3 * time.Second

// defaultShutdownWatchdog bounds the ENTIRE graceful-shutdown tail — from the
// moment a shutdown trigger fires to Run() returning — not just the MCP
// drain. #5710: a stalled Service.Rebuild RPC holds its connection open
// indefinitely (rebuildRPCTimeout is 2h and has no shutdown/ctx.Done case),
// so connWG.Wait() can block forever with no MCP-drain-timeout in the world
// that would ever unblock it. 5s keeps the watchdog under launchd's default
// SIGTERM→SIGKILL grace window (and systemd's TimeoutStopSec), matching the
// same target mcpDrainTimeout above already stays under.
const defaultShutdownWatchdog = 5 * time.Second

// shutdownWatchdogEnv overrides defaultShutdownWatchdog with a Go duration
// string (e.g. "200ms"), primarily so tests can exercise the force-exit path
// without a real 5s wait.
const shutdownWatchdogEnv = "GRAFEL_SHUTDOWN_WATCHDOG"

// shutdownWatchdogTimeout resolves the watchdog bound: shutdownWatchdogEnv if
// set to a valid positive duration, else defaultShutdownWatchdog.
func shutdownWatchdogTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv(shutdownWatchdogEnv)); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return defaultShutdownWatchdog
}

// acceptLoop pulls connections off the listener and hands each to
// jsonrpc.ServeConn under the registered server. The waitgroup tracks
// each conn so Run can join them on shutdown.
func acceptLoop(l net.Listener, srv *rpc.Server, wg *sync.WaitGroup, logger *slog.Logger, done chan<- struct{}) {
	defer close(done)
	// Exponential backoff bounds for transient Accept() errors, mirroring
	// the pattern in net/http.Server.Serve (see "tempDelay"). A transient
	// failure (e.g. EMFILE under fd pressure) must NOT bring the daemon
	// down: returning here causes Run to unlink the socket and every MCP
	// client drops. So we back off and keep serving instead.
	const (
		backoffStart = 5 * time.Millisecond
		backoffMax   = 1 * time.Second
	)
	var backoff time.Duration
	for {
		conn, err := l.Accept()
		if err != nil {
			// Listener closed during shutdown — that's the happy path.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient error: do not exit. Resource-exhaustion and
			// connection-reset conditions are expected under load and
			// recover on their own. Treating them as fatal removed the
			// socket and caused MCP outages (issue: acceptLoop exits on
			// transient Accept errors). net.Error.Timeout() is the modern
			// stand-in for the deprecated Temporary().
			if isTransientAcceptErr(err) {
				if backoff == 0 {
					backoff = backoffStart
				} else {
					backoff *= 2
				}
				if backoff > backoffMax {
					backoff = backoffMax
				}
				logger.Warn("accept: transient error, backing off",
					"err", err, "retry_in", backoff)
				time.Sleep(backoff)
				continue
			}
			// Genuinely unrecoverable: log and exit. The deferred cleanup
			// in Run will remove the socket. Non-transient Accept errors
			// on a unix listener are rare and indicate the listener itself
			// is unusable.
			logger.Error("accept: fatal", "err", err)
			return
		}
		// Successful Accept — reset the transient-error backoff.
		backoff = 0
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer c.Close()
			srv.ServeCodec(jsonrpc.NewServerCodec(&loggingConn{Conn: c, log: logger}))
		}(conn)
	}
}

// isTransientAcceptErr reports whether an error returned by net.Listener.Accept
// is transient — i.e. the listener is still usable and the loop should back off
// and retry rather than tear the daemon (and its socket) down.
//
// This mirrors net/http.Server.Serve, which retries on any net.Error whose
// (deprecated) Temporary() reports true. We use Timeout() as the modern
// stand-in and additionally recognise the specific syscall errnos that show up
// under resource pressure: EMFILE/ENFILE (fd exhaustion), ENOMEM/ENOBUFS
// (memory pressure), and ECONNABORTED (peer reset between the SYN and accept).
func isTransientAcceptErr(err error) bool {
	switch {
	case errors.Is(err, syscall.EMFILE),
		errors.Is(err, syscall.ENFILE),
		errors.Is(err, syscall.ENOMEM),
		errors.Is(err, syscall.ENOBUFS),
		errors.Is(err, syscall.ECONNABORTED):
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
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
