package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cajasmota/grafel/internal/agents"
	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/caps"
	"github.com/cajasmota/grafel/internal/daemon/extract"
	"github.com/cajasmota/grafel/internal/daemon/mode"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/daemon/worktree"
	"github.com/cajasmota/grafel/internal/dashboard"
	"github.com/cajasmota/grafel/internal/docgen"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/jobs"
	"github.com/cajasmota/grafel/internal/mcp"
	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/quality"
	"github.com/cajasmota/grafel/internal/quality/audit"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/resolve"
)

// daemonProgressBroker is the process-wide indexer progress bus. The Rebuild
// path publishes granular per-repo progress.Event records into it (via the
// indexer's WithPublisher option) and the dashboard's /api/index-progress SSE
// endpoints subscribe to it, so the WebUI Index step renders live per-repo /
// per-module rows with file counters instead of a generic bar (#1531). It is
// created once in runDaemon before the RPC + dashboard servers start.
var daemonProgressBroker = progress.NewBroker()

// daemonActivityBroker is the process-wide MCP activity broker, captured at
// dashboard wiring time so the graceful-stop path (ShutdownCleanup) can flush
// and close its disk log handle (~/.grafel/mcp-activity.jsonl) before anything
// removes the home dir. On Windows an open handle blocks unlink, which is what
// made the isolated selftest teardown fail (#5264). Guarded by
// daemonActivityBrokerMu.
var (
	daemonActivityBroker   *mcp.MCPActivityBroker
	daemonActivityBrokerMu sync.Mutex
)

// setDaemonActivityBroker records the process-wide activity broker so the
// shutdown path can close its disk log.
func setDaemonActivityBroker(b *mcp.MCPActivityBroker) {
	daemonActivityBrokerMu.Lock()
	daemonActivityBroker = b
	daemonActivityBrokerMu.Unlock()
}

// closeDaemonActivityLog flushes and closes the MCP activity disk log handle.
// Idempotent and nil-safe; called from ShutdownCleanup on graceful stop.
func closeDaemonActivityLog() {
	daemonActivityBrokerMu.Lock()
	b := daemonActivityBroker
	daemonActivityBrokerMu.Unlock()
	if b != nil {
		b.CloseLog()
	}
}

// daemonShutdownCleanup is the single graceful-stop cleanup used by BOTH the
// production daemon (runDaemon) and the in-process selftest daemon
// (selftestDaemonConfig). It stops the shared MCP server (flushing session
// metrics, #2530) and flushes + closes the MCP activity disk log handle so its
// file handle is released (#5264).
//
// Owning the activity-log close here — on the cleanup path BOTH daemon
// configurations install — is what makes the close robust regardless of which
// component lazily opened ~/.grafel/mcp-activity.jsonl. The handle is created on
// the first MCP Append (e.g. the selftest's grafel_stats call) via the single
// broker that the dashboard wiring (makeDaemonDashboardServe) registers
// process-wide; the previous fix (#5271) only wired this cleanup into runDaemon,
// so the selftest daemon — which builds its config separately — never closed the
// handle and leaked it, failing the Windows teardown layer. Best-effort and
// idempotent: safe to call on every shutdown.
func daemonShutdownCleanup() {
	if mcpSrv, err := mcpServerInstance(); err == nil {
		mcpSrv.Stop()
	}
	// #5264: flush + close the MCP activity disk log so its file handle is
	// released. On Windows an open handle blocks unlink, which made the
	// isolated selftest teardown (os.RemoveAll of ~/.grafel) fail.
	closeDaemonActivityLog()
}

// defaultDashboardPort is the default TCP port for the embedded dashboard.
const defaultDashboardPort = 47274

// defaultRSSBudgetMB returns the production default for the admission-control
// budget (in MB). It auto-tunes based on available system memory so that
// the daemon's idle RSS (heap inflation after graph load) does not cause the
// scheduler to wedge when the user's repos are large.
//
// Formula: min(2048, systemMemoryMB / 8).  On a 16 GB machine this gives
// 2 GB; on an 8 GB machine 1 GB; on a 4 GB machine 512 MB.  The env var
// GRAFEL_MAX_RSS_BUDGET_MB and the --max-rss-budget flag both override
// the result, so operators can tune down on constrained hardware.
//
// NOTE: this budget is for the ADDITIONAL predicted RSS of concurrently
// running index jobs only — the daemon's idle RSS is never subtracted from
// it (delta-based accounting).  See internal/daemon/sched for the admission
// logic.
func defaultRSSBudgetMB() int64 {
	sysMB := systemTotalMemoryMB()
	if sysMB <= 0 {
		return 500 // safe fallback when sysinfo is unavailable
	}
	budget := sysMB / 8
	const cap = 2048
	if budget > cap {
		budget = cap
	}
	return budget
}

// systemTotalMemoryMB returns total host physical memory in MB via the
// process package's platform-specific sysinfo implementation.
func systemTotalMemoryMB() int64 {
	return process.TotalMemoryMB()
}

// computeRebuildConcurrency applies the auto-tune formula to an explicit
// memory size (in MB). This is the pure, testable core of defaultRebuildConcurrency.
//
// Phase 1 formula (post-#2141 P0.2, streaming FB writes — ~800MB peak per rebuild):
// min(16, sysMB/2048), floored at 2.
//
//   - sysMB ≤ 0 → 2 (sysinfo unavailable)
//   - < 4 GB    → 2 (floor)
//   - 8 GB      → 4
//   - 16 GB     → 8
//   - 32 GB     → 16
//   - ≥ 32 GB   → 16 (ceiling)
//
// Previous formula was min(8, sysMB/4096). The raise is safe because #2141 P0.2
// (streaming FB writes) reduced per-rebuild peak RSS from ~2 GB to ~800 MB,
// so 16 concurrent jobs on 32 GB = ~12.8 GB worst-case — well within headroom.
// See issue #2147 for the full phased evolution plan.
func computeRebuildConcurrency(sysMB int64) int {
	if sysMB <= 0 {
		return 2
	}
	n := int(sysMB / 2048)
	if n < 2 {
		n = 2
	}
	if n > 16 {
		n = 16
	}
	return n
}

// defaultRebuildConcurrency auto-tunes the parallel rebuild cap based on
// available system memory (#2127). Delegates to computeRebuildConcurrency
// with the live system total so the logic is independently testable.
//
// The env var GRAFEL_REBUILD_CONCURRENCY and the --max-concurrent-groups
// flag both override the result.
func defaultRebuildConcurrency() int {
	return computeRebuildConcurrency(systemTotalMemoryMB())
}

// defaultPerRepoRebuildTimeout bounds how long a SINGLE repo's index may run
// inside a group rebuild before it is surfaced as a stalled repo and skipped
// (#5143). Without it, one slow/stuck repo wedges the whole group rebuild for
// the full 2h RPC timeout with no indication of which repo is stuck — the
// reported symptom (35m+ "no result yet", my-service stale). The
// group still serializes repos and returns partial results for the rest.
//
// Generous default so a genuinely large repo isn't killed; tune via
// GRAFEL_REBUILD_REPO_TIMEOUT (Go duration, e.g. "20m"). Zero/negative
// disables the per-repo bound.
const defaultPerRepoRebuildTimeout = 30 * time.Minute

// resolvePerRepoRebuildTimeout returns the effective per-repo timeout, honoring
// GRAFEL_REBUILD_REPO_TIMEOUT. A value of "0" (or any non-positive
// duration) disables the bound and returns 0.
func resolvePerRepoRebuildTimeout() time.Duration {
	if v := os.Getenv("GRAFEL_REBUILD_REPO_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			if d <= 0 {
				return 0
			}
			return d
		}
	}
	return defaultPerRepoRebuildTimeout
}

// resolveEnvRebuildConcurrency reads GRAFEL_REBUILD_CONCURRENCY (then
// GRAFEL_MAX_CONCURRENT_GROUPS as a legacy fallback) and returns the
// effective concurrency value, falling back to the auto-tuned default when
// the env var is absent or invalid. This mirrors the parsing logic in
// runDaemon and is exposed for unit testing.
func resolveEnvRebuildConcurrency() int {
	if v := os.Getenv("GRAFEL_REBUILD_CONCURRENCY"); v != "" {
		if parsed, perr := strconv.Atoi(v); perr == nil && parsed >= 1 {
			return parsed
		}
	}
	if v := os.Getenv("GRAFEL_MAX_CONCURRENT_GROUPS"); v != "" {
		if parsed, perr := strconv.Atoi(v); perr == nil && parsed >= 1 {
			return parsed
		}
	}
	return defaultRebuildConcurrency()
}

// resolveDaemonGOMAXPROCS returns the GOMAXPROCS the daemon process should run
// at, given the host core count and the GRAFEL_DAEMON_GOMAXPROCS env var
// (#5135). It returns 0 when no cap should be applied (env unset/invalid or the
// requested value is >= the host core count, in which case the Go default is
// already correct and we leave it untouched).
//
// This is the NATIVE in-process knob: it bounds the daemon's own Go runtime
// parallelism (in-process extraction/reindex, GC, algorithm passes) WITHOUT
// requiring the generic GOMAXPROCS env var (which is fine, but undocumented and
// easy to confuse with the per-subprocess GRAFEL_EXTRACT_GOMAXPROCS cap).
//
// Tradeoff (documented in docs/settings.md): because query handling shares the
// same process, lowering this also lowers the ceiling on concurrent query
// throughput. It is the right knob when the daemon's OWN in-process indexing
// (GRAFEL_SUBPROC_EXTRACT unset/0) is the CPU source; when the subprocess
// extractor is enabled, prefer GRAFEL_EXTRACT_GOMAXPROCS / _CONCURRENCY,
// which throttle the children without touching query latency.
func resolveDaemonGOMAXPROCS(hostCPU int) int {
	return resolveDaemonGOMAXPROCSWith(hostCPU, 0)
}

// resolveDaemonGOMAXPROCSWith is the #5137 runtime-reloadable form of
// resolveDaemonGOMAXPROCS. fileVal is the cpu.json override (0 = unset). The
// precedence is env (GRAFEL_DAEMON_GOMAXPROCS) > cpu.json > half-cores
// default: env is captured at process start and never changes in a running
// daemon, so the config file is the live-mutable surface the SIGHUP handler
// reads. A requested value at/above the host core count returns 0 ("the Go
// default is already correct, leave it untouched").
//
// Resource-safe default (v0.1.1): when NEITHER env NOR cpu.json pins a value,
// the daemon caps its own in-process Go parallelism at ~half the host cores
// (defaultDaemonGOMAXPROCS). On a fresh `curl|bash` install that sets no env
// vars this bounds the daemon's own GC / algorithm passes / in-process index
// fallback so background work cannot saturate every core — the runaway the
// dogfooding report observed. Query handling shares this process, so half (not
// fewer) keeps MCP latency healthy while leaving headroom for the user's own
// build/test/editor. Operators can raise it (up to the host count, which
// disables the cap) or lower it via GRAFEL_DAEMON_GOMAXPROCS / cpu.json.
func resolveDaemonGOMAXPROCSWith(hostCPU, fileVal int) int {
	n := envPositiveInt2("GRAFEL_DAEMON_GOMAXPROCS")
	if n <= 0 && fileVal > 0 {
		n = fileVal
	}
	if n <= 0 {
		// No explicit cap from env or cpu.json — apply the half-cores default.
		n = defaultDaemonGOMAXPROCS(hostCPU)
	}
	if n <= 0 {
		return 0
	}
	if hostCPU > 0 && n >= hostCPU {
		// Already at/above the Go default — nothing to cap.
		return 0
	}
	return n
}

// defaultDaemonGOMAXPROCS returns the resource-safe default in-process
// GOMAXPROCS for the daemon when the operator has pinned nothing: ~half the
// host cores, floored at 1. Returns 0 when hostCPU is unknown (<=0) so the
// caller leaves the Go default untouched rather than guessing. On a 2-core
// host this resolves to 1; on 12 cores, 6.
func defaultDaemonGOMAXPROCS(hostCPU int) int {
	if hostCPU <= 0 {
		return 0
	}
	n := hostCPU / 2
	if n < 1 {
		n = 1
	}
	return n
}

// applyDaemonGOMAXPROCSFromCaps re-resolves the daemon's in-process GOMAXPROCS
// from (env + cpu.json) and live-applies it via runtime.GOMAXPROCS when it
// differs from the current setting. Returns (newValue, previousValue, changed).
// runtime.GOMAXPROCS(n) is documented as safe to call from a running program,
// so this is the #5137 no-restart live re-apply. A resolved value of 0 means
// "no cap" — we restore the Go default (host core count) so lowering then
// clearing the cap in cpu.json restores full parallelism without a restart.
func applyDaemonGOMAXPROCSFromCaps(store *caps.Store, hostCPU int) (int, int, bool) {
	fileVal := 0
	if store != nil {
		if cfg, err := store.Load(); err == nil {
			fileVal = cfg.DaemonGOMAXPROCSValue()
		}
	}
	target := resolveDaemonGOMAXPROCSWith(hostCPU, fileVal)
	if target <= 0 {
		// No cap requested — ensure we are at the Go default (host cores).
		target = hostCPU
	}
	if target < 1 {
		target = 1
	}
	cur := runtime.GOMAXPROCS(0) // query without changing
	if cur == target {
		return target, cur, false
	}
	prev := runtime.GOMAXPROCS(target)
	return target, prev, true
}

// installCapReloadHandler registers a SIGHUP handler that re-reads cpu.json and
// live-applies the daemon's in-process GOMAXPROCS (#5137). The per-subprocess
// extract caps need no signal — the coordinator re-reads cpu.json on each
// reindex via the installed extract caps Store — but the daemon's OWN GOMAXPROCS
// is applied once at process start, so a signal (or restart) is required to
// change it live. SIGHUP is the conventional "reload config" signal.
//
// The handler runs for the life of the process; the registered channel is never
// closed (daemon teardown is process exit), matching the daemon's other
// long-lived goroutines.
func installCapReloadHandler(store *caps.Store, logf interface{ Printf(string, ...any) }) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			n, prev, changed := applyDaemonGOMAXPROCSFromCaps(store, runtime.NumCPU())
			if changed {
				logf.Printf("cpu-tune: SIGHUP reload — daemon GOMAXPROCS=%d applied (was %d, host=%d)", n, prev, runtime.NumCPU())
			} else {
				logf.Printf("cpu-tune: SIGHUP reload — daemon GOMAXPROCS unchanged (=%d, host=%d)", n, runtime.NumCPU())
			}
		}
	}()
}

// envPositiveInt2 reads a strictly-positive integer from the named env var,
// returning 0 when unset, empty, non-numeric, or <= 0. (Mirrors the helper in
// internal/daemon/extract; duplicated here to avoid an import cycle / exporting
// an internal helper for one call site.)
func envPositiveInt2(name string) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// runDaemon is the long-running mode of the grafel binary. It is
// wired into the CLI as a hidden `grafel daemon` subcommand —
// users normally reach it via `grafel start`, which forks this
// process and detaches.
//
// All extractor + registry + linker work happens here. The CLI's other
// subcommands are thin RPC clients (see internal/daemon/client).
func runDaemon(argv []string) error {
	// Fix root-cause E (#2141): lower the GC trigger from the default 100%
	// heap-growth to 50%. This trades ~5% additional CPU for ~30% lower
	// steady-state heap by collecting unreachable objects twice as often.
	// Only applied when the user has not set GOGC explicitly, so they can
	// opt-out or tune higher if needed.
	gcOverride := os.Getenv("GOGC") != ""
	if !gcOverride {
		debug.SetGCPercent(50)
	}
	// Always log so future heap regressions are diagnosable.
	gcLog := log.New(os.Stderr, "grafel-daemon: ", log.LstdFlags|log.Lmicroseconds)
	gcLog.Printf("gc-tune: GOGC=50 (override=%v)", gcOverride)

	// #5135: native in-process GOMAXPROCS cap. GRAFEL_DAEMON_GOMAXPROCS
	// bounds the daemon's own Go-runtime parallelism (in-process extraction,
	// reindex, GC, algorithm passes) without needing the generic GOMAXPROCS
	// env var. Only applied when set, valid, and below the host core count;
	// otherwise the Go default (= host cores) is left untouched. See
	// docs/settings.md for the query-latency tradeoff.
	if gmp := resolveDaemonGOMAXPROCS(runtime.NumCPU()); gmp > 0 {
		prev := runtime.GOMAXPROCS(gmp)
		gcLog.Printf("cpu-tune: GRAFEL_DAEMON_GOMAXPROCS=%d applied (was %d, host=%d)", gmp, prev, runtime.NumCPU())
	}

	// Parse daemon-only flags. The root cobra command has flag parsing
	// disabled for "daemon" so we own the argv. Unknown flags exit
	// with a clear error rather than being silently ignored.
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	var daemonModeFlag string
	fs.StringVar(&daemonModeFlag, "mode", "",
		"operational mode: background, workstation, readonly (default: read from daemon.config.json)")
	var maxRSSBudget int64
	envBudget := defaultRSSBudgetMB()
	if v := os.Getenv("GRAFEL_MAX_RSS_BUDGET_MB"); v != "" {
		if parsed, perr := strconv.ParseInt(v, 10, 64); perr == nil && parsed >= 0 {
			envBudget = parsed
		}
	}
	fs.Int64Var(&maxRSSBudget, "max-rss-budget", envBudget,
		"max predicted RSS (MB) for concurrent index jobs; 0 disables admission control")

	var maxConcurrentGroups int
	// Priority: GRAFEL_REBUILD_CONCURRENCY > GRAFEL_MAX_CONCURRENT_GROUPS > auto-tune.
	envConcGroups := resolveEnvRebuildConcurrency()
	fs.IntVar(&maxConcurrentGroups, "max-concurrent-groups", envConcGroups,
		"max repos indexed in parallel during rebuild (auto-tuned from memory; floor=2 cap=16)")

	// --no-auto-cleanup disables the background docgen cleanup sweeper (#2216).
	var noAutoCleanup bool
	fs.BoolVar(&noAutoCleanup, "no-auto-cleanup", false,
		"disable the background docgen cleanup sweeper (default: enabled)")

	// --foreground (#5225): run the daemon WITHOUT an OS service manager
	// (launchd/systemd/Windows-Service). This is the CI / selftest / container
	// path: the process starts, binds the socket + dashboard, logs to stdout,
	// and blocks until SIGINT/SIGTERM. The normal background/service path
	// (`grafel start` → launchd/systemd → `grafel daemon`) is unchanged — that
	// path simply does not pass this flag. Also honours GRAFEL_DAEMON_FOREGROUND=1
	// so a runner can opt in without rewriting argv.
	//
	// Effects when enabled:
	//   - logs an explicit "foreground mode" banner to stdout so CI logs show it,
	//   - disables the Layer-1 self-defense conflict check (GRAFEL_DISABLE_SELFDEFENSE)
	//     so an ISOLATED daemon (its own GRAFEL_DAEMON_ROOT + dynamic port) can boot
	//     even when a canonical user daemon is already running — matching the
	//     in-test isolation seam. The Layer-2 CPU watchdog is left intact.
	var foreground bool
	fs.BoolVar(&foreground, "foreground", false,
		"run in the foreground without an OS service manager (CI / container mode); blocks until SIGINT/SIGTERM")

	if err := fs.Parse(argv); err != nil {
		return err
	}

	// Env-var opt-in mirrors the flag (#5225). Either turns foreground mode on.
	if v := strings.TrimSpace(os.Getenv("GRAFEL_DAEMON_FOREGROUND")); v == "1" || strings.EqualFold(v, "true") {
		foreground = true
	}
	if foreground {
		// Disable the Layer-1 startup conflict check for this run so an
		// isolated foreground daemon can boot alongside a canonical one. Set
		// before daemon.Run reads it. Idempotent: a no-op if already set.
		if os.Getenv(daemon.EnvDisableSelfDefense) == "" {
			_ = os.Setenv(daemon.EnvDisableSelfDefense, "1")
		}
		fmt.Fprintf(os.Stdout,
			"grafel: foreground mode — no OS service manager; logging to stdout; SIGINT/SIGTERM to stop (root=%s)\n",
			os.Getenv(daemon.EnvRoot))
	}

	layout, err := daemon.DefaultLayout()
	if err != nil {
		return fmt.Errorf("resolve daemon layout: %w", err)
	}

	// S7 (#2157): load mode from daemon.config.json then apply env defaults.
	// Precedence: --mode flag > daemon.config.json > Background default.
	// Env vars always win over mode defaults (ApplyDefaults only sets unset vars).
	// activeDaemonMode is captured at construction time and threaded into
	// daemon.Config.DaemonMode so the Status RPC can surface it — no package-level
	// singleton needed (issue #2411).
	var activeDaemonMode string
	{
		cfgPath := mode.DefaultConfigPath(layout.Root)
		modeCfg, _ := mode.LoadConfig(cfgPath) // missing file → zero value; not fatal
		activeMode := modeCfg.Mode
		if daemonModeFlag != "" {
			if parsed, perr := mode.Parse(daemonModeFlag); perr == nil {
				activeMode = parsed
			}
		}
		if activeMode == "" {
			activeMode = mode.Background // open-source default
		}
		mode.ApplyDefaults(activeMode)
		gcLog.Printf("daemon mode: %s", activeMode)
		activeDaemonMode = string(activeMode)
	}

	if err := daemon.EnsureLayout(layout); err != nil {
		return fmt.Errorf("ensure layout: %w", err)
	}

	// #5137: install the runtime-reloadable CPU/concurrency cap store and a
	// SIGHUP handler. cpu.json under the daemon root is re-read cheaply (mtime
	// cached) on the reindex hot path by the extract coordinator (so editing it
	// changes the per-subprocess extract caps on the NEXT reindex with no
	// restart), and SIGHUP triggers a LIVE re-apply of the daemon's own
	// in-process GOMAXPROCS via runtime.GOMAXPROCS — which is safe to call at
	// runtime. Precedence (per knob): env var > cpu.json > built-in default.
	capStore := caps.NewStore(caps.DefaultPath(layout.Root))
	extract.SetRuntimeCaps(capStore)
	installCapReloadHandler(capStore, gcLog)

	// #1626: one-time sweep to relocate any pre-existing in-repo
	// `.grafel/` graph artifacts into the external store, so groups
	// that were indexed before this change don't need a full re-index and
	// their working trees end up clean. Best-effort + idempotent.
	for _, repoPath := range daemonReposToWatch() {
		if migrated, mErr := daemon.MigrateInRepoState(repoPath); mErr != nil {
			fmt.Fprintf(os.Stderr, "grafel: migrate %s: %v\n", repoPath, mErr)
		} else if migrated {
			fmt.Fprintf(os.Stderr, "grafel: migrated in-repo .grafel for %s → store\n", repoPath)
		}
	}

	// Log to both stderr (so `grafel start` foreground mode shows
	// progress) and the rotating log file. Phase B will replace the
	// raw file with a size-rotated writer; for Phase A a single append
	// file is fine.
	logFile, err := os.OpenFile(layout.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", layout.LogPath, err)
	}
	defer logFile.Close()
	logger := buildDaemonSlogLogger(io.MultiWriter(os.Stderr, logFile))
	// ADR-0016 flip-day (#808): log the active graph format mode so users
	// can confirm the daemon is running in the expected configuration.
	logger.Info("graph format: fb-default (json-fallback enabled) — graph.fb written on every index; --skip-json opt-in drops graph.json")

	// Resolve dashboard port: env var > default. A future
	// ~/.config/grafel/daemon.toml can add more overrides.
	dashPort := defaultDashboardPort
	if v := os.Getenv("GRAFEL_DASHBOARD_PORT"); v != "" {
		if p, perr := strconv.Atoi(v); perr == nil && p > 0 && p <= 65535 {
			dashPort = p
		}
	}

	// Issue #2397: build the ExtractorConfig once at daemon startup from the
	// process environment so downstream paths (scheduler, TryIncremental) can
	// consult IsIncrementalEnabled() rather than re-reading env vars directly.
	// Captured by value here; the pointer below is the sole owner (issue #2406).
	extractorCfg := extractor.ConfigFromEnv()

	cfg := daemon.Config{
		Layout:       layout,
		Logger:       logger,
		Index:        daemonIndexFunc,
		Rebuild:      makeDaemonRebuildFunc(maxConcurrentGroups),
		QualityAudit: daemonQualityAuditFunc,

		// Phase B — wire the watcher + scheduler. The fast reactive
		// reindex skips Pass 4 (graph algorithms) so a freshly-saved
		// file becomes queryable as soon as the basic graph lands;
		// the algorithm pass is run separately on a 30s debounce.
		ReposToWatch:  daemonReposToWatch,
		GroupsForRepo: daemonGroupsForRepo,

		// #3353/#3354: linked-worktree discovery + working-tree watching.
		// Only groups with track_worktrees or watchers enabled are returned;
		// nil → discovery not started.
		WorktreeParents: daemonWorktreeParents,
		SchedulerIndex:  daemonSchedulerIndex,
		SchedulerLinks:  daemonSchedulerLinks,
		SchedulerAlgo:   daemonSchedulerAlgo,
		// Issue #2406: capture extractorCfg at construction time so the closure
		// owns an immutable pointer — no package-level singleton needed.
		SchedulerIncremental: func(ctx context.Context, repoPath string, ref string) sched.IncrementalResult {
			stateDir := daemon.StateDirForRepoRef(repoPath, ref)
			if stateDir == "" {
				stateDir = daemon.StateDirForRepo(repoPath)
			}
			// Use the caller-supplied ctx (the scheduler's shutdownCtx) so that
			// daemon SIGTERM cancels any in-flight incremental subprocess —
			// matching the fix applied to runIndex in issue #2176/#2491.
			// Fixes issue #2495.
			res := extractors.TryIncremental(ctx, repoPath, stateDir, nil, &extractorCfg)
			if res.Done {
				invalidateAfterIndex(repoPath)
				tierAfterIndex(repoPath, ref)
			}
			return sched.IncrementalResult{
				Done:           res.Done,
				FallbackReason: res.FallbackReason,
				ChangedFiles:   res.ChangedFiles,
			}
		},
		// Single source of truth for the incremental toggle (issue #2397).
		ExtractorConfig: &extractorCfg,

		MaxRSSBudgetMB:      maxRSSBudget,
		RSSHistoryPath:      filepath.Join(filepath.Dir(layout.PIDPath), "repo-rss-history.json"),
		MaxConcurrentGroups: maxConcurrentGroups,

		// S7 (#2157): propagate the resolved operational mode so the Status
		// RPC can surface it for `grafel status`.
		DaemonMode: activeDaemonMode,

		// Pattern confidence time-decay: runs every 6 hours.
		// PatternGroupDirs returns the patterns storage directory for each
		// registered group so the decay scheduler can find patterns.json.
		PatternGroupDirs: daemonPatternGroupDirs,

		// Phase D — MCP RPC surface (ADR-0017 #832).
		// Inject the tool catalog and dispatcher so the bridge can call
		// Daemon.MCPToolList / Daemon.MCPToolCall over the socket.
		MCPListTools: daemonMCPListTools,
		MCPCallTool:  daemonMCPCallTool,

		// #2224: on every branch switch, invalidate stale CrossLinkCache
		// entries in the MCP server so the next cross-repo query recomputes
		// fresh candidates for the new ref rather than returning stale ones.
		BranchSwitchSink: func(repoPath, oldRef string) {
			if srv, err := mcpServerInstance(); err == nil {
				n := srv.State.NotifyRefSwitch(repoPath, oldRef)
				_ = n // eviction count; non-zero only on multi-ref installations
			}
		},

		// Dashboard HTTP server (#929/#931): fold the SPA + REST API
		// into the daemon process so a single launchd unit serves both.
		// Capture startedAt so /api/info can report daemon uptime (#991).
		DashboardServe: makeDaemonDashboardServe(time.Now()),
		DashboardPort:  dashPort,
		DashboardBind:  "127.0.0.1",

		// PH2a (#2096): wire the watcher pause/resume manager once the
		// fsnotify watcher is up and repos are subscribed. The scheduler
		// enqueue function is injected here so the stale-detection path in
		// tierReloadCallback can trigger a reactive reindex without a global
		// reference to the scheduler.
		OnWatcherReady: func(w *watch.Watcher) {
			onWatcherReady(w, logger)
		},

		// PH2a (#2096): provide watcher pause/resume slot counts to the
		// Status RPC via a lazy wrapper around daemonWatcherMgr (which is
		// nil until OnWatcherReady fires, but Status is only called after
		// the daemon is serving).
		WatcherMgrStats: &lazyWatcherMgrStats{},

		// Docgen background sweeper (#2216): runs at startup + every 24 h to
		// remove stale staging runs and .previous-* backups.
		// Disabled via --no-auto-cleanup on `grafel start`.
		DocgenSweep: func() *daemon.DocgenSweeperConfig {
			if noAutoCleanup {
				return nil
			}
			// Snapshot the project roots once at startup so the closure does
			// not re-scan the registry on every sweep tick.
			roots := daemonReposToWatch()
			return &daemon.DocgenSweeperConfig{
				CleanupFn: func() (int, int64, error) {
					result, err := docgen.RunDocgenCleanup(docgen.CleanupOptions{
						ProjectRoots: roots,
					})
					if err != nil {
						return 0, 0, err
					}
					for _, e := range result.Errors {
						_ = e // non-fatal; logged by the sweeper
					}
					return len(result.RemovedPaths), result.TotalBytes, nil
				},
			}
		}(),

		// Shutdown cleanup: flush MCP session metrics to disk (issue #2530) and
		// close the MCP activity disk log handle (#5264). Shared with the
		// in-process selftest daemon so both close the handle identically.
		ShutdownCleanup: daemonShutdownCleanup,

		// #5236: dead-ref / dead-worktree GC hooks. When a branch is deleted or
		// a worktree removed, the reaper-driven sweep reclaims its store dir;
		// these hooks also drop the cached mmap reader and the tier slot so the
		// resident graph leaves memory.
		DeadRefTier:       deadRefTierForgetter{},
		DeadRefDropReader: deadRefDropReader,
	}

	ctx := context.Background()

	// PH2a (#2096): wire the scheduler enqueue function for stale-detection
	// in cold-wake. This is set before daemon.Run so that the first cold-wake
	// after startup can enqueue a reindex. daemonSchedulerIndex is the fast
	// reactive reindex path (skip algo pass) used by the watcher.
	daemonSchedulerEnqueue = func(repoPath string) {
		_ = daemonSchedulerIndex(ctx, repoPath, "")
	}

	// PH2 (#2090): start the tiered hibernation state machine before the daemon
	// begins serving requests. The scanner goroutine runs until ctx is cancelled.
	startDaemonTierManager(ctx, logger)

	return daemon.Run(ctx, cfg)
}

// daemonReposToWatch returns every repo from every registered group
// (deduped by absolute path). Called once at daemon startup.
//
// #2084: fleet config entries with relative paths or paths that no longer
// exist on disk (e.g. deleted worktrees) are resolved to absolute and then
// validated — entries that fail the stat check are skipped with a warning
// log line so the daemon never spawns a watcher for a phantom directory.
func daemonReposToWatch() []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var raw []string
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			raw = append(raw, r.Path)
		}
	}
	// Resolve + validate — drops relative paths to gone worktrees.
	resolved := daemon.ResolveFleetRepoPaths(raw, slog.Default())
	var out []string
	for _, abs := range resolved {
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}

// daemonGroupsForRepo returns the names of the groups whose config
// lists repoPath (compared by absolute path).
func daemonGroupsForRepo(repoPath string) []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	var out []string
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			rp, err := filepath.Abs(r.Path)
			if err != nil {
				rp = r.Path
			}
			if rp == abs {
				out = append(out, g.Name)
				break
			}
		}
	}
	return out
}

// daemonWorktreeParents returns the registered repos whose group opts into
// linked-worktree tracking (#3353/#3354). A group opts in when either
// features.track_worktrees OR features.watchers is true — worktree
// working-tree watching is a strict extension of the file watcher, so any
// group that already has watchers enabled gets it. Returns nil when no
// group opts in (the daemon then does not start worktree discovery).
//
// Each returned ParentRepo carries the group name, repo slug, and the
// resolved absolute path to the main checkout. Bare worktrees and the main
// checkout itself are filtered downstream by runWorktreeList.
func daemonWorktreeParents() []worktree.ParentRepo {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []worktree.ParentRepo
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		if !cfg.Features.TrackWorktrees && !cfg.Features.Watchers {
			continue
		}
		for _, r := range cfg.Repos {
			abs, aerr := filepath.Abs(r.Path)
			if aerr != nil {
				abs = r.Path
			}
			// Dedup on (group, path): a repo may legitimately appear in
			// multiple groups, but within a group the path is unique.
			key := g.Name + "\x00" + abs
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, worktree.ParentRepo{
				GroupName: g.Name,
				Slug:      r.Slug,
				Path:      abs,
			})
		}
	}
	return out
}

// daemonSchedulerIndex is the fast reactive reindex used by the
// scheduler's worker pool. It skips the graph-algorithm pass so the
// basic graph is available to queries within seconds of a file save;
// the algorithm pass runs separately via daemonSchedulerAlgo on a
// longer debounce.
//
// ref is the git branch name captured at Enqueue time (PH1b of #2087).
// It is passed as the repoTag so the graph artifact is written into the
// correct per-ref directory. When ref is empty the indexer falls back to
// the current HEAD ref via gitmeta.Capture inside StateDirForRepo.
//
// S5 (#2155): by default (v0.1.1) the actual Index() call is delegated to a
// short-lived child process so the daemon's heap stays flat across sustained
// reindex workloads AND the per-child GOMAXPROCS cap (default 2) bounds CPU.
// The in-process path is the OPT-OUT (GRAFEL_SUBPROCESS_INDEXER=0); see
// sched.SubprocessIndexEnabled for the env gate.
func daemonSchedulerIndex(ctx context.Context, repoPath string, ref string) error {
	var err error
	if sched.SubprocessIndexEnabled() {
		// S5 path: fork-exec `grafel index-internal` for memory isolation.
		err = sched.RunSubprocessIndex(ctx, repoPath, ref, []string{"graph-algo"}, slog.Default())
	} else {
		// In-process path (opt-out via GRAFEL_SUBPROCESS_INDEXER=0).
		// ADR-0016 flip-day (#808): graph.fb is always written by default now.
		// ref is stored via StateDirForRepo → StateDirForRepoRef at write time;
		// the indexer itself resolves the correct path via gitmeta at index time.
		_ = ref
		err = Index(repoPath, "", "", []string{"graph-algo"}, false, false)
	}
	// Drop the cached mmap so the next MCP query reopens against the
	// freshly written graph.fb. Done on both success and failure paths
	// — a stale handle is worse than a cold miss.
	invalidateAfterIndex(repoPath)
	// PH2 (#2090): register / re-activate the tier slot as HOT after index.
	tierAfterIndex(repoPath, ref)
	return err
}

// daemonSchedulerLinks re-runs the cross-repo link passes for a group.
// Delegates to the context-aware version of the links hook so the scheduler's
// shutdownCtx is threaded through for clean cancellation on daemon shutdown.
// Behaviour is identical to a force rebuild's link step, except context is
// propagated.
func daemonSchedulerLinks(ctx context.Context, group string) error {
	return runLinksHookWithCtx(ctx, group)
}

// daemonSchedulerAlgo runs the full index (including Pass 4 algorithms)
// against a repo. The scheduler arranges cancel+reschedule on new
// writes, so this is allowed to be slow.
// The ctx is the scheduler's shutdownCtx and is available for future use
// (e.g. to cancel long-running subprocess operations on daemon shutdown).
func daemonSchedulerAlgo(ctx context.Context, repoPath string) error {
	// ADR-0016 flip-day (#808): graph.fb is always written by default now.
	// #1576: tag with the registered CONFIG slug when this path is known to a
	// group, so a watcher-triggered re-index keeps doc.Repo aligned with the
	// dashboard's node slugs and the cross-repo link endpoints. An empty
	// repoTag would fall back to the dir basename and diverge from a slugified
	// config slug (e.g. my_app vs my-app), dropping cross-repo edges.
	// NOTE: ctx is not yet used by Index, but is threaded through for future
	// context-aware subprocess handling (similar to SchedulerIncremental above).
	_ = ctx
	err := Index(repoPath, "", configSlugForPath(repoPath), nil, false, false)
	invalidateAfterIndex(repoPath)
	// PH2 (#2090): re-activate the tier slot as HOT. ref="" here — the algo
	// pass runs after the fast-index which already registered the slot; this
	// just refreshes lastAccessedAt.
	tierAfterIndex(repoPath, "")
	return err
}

// configSlugForPath returns the registered config slug for repoPath by
// scanning all group configs, or "" when the path is not registered (in which
// case Index falls back to the directory basename). Paths are compared after
// filepath.Clean so trailing-slash / relative differences do not defeat the
// match.
func configSlugForPath(repoPath string) string {
	want := filepath.Clean(repoPath)
	groups, err := registry.Groups()
	if err != nil {
		return ""
	}
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			if filepath.Clean(r.Path) == want {
				return r.Slug
			}
		}
	}
	return ""
}

// daemonIndexFunc is the IndexFunc handed to daemon.Run. It bridges the
// RPC argument struct onto the existing in-process Index() entrypoint
// defined in this same package.
func daemonIndexFunc(args proto.IndexArgs) (string, string, error) {
	opts := []IndexOption{
		WithRepairCandidates(args.Repair),
		WithRepairApply(args.RepairApply),
		WithExportFB(args.ExportFB),
		WithPrintSkipped(args.PrintSkipped),
		WithAdditionalSkipDirs(args.AdditionalSkipDirs),
		WithExportJSON(args.ExportJSON),
		// #5135: an `grafel index` RPC is an explicit user-triggered
		// foreground index — run it at the higher rebuild CPU cap.
		WithInteractive(true),
	}
	// Capture stats into a local buffer when the caller asked for them.
	// setCapturedStats is a tiny package-level swap (Phase A serializes
	// indexes, so the single-writer assumption holds — see comment in
	// index.go). Phase B's job queue will thread the writer explicitly.
	var statsBuf bytes.Buffer
	if args.JSONStats {
		restore := setCapturedStats(&statsBuf)
		defer restore()
	}
	err := Index(args.RepoPath, args.OutPath, args.RepoTag, args.SkipPasses,
		args.Pretty, args.JSONStats, opts...)
	if err != nil {
		return "", "", err
	}
	graphPath := args.OutPath
	if graphPath == "" {
		graphPath = daemon.GraphPathForRepo(args.RepoPath)
	}
	return graphPath, statsBuf.String(), nil
}

// makeDaemonRebuildFunc returns the RebuildFunc injected into daemon.Config.
// concurrency is captured at construction time from runDaemon's maxConcurrentGroups
// so no package-level singleton is needed (issue #2411).
// indexFn and linksFn are captured at construction time so no package-level
// singleton is needed (issue #2414).
//
// The returned func force-indexes every repo in a group. We deliberately
// re-implement the iteration here rather than calling into internal/cli
// to avoid pulling cobra back into the daemon's call graph.
//
// Parallelism (#1276): repos are indexed concurrently up to concurrency
// workers. One failing repo does not stop the others — all are attempted and
// any errors are collected and returned together. Per-repo wall time is logged
// to stderr for diagnostics. The cross-repo link pass runs only once all
// per-repo indexes complete.
func makeDaemonRebuildFunc(concurrency int) daemon.RebuildFunc {
	indexFn := func(repoPath, outPath, repoTag string, skipPasses []string, pretty, jsonStats bool, opts ...IndexOption) error {
		// #5135: a rebuild RPC is an explicit, user-triggered foreground
		// rebuild — run it at the higher GRAFEL_REBUILD_GOMAXPROCS cap
		// instead of the throttled background extract cap. WithInteractive is
		// prepended so an explicit opts override (if any) still wins.
		opts = append([]IndexOption{WithInteractive(true)}, opts...)
		return Index(repoPath, outPath, repoTag, skipPasses, pretty, jsonStats, opts...)
	}
	linksFn := func(group string) error {
		return runLinksHook(group)
	}
	return func(args proto.RebuildArgs) ([]string, string, error) {
		return daemonRebuildFuncCore(concurrency, args, indexFn, linksFn)
	}
}

// repoResult holds the outcome of indexing a single repo during a rebuild.
// It is shared between the serial and parallel paths in daemonRebuildFuncCore
// and filled by rebuildWorkerPool.
type repoResult struct {
	path string
	slug string
	err  error
	took time.Duration
}

// repoWork is the unit of work dispatched to each indexer invocation.
type repoWork struct {
	r registry.Repo
}

// rebuildWorkerPool dispatches work items to at most conc concurrent goroutines
// and collects the results into a slice that preserves input order.
//
// workFn is called once per item. It must be safe to invoke concurrently.
// Panics inside workFn are NOT recovered here — callers are responsible for
// protecting workFn with a recover if needed (see daemonRebuildFuncCore).
//
// The semaphore protocol guarantees that the defer releasing a slot only fires
// after the slot has been acquired, so a panic before sem<- cannot leave a
// phantom holder. The deferred wg.Done is registered before sem<-, which means
// it fires even if the goroutine panics after launch but before acquiring the
// slot — in that rare edge the slot is simply never acquired and the result
// slot stays at its zero value.
func rebuildWorkerPool(conc int, work []repoWork, workFn func(idx int, rw repoWork) repoResult) []repoResult {
	results := make([]repoResult, len(work))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i, w := range work {
		wg.Add(1)
		go func(idx int, rw repoWork) {
			defer wg.Done()

			// Acquire semaphore slot. This MUST come before any panic-recovery
			// defer so the defer's <-sem only fires once the slot is actually held.
			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = workFn(idx, rw)
		}(i, w)
	}
	wg.Wait()
	return results
}

// daemonRebuildFuncCore is the testable inner implementation of the rebuild
// logic. concurrency is supplied by the caller (captured in the closure
// returned by makeDaemonRebuildFunc, or set directly in tests). indexFn and
// linksFn are the per-repo index and cross-repo link hooks; production callers
// pass the real implementations (captured at construction time in
// makeDaemonRebuildFunc); tests pass mocks directly — no package-level
// singleton mutation required (issue #2414).
func daemonRebuildFuncCore(
	concurrency int,
	args proto.RebuildArgs,
	indexFn func(repoPath, outPath, repoTag string, skipPasses []string, pretty, jsonStats bool, opts ...IndexOption) error,
	linksFn func(group string) error,
) ([]string, string, error) {
	rebuildStart := time.Now()
	fmt.Fprintf(os.Stderr, "grafel: rebuild start group=%s slug=%q wipe=%v incremental=%v\n",
		args.Group, args.Slug, args.Wipe, args.Incremental)
	defer func() {
		fmt.Fprintf(os.Stderr, "grafel: rebuild exit group=%s took=%s\n",
			args.Group, time.Since(rebuildStart).Truncate(time.Millisecond))
	}()

	groups, err := registry.Groups()
	if err != nil {
		return nil, "", err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == args.Group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return nil, "", fmt.Errorf("unknown group: %s", args.Group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return nil, "", err
	}
	// Issue #1206 — apply group-level extra_stdlib_filter before indexing so
	// the synthesiser suppresses user-configured framework stdlib names.
	for lang, names := range cfg.ExtraStdlibFilter {
		resolve.RegisterExtraStdlibFilter(lang, names)
	}

	// Collect repos to index, respecting the optional single-slug filter.
	var work []repoWork
	for _, r := range cfg.Repos {
		if args.Slug != "" && r.Slug != args.Slug {
			continue
		}
		work = append(work, repoWork{r: r})
	}

	// Serial fast path: single worker or single repo skips goroutine overhead.
	conc := concurrency
	if conc < 1 {
		conc = 1
	}

	perRepoTimeout := resolvePerRepoRebuildTimeout()

	// indexOne executes the index function for a single repo and returns its
	// result. It is shared by both the serial and parallel paths so the logic
	// (panic recovery, wipe, incremental opts, progress slugs, slug tag) stays
	// in one place.
	indexOneInner := func(idx int, rw repoWork) repoResult {
		t0 := time.Now()
		var indexErr error
		func() {
			// Panic recovery (#2097): convert an extractor panic into an error so
			// the remaining repos in the batch can still run, and so a panicking
			// goroutine cannot crash the daemon process.
			defer func() {
				if r := recover(); r != nil {
					indexErr = fmt.Errorf("index panic: %v", r)
					fmt.Fprintf(os.Stderr,
						"grafel: rebuild %s panic recovered: %v\n",
						rw.r.Slug, r)
				}
			}()
			if args.Wipe {
				_ = os.RemoveAll(daemon.StateDirForRepo(rw.r.Path))
			}
			var opts []IndexOption
			if args.Incremental && !args.Wipe {
				opts = append(opts, WithIncremental(daemon.StateDirForRepo(rw.r.Path)))
			}
			// Publish granular per-repo progress into the shared broker so the
			// WebUI Index step renders live rows + file counters (#1531).
			opts = append(opts,
				WithPublisher(daemonProgressBroker),
				WithProgressSlugs(args.Group, rw.r.Slug))
			// #1576: tag the graph with the CONFIG slug (not the on-disk
			// directory basename) so doc.Repo matches the slug the dashboard
			// keys nodes by and the slug the cross-repo link pass emits as the
			// link endpoint prefix. When the wizard slugifies a repo name
			// (e.g. my_app → my-app) an empty repoTag would fall back
			// to the dir basename and diverge, dropping every cross-repo edge.
			indexErr = indexFn(rw.r.Path, "", rw.r.Slug, nil, false, false, opts...)
		}()
		return repoResult{
			path: rw.r.Path,
			slug: rw.r.Slug,
			err:  indexErr,
			took: time.Since(t0),
		}
	}

	// indexOne wraps indexOneInner with a per-repo wall-clock timeout (#5143).
	// A single slow/stuck repo no longer wedges the whole group rebuild for the
	// 2h RPC timeout: it is surfaced (which repo + how long) and returned as a
	// typed timeout failure so the group continues with the remaining repos and
	// returns partial results. The orphaned index goroutine is left to finish
	// in the background (matching the existing RPC-timeout semantics) rather
	// than killed mid-write.
	indexOne := func(idx int, rw repoWork) repoResult {
		if perRepoTimeout <= 0 {
			return indexOneInner(idx, rw)
		}
		t0 := time.Now()
		done := make(chan repoResult, 1)
		go func() { done <- indexOneInner(idx, rw) }()
		timer := time.NewTimer(perRepoTimeout)
		defer timer.Stop()
		select {
		case res := <-done:
			return res
		case <-timer.C:
			fmt.Fprintf(os.Stderr,
				"grafel: rebuild %s STALLED — no result after %s; surfacing as timeout and continuing with remaining repos (group=%s)\n",
				rw.r.Slug, perRepoTimeout, args.Group)
			return repoResult{
				path: rw.r.Path,
				slug: rw.r.Slug,
				err:  fmt.Errorf("repo index timed out after %s (still running in background)", perRepoTimeout),
				took: time.Since(t0),
			}
		}
	}

	var results []repoResult
	if conc == 1 || len(work) <= 1 {
		// --- Serial path ---
		results = make([]repoResult, len(work))
		for i, w := range work {
			results[i] = indexOne(i, w)
		}
	} else {
		// --- Parallel path: delegate to rebuildWorkerPool ---
		results = rebuildWorkerPool(conc, work, indexOne)
	}

	// Collect successful paths; log per-repo wall time; gather errors.
	var rebuilt []string
	var errs []string
	for _, res := range results {
		if res.path == "" {
			continue // slot never filled (shouldn't happen)
		}
		fmt.Fprintf(os.Stderr, "grafel: rebuild %s took %s",
			res.slug, res.took.Truncate(time.Millisecond))
		if res.err != nil {
			fmt.Fprintf(os.Stderr, " [FAILED: %v]\n", res.err)
			errs = append(errs, fmt.Sprintf("index %s: %v", res.slug, res.err))
			continue
		}
		fmt.Fprintln(os.Stderr, "")
		rebuilt = append(rebuilt, res.path)

		// Auto-inject Architecture Map block into AGENTS.md / CLAUDE.md when
		// opted in. Best-effort: a write failure is logged but never fails the
		// rebuild so a read-only repo or missing permissions don't surface as
		// an error to the user (#1216).
		if cfg.Features.AutoInjectAgentsMD {
			mapStats := buildAgentsMapStats(cfg.Name, res.path)
			if err := agents.InjectArchitectureMap(res.path, mapStats); err != nil {
				fmt.Fprintf(os.Stderr,
					"grafel: auto-inject agents map for %s: %v (non-fatal)\n",
					res.slug, err)
			}
		}
	}

	// Return a combined error if any repos failed. The rebuilt list still
	// contains all repos that succeeded, so the caller can report partial results.
	if len(errs) > 0 {
		return rebuilt, "", fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	// Cross-repo link passes run after every member is indexed.
	warning := ""
	if err := linksFn(args.Group); err != nil {
		// Best-effort — surface as a warning, not a hard failure.
		warning = fmt.Sprintf("link passes failed: %v", err)
	}

	// Explicitly invalidate the cache for each rebuilt repo (#2607).
	// Belt-and-braces: the LRU cache's mtime safety-net has 1s granularity
	// which can race when rebuild completes faster. Explicit invalidation
	// ensures the next MCP query sees the freshly written graph.fb.
	for _, repoPath := range rebuilt {
		invalidateAfterIndex(repoPath)
	}

	// Persist a quality-metrics snapshot to health-history.jsonl (#1329).
	// Best-effort: failure is logged but never blocks the caller.
	go func() {
		if layout, lerr := daemon.DefaultLayout(); lerr == nil {
			if herr := appendRebuildHistory(layout.Root, args.Group, cfg, rebuilt); herr != nil {
				fmt.Fprintf(os.Stderr, "grafel: record quality history for %s: %v (non-fatal)\n",
					args.Group, herr)
			}
		}
	}()

	return rebuilt, warning, nil
}

// buildAgentsMapStats loads the per-repo graph artefacts produced by the
// just-completed index and assembles the Stats struct passed to
// agents.InjectArchitectureMap. It is intentionally best-effort — any read
// failure yields a zero-valued field rather than an error.
func buildAgentsMapStats(group, repoPath string) agents.Stats {
	stateDir := daemon.StateDirForRepo(repoPath)

	s := agents.Stats{
		Group:         group,
		DashboardPort: resolveDefaultDashboardPort(),
	}

	// Read graph.fb for per-kind entity breakdown. Falls back gracefully if the
	// file is absent or the FB decoder is unavailable.
	if doc, err := loadGraphFromStateDir(stateDir); err == nil && doc != nil {
		s.Entities = doc.Stats.Entities
		s.Relationships = doc.Stats.Relationships
		for _, e := range doc.Entities {
			switch e.Kind {
			// #1217: count all three http endpoint kind strings.
			case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
				s.HTTPEndpoints++
			case "queue":
				s.Queues++
			case "topic", "pubsub_topic":
				s.Topics++
			}
			if strings.HasPrefix(e.Kind, "SCOPE.Process") || e.Kind == "process" {
				s.ProcessFlows++
			}
		}
	}

	return s
}

// loadGraphFromStateDir is a thin wrapper around graph.LoadGraphFromDir that
// isolates the graph-loading call used by buildAgentsMapStats. Keeping it
// separate makes it easy to stub in tests without touching the full graph
// package.
func loadGraphFromStateDir(stateDir string) (*graph.Document, error) {
	return graph.LoadGraphFromDir(stateDir)
}

// daemonQualityAuditFunc is the QualityAuditFunc handed to daemon.Run.
// It calls audit.AuditPath (in this process — the daemon process) and
// serialises the result into the wire reply.
func daemonQualityAuditFunc(args proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
	rep, err := audit.AuditPath(args.RepoPath, args.Corpus)
	if err != nil {
		return proto.QualityAuditReply{}, err
	}

	// Build the scalar summary by folding per-repo numbers.
	var totalEntities, totalOrphans int
	orphansByKind := make(map[string]int)
	for _, rr := range rep.Repos {
		if rr == nil {
			continue
		}
		totalEntities += rr.Entities
		totalOrphans += rr.Orphans
		for cause, n := range rr.OrphanClassification {
			orphansByKind[string(cause)] += n
		}
	}
	orphanRate := 0.0
	if totalEntities > 0 {
		orphanRate = 100.0 * float64(totalOrphans) / float64(totalEntities)
	}

	// Serialise the report according to the requested format.
	var sb strings.Builder
	if args.JSON {
		enc := json.NewEncoder(&sb)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return proto.QualityAuditReply{}, fmt.Errorf("encode audit report: %w", err)
		}
	} else {
		if err := rep.WriteMarkdown(&sb); err != nil {
			return proto.QualityAuditReply{}, fmt.Errorf("format audit report: %w", err)
		}
	}

	return proto.QualityAuditReply{
		OrphansByKind:     orphansByKind,
		TotalEntities:     totalEntities,
		TotalOrphans:      totalOrphans,
		OrphanRatePercent: orphanRate,
		Markdown:          sb.String(),
	}, nil
}

// daemonRecallFunc is the RecallRunner injected into the dashboard server.
// It runs the full in-process indexer against a named golden fixture and
// returns the quality.JSONReport serialised as JSON bytes.
//
// The fixture must be one of the directories inside internal/quality/golden/;
// the path is resolved via goldenFixturesDir() inside the handler.
func daemonRecallFunc(fixtureName string) ([]byte, error) {
	goldenDir, err := dashboard.GoldenFixturesDir()
	if err != nil {
		return nil, fmt.Errorf("locate fixtures: %w", err)
	}
	fixtureDir := filepath.Join(goldenDir, fixtureName)

	fix, err := quality.LoadFixture(fixtureDir)
	if err != nil {
		return nil, fmt.Errorf("load fixture %q: %w", fixtureName, err)
	}
	srcDir := quality.SourceDir(fixtureDir)
	if st, serr := os.Stat(srcDir); serr != nil || !st.IsDir() {
		return nil, fmt.Errorf("fixture src/ missing or not a directory: %s", srcDir)
	}

	tmp, err := os.MkdirTemp("", "grafel-recall-*")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(tmp)

	graphPath := filepath.Join(tmp, "graph.json")
	if err := Index(srcDir, graphPath, fix.Name, nil, false, false, WithExportJSON(true)); err != nil {
		return nil, fmt.Errorf("index fixture src: %w", err)
	}

	doc, err := loadDocument(graphPath)
	if err != nil {
		return nil, fmt.Errorf("load graph: %w", err)
	}

	rep := quality.Evaluate(fix, doc)
	jr := rep.ToJSON()
	raw, err := json.Marshal(jr)
	if err != nil {
		return nil, fmt.Errorf("encode recall report: %w", err)
	}
	return raw, nil
}

// mustEncodeStatus is a small helper for the `status` command when it
// prints the daemon's reply as JSON. Lives here so cmd/grafel
// doesn't have to import encoding/json from a half-dozen call sites.
func mustEncodeStatus(w io.Writer, reply proto.StatusReply) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(reply)
}

// daemonNotRunningErr is the canonical user-facing error returned by
// any client subcommand when the daemon socket is unreachable.
var daemonNotRunningErr = errors.New(
	"daemon not running; run 'grafel start' or reinstall via 'grafel install'",
)

// daemonPatternGroupDirs returns a map of group-name → patterns storage
// directory for every registered group. This is injected into daemon.Config
// so the pattern decay scheduler can find each group's patterns.json.
//
// Directory convention mirrors internal/mcp/patterns.go defaultPatternsDir:
// ~/.grafel/groups/<group>-patterns/. Groups whose patterns are stored in
// a custom MemoryDir (MCP registry config) will be found there by the MCP
// server; the daemon uses the default path which covers production deployments.
func daemonPatternGroupDirs() map[string]string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	out := make(map[string]string, len(groups))
	for _, g := range groups {
		dir := filepath.Join(home, ".grafel", "groups", g.Name+"-patterns")
		out[g.Name] = dir
	}
	return out
}

// makeDaemonDashboardServe returns the DashboardServe hook injected into
// daemon.Config. It captures daemonStartedAt so the /api/info endpoint can
// report uptime without a separate RPC call (#991).
//
// This function lives in cmd/grafel (not internal/daemon) to avoid the
// import cycle: internal/dashboard already imports internal/daemon.
func makeDaemonDashboardServe(daemonStartedAt time.Time) func(ctx context.Context, bind string, port int, logger *slog.Logger, onListen func(addr string)) error {
	return func(ctx context.Context, bind string, port int, logger *slog.Logger, onListen func(addr string)) error {
		addr := net.JoinHostPort(bind, strconv.Itoa(port))
		// #5264: bracket the dashboard net.Listen — the prime suspect for the
		// Windows isolated-selftest startup hang. If "dashboard-listen begin"
		// appears with no matching "done", the TCP bind is wedging.
		if logger != nil {
			logger.Info("startup: dashboard-listen begin", "addr", addr)
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("dashboard listen %s: %w", addr, err)
		}
		if logger != nil {
			logger.Info("startup: dashboard-listen done", "addr", l.Addr().String())
		}

		// Resolve the ACTUAL bound address. When port==0 the OS assigned a
		// free port at bind time (#5224); read it back so the dashboard
		// config and any onListen observer see the real port — no
		// pick-then-close-then-rebind race.
		resolvedPort := port
		if tcpAddr, ok := l.Addr().(*net.TCPAddr); ok {
			resolvedPort = tcpAddr.Port
		}
		addr = net.JoinHostPort(bind, strconv.Itoa(resolvedPort))
		if onListen != nil {
			onListen(l.Addr().String())
		}

		// Build dashboard config: fixed port (the daemon already owns the listener).
		cfg := dashboard.Config{
			PortRange: dashboard.PortRange{Min: resolvedPort, Max: resolvedPort},
			Bind:      bind,
		}
		srv, err := dashboard.NewServer(cfg, dashboard.NewLiveStore())
		if err != nil {
			_ = l.Close()
			return fmt.Errorf("dashboard new server: %w", err)
		}
		// Tell the dashboard server when the daemon started so /api/info
		// can compute and report uptime (#991).
		srv.SetDaemonStartedAt(daemonStartedAt)

		// Wire the shared indexer progress broker (#1531) so the
		// /api/index-progress SSE endpoints can fan granular per-repo /
		// per-module progress.Event records to the WebUI Index step. The
		// Rebuild path publishes into this same broker (see daemonRebuildFunc).
		srv.SetProgressBroker(daemonProgressBroker)

		// Wire MCP activity broker (epic #1157, Phase 1: Jarvis).
		// The same broker is injected into the shared MCP server so tool
		// calls emit events that flow through the dashboard SSE endpoint.
		activityBroker := mcp.NewMCPActivityBroker()
		logPath := mcp.DefaultActivityLogPath()
		if logPath != "" {
			actLog := mcp.NewActivityLog(logPath)
			activityBroker.SetLog(actLog)
			srv.SetMCPActivityLog(logPath)
		}
		srv.SetMCPActivityBroker(activityBroker)
		// Record the broker process-wide so the graceful-stop path can flush +
		// close its disk log handle before ~/.grafel is removed (#5264).
		setDaemonActivityBroker(activityBroker)
		// Wire the broker into the shared MCP server (lazily initialised).
		// We call mcpServerInstance here to ensure it exists; on failure we
		// proceed without activity emission rather than crashing the daemon.
		if mcpSrv, initErr := mcpServerInstance(); initErr == nil {
			mcpSrv.SetActivityBroker(activityBroker)
		}

		// Wire the recall runner so POST /api/quality/recall can run the
		// in-process indexer against golden fixtures (#1198).
		srv.SetRecallRunner(daemonRecallFunc)

		// PH2 (#2090): wire the tier manager into the dashboard so that
		// GET /api/v2/groups/:g/refs returns real HOT/WARM/COLD status.
		if daemonTierMgr != nil {
			srv.SetTierQuerier(daemonTierMgr)
		}
		// PH2a (#2096): wire the watcher pause/resume state into the dashboard
		// so that GET /api/v2/groups/:g/refs returns watcher_state per ref.
		if daemonWatcherMgr != nil {
			srv.SetWatcherQuerier(daemonWatcherMgr)
		}

		// #5238: register the dashboard's per-group invalidator so the tier
		// manager can evict the dashboard GraphCache's heavy materialised
		// graph state on a WARM→COLD demotion (not just the cheap mmap'd MCP
		// reader). Without this, an idle group's full *graph.Document +
		// re-derived algorithm results + search index stay on the heap until
		// the group happens to be re-requested past its TTL.
		setDashboardGroupInvalidator(srv.InvalidateGroup)

		// Wire the enrichment job queue (#1244). Jobs persist to
		// ~/.grafel/jobs.jsonl so history survives daemon restarts.
		var jobHistoryPath string
		if daemonLayout, layoutErr := daemon.DefaultLayout(); layoutErr == nil {
			jobHistoryPath = filepath.Join(daemonLayout.Root, "jobs.jsonl")
		}
		jobQueue := jobs.NewQueue(jobHistoryPath, jobs.DefaultWorkers)
		jobQueue.Start()
		srv.SetJobQueue(jobQueue)
		// Stop the job queue when the daemon context is cancelled.
		go func() {
			<-ctx.Done()
			jobQueue.Stop()
		}()

		srv.UseListener(l)
		if logger != nil {
			logger.Info("dashboard ready", "url", "http://"+addr+"/")
			// #5264: last trace before the blocking Serve loop. Once
			// "dashboard ready" is logged the listener is bound + wired, so a
			// hang past this point is in Serve/HTTP, not startup wiring.
			logger.Info("startup: dashboard-serve-loop begin", "url", "http://"+addr+"/")
		}
		return srv.Serve(ctx)
	}
}

// buildDaemonSlogLogger constructs a *slog.Logger for the daemon process.
// Handler selection follows GRAFEL_DAEMON_LOG_JSON (same as daemon.buildSlogLogger).
func buildDaemonSlogLogger(w io.Writer) *slog.Logger {
	v := strings.TrimSpace(os.Getenv("GRAFEL_DAEMON_LOG_JSON"))
	if v == "1" || strings.EqualFold(v, "true") {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}
