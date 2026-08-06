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
	"github.com/cajasmota/grafel/internal/daemon/fdlimit"
	"github.com/cajasmota/grafel/internal/daemon/mode"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/daemon/worktree"
	"github.com/cajasmota/grafel/internal/dashboard"
	"github.com/cajasmota/grafel/internal/docgen"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/jobs"
	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/mcp"
	"github.com/cajasmota/grafel/internal/memtrace"
	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/quality"
	"github.com/cajasmota/grafel/internal/quality/analytics"
	"github.com/cajasmota/grafel/internal/quality/audit"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/repolock"
	"github.com/cajasmota/grafel/internal/resolve"
)

// daemonProgressBroker is the process-wide indexer progress bus. The Rebuild
// path publishes granular per-repo progress.Event records into it (via the
// indexer's WithPublisher option) and the dashboard's /api/index-progress SSE
// endpoints subscribe to it, so the WebUI Index step renders live per-repo /
// per-module rows with file counters instead of a generic bar (#1531). It is
// created once in runDaemon before the RPC + dashboard servers start.
var daemonProgressBroker = progress.NewBroker()

// daemonIndexGate is the process-wide index-concurrency gate (#5493). Every
// per-module/per-repo index dispatched by the rebuild fan-out drains through it,
// so a group/monorepo with many modules indexes at most GRAFEL_INDEX_CONCURRENCY
// (default 2) at a time instead of all-at-once — the fix for the 30-module
// monorepo storm. The rebuild worker pool may still SIZE itself to the larger
// memory-auto-tuned cap, but this gate is the real throttle on simultaneous
// indexes; the per-index core cap (GRAFEL_EXTRACT_GOMAXPROCS) only bounds cores
// WITHIN one index. One slot is reserved for foreground/interactive index so a
// background storm cannot lock it out (#5328).
var daemonIndexGate = daemon.NewIndexGateFromEnv()

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
	if configured := daemon.ConfiguredRSSBudgetMB(); configured > 0 {
		return configured
	}
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
	return resolvePerRepoRebuildTimeoutWithOverride("")
}

// resolvePerRepoRebuildTimeoutWithOverride is resolvePerRepoRebuildTimeout,
// additionally honoring a per-invocation override (proto.RebuildArgs.RepoTimeout,
// #5822 sub-ask 3) — e.g. `grafel rebuild --timeout <dur>` — which takes
// PRIORITY over GRAFEL_REBUILD_REPO_TIMEOUT so a single genuinely-large-repo
// rebuild can raise (or disable, via "0") the watchdog without touching daemon
// env/config. An empty or unparseable override falls through to the
// env/default resolution unchanged.
func resolvePerRepoRebuildTimeoutWithOverride(override string) time.Duration {
	if override != "" {
		if d, err := time.ParseDuration(override); err == nil {
			if d <= 0 {
				return 0
			}
			return d
		}
	}
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
	// #6108 — MUST go through process.ApplyGOMAXPROCS, not runtime directly.
	// The daemon now opens capped GOMAXPROCS regions around its in-process
	// analytics passes, and a handler that read/wrote the runtime value itself
	// lost this reload whenever one was open: a target equal to the region's
	// clamp read back as "unchanged" and no-oped, and the region's restore then
	// wrote back the pre-region host value — leaving the daemon PERMANENTLY
	// ABOVE the operator's configured cap. Routing through the package makes the
	// operator's value the baseline a live region restores to.
	prev, changed := process.ApplyGOMAXPROCS(target)
	return target, prev, changed
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
			// #5970: the in-process parse gate reads the same cpu.json pin, so
			// re-resolve it here too. Without this an operator who tightens
			// cpu.json and SIGHUPs would move the daemon's GOMAXPROCS while the
			// parse gate — the thing that actually bounds cgo — kept its
			// start-up value. Raising wakes queued parses; lowering applies to
			// the next acquirer and never preempts a parse already running.
			parseCap := installDaemonParseCap(runtime.NumCPU(), store)
			logf.Printf("cpu-tune: SIGHUP reload — in-process parse concurrency cap=%d (#5970)", parseCap)
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

// daemonParseCap resolves the cap on CONCURRENT in-process tree-sitter parses
// for a daemon process (`grafel serve` / `grafel engine`), given the host core
// count. Never returns 0 — 0 means "unbounded" to the gate, the opposite of
// what a degenerate core count must produce.
//
// WHY NOT GOMAXPROCS (#5970). Until this change the daemon sized this gate from
// runtime.GOMAXPROCS(0), i.e. 100% of its own runtime parallelism — half the
// box on a default install, the WHOLE box whenever the half-cores default did
// not apply (unknown core count, or an operator raising the pin). That is the
// reactive path: the watcher fires on the user's own `git checkout`/save, the
// daemon re-parses in-process, and the user's machine slows down while they
// type. The standing policy is 25% of machine capacity for background indexing
// (process.IndexCoreBudget), and #5972/#5973 shipped it for the CLI index path
// and the extract-subprocess fanout respectively — both of which fork, and
// neither of which touched this call site. This is the last one, and the only
// one that runs unasked during the user's normal work.
//
// GOMAXPROCS is also not a substitute for this gate in the first place:
// tree-sitter parsing is cgo, and a goroutine inside a cgo call parks in
// _Gsyscall while the runtime hands its P to another goroutine, so N concurrent
// parses occupy N OS threads whatever GOMAXPROCS says. The counting semaphore
// held across the cgo call is the real bound (see internal/indexstate/parsegate.go).
//
// OPERATOR OVERRIDE. The two surfaces that pin the daemon's CPU — the
// GRAFEL_DAEMON_GOMAXPROCS env var and cpu.json's daemon GOMAXPROCS field —
// are operator decisions about how much of the machine this daemon may use,
// and keep top precedence UNCLAMPED in both directions: either may tighten the
// gate below the budget or open it above, up to and including the whole host.
// fileVal is the cpu.json value (0 = unset), so the precedence here is the same
// env > cpu.json > default chain resolveDaemonGOMAXPROCSWith documents; only
// the trailing default differs, and deliberately: the 25% background budget,
// not the half-cores GOMAXPROCS default. That default is a bound on the
// daemon's Go-runtime parallelism (query handling included), not a statement
// about how much background parsing may draw.
func daemonParseCap(hostCPU, fileVal int) int {
	if n := envPositiveInt2("GRAFEL_DAEMON_GOMAXPROCS"); n > 0 {
		return n
	}
	if fileVal > 0 {
		return fileVal
	}
	return process.IndexCoreBudgetFor(hostCPU)
}

// daemonParseCapFileVal reads the cpu.json daemon-GOMAXPROCS pin from the caps
// store, returning 0 when there is no store, no file, or no value. Kept
// separate from daemonParseCap so the resolution policy stays a pure function.
func daemonParseCapFileVal(store *caps.Store) int {
	if store == nil {
		return 0
	}
	cfg, err := store.Load()
	if err != nil {
		return 0
	}
	return cfg.DaemonGOMAXPROCSValue()
}

// installDaemonParseCap resolves the daemon's parse cap for hostCPU (honouring
// the cpu.json pin in store, if any) and installs it on the process-wide gate,
// returning the value now in force.
//
// Separated from runDaemonMode so the value that actually reaches
// indexstate is assertable in a test — runDaemonMode binds sockets, spawns
// children and blocks forever, so it cannot itself be unit-tested, and the two
// previous attempts at this fix both left a cap that "looked right" on a path
// no test exercised.
//
// The installed cap is the BACKGROUND ceiling. User-awaited in-process parsing
// inside this same process lifts it for its duration via
// indexstate.BeginForegroundParse (see daemonIndexFunc, daemonRebuildFuncCore).
func installDaemonParseCap(hostCPU int, store *caps.Store) int {
	cap := daemonParseCap(hostCPU, daemonParseCapFileVal(store))
	indexstate.SetParseConcurrency(cap)
	return cap
}

// daemonRunMode selects which of the daemon.Config-driven entrypoints
// runDaemonMode hands off to at the end of its shared runtime-tune +
// config-assembly prelude (ADR-0024 Phase 1: entrypoint/config carve).
type daemonRunMode int

const (
	daemonRunModeServe daemonRunMode = iota
	daemonRunModeEngine
)

// runDaemon is the long-running mode of the grafel binary. It is wired
// into the CLI as a hidden `grafel daemon` subcommand — users normally
// reach it via `grafel start`, which forks this process and detaches.
//
// ADR-0024 (serve/engine split, Phase 1): `daemon` is now a back-compat
// shim that runs the SAME path as `grafel serve` (runServe below), so an
// existing OS unit that still execs `grafel daemon` transparently becomes
// a serve process. Zero client-visible change.
func runDaemon(argv []string) error {
	return runServe(argv)
}

// runServe is the `grafel serve` entrypoint (ADR-0024): the MCP socket +
// dashboard plane. As of PR6/epic #5729 the capability flag
// (daemon.SplitModeEnabled) is ON BY DEFAULT, so serve spawns and supervises
// a separate `grafel engine` child for the scheduler/watcher/extraction/
// fbwriter. The escape hatch — GRAFEL_SPLIT_MODE=0 (or "false"/"off"/"no") —
// falls back to running the engine plane in-process, identically to the
// pre-split daemon. It shares the entire runtime-tune + daemon.Config
// assembly prelude with runEngine via runDaemonMode.
func runServe(argv []string) error {
	return runDaemonMode(argv, daemonRunModeServe)
}

// runEngine is the `grafel engine` entrypoint (ADR-0024 Phase 1): the
// scheduler/watcher/extraction/fbwriter plane. In this PR it shares the
// same config-assembly prelude as runServe but hands off to
// daemon.RunEngine, which — until PR2 lands the real process split —
// deliberately refuses to run standalone (see daemon.RunEngine's doc).
func runEngine(argv []string) error {
	return runDaemonMode(argv, daemonRunModeEngine)
}

// runDaemonMode holds the runtime-tune prelude (GC/GOMAXPROCS/fd-limit
// tuning, flag parsing, layout/mode resolution, daemon.Config assembly)
// shared by runServe and runEngine (ADR-0024 Phase 1 config carve), then
// dispatches to daemon.RunServe or daemon.RunEngine depending on mode.
//
// All extractor + registry + linker work happens here. The CLI's other
// subcommands are thin RPC clients (see internal/daemon/client).
func runDaemonMode(argv []string, runMode daemonRunMode) error {
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

	// #5675: raise RLIMIT_NOFILE toward the hard limit so a worktree indexing
	// storm (each subscribed working tree costs ~1 fd per directory on Linux
	// inotify) cannot exhaust fds and crash the daemon into a KeepAlive/Restart
	// relaunch loop. Best-effort and NON-FATAL: never lowers an already-high
	// limit; a failure to raise is logged and startup continues. Overridable
	// via GRAFEL_DAEMON_FD_LIMIT.
	fdTarget := fdlimit.DefaultTarget
	if v := envPositiveInt2("GRAFEL_DAEMON_FD_LIMIT"); v > 0 {
		fdTarget = uint64(v)
	}
	if oldFD, newFD, ferr := fdlimit.Raise(fdTarget); ferr != nil {
		gcLog.Printf("fd-tune: WARN could not raise RLIMIT_NOFILE (target=%d, current=%d): %v", fdTarget, oldFD, ferr)
	} else {
		gcLog.Printf("fd-tune: RLIMIT_NOFILE soft limit %d -> %d (target=%d)", oldFD, newFD, fdTarget)
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

	// Windows: detach from any inherited console window so the background daemon
	// survives the launching terminal. When `grafel install` (or a Task
	// Scheduler InteractiveToken action) starts the daemon, it can inherit the
	// installing shell's console; closing that window would otherwise take the
	// daemon — and the dashboard it serves — down with it. FreeConsole drops the
	// association. Skipped in foreground mode, where the console is intentionally
	// the log sink (CI / container). No-op on macOS/Linux. See detachconsole_*.go.
	if !foreground {
		detachConsole()
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

	// #5630/#5970: bound CONCURRENT in-process tree-sitter parses at the
	// BACKGROUND core budget. See installDaemonParseCap for why GOMAXPROCS was
	// the wrong number here, and for the foreground exemption. Installed HERE,
	// after capStore exists, so the cpu.json pin is honoured — the same
	// env > cpu.json > default precedence every other daemon CPU knob has, and
	// re-applied on SIGHUP by installCapReloadHandler. Nothing between process
	// start and this point parses, so the gate is in force before any acquirer
	// reaches it.
	parseCap := installDaemonParseCap(runtime.NumCPU(), capStore)
	gcLog.Printf("cpu-tune: in-process parse concurrency cap=%d of %d cores (#5970)", parseCap, runtime.NumCPU())

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

	// #5956 memtrace M3: whole-machine rollup. This process self-samples
	// into the SAME GRAFEL_MEMTRACE_DIR the index-internal child writes into
	// (inherited automatically — RunSubprocessIndex builds the child's env
	// from os.Environ(), so setting this var before starting the daemon is
	// enough for both the engine/serve process AND every child it forks to
	// trace).
	//
	// #5954: phaseFn is links.CurrentPhase, NOT nil. It used to be nil on the
	// reasoning that "no phase concept applies to a long-running process", and
	// the cost of that was a whole unprofiled plane: the engine peaks AFTER the
	// index child exits and holds ~1.9GB for minutes with every sample tagged
	// "", leaving heap-peak.pprof as the only usable artifact. The engine DOES
	// have one heavy staged workload — the cross-repo link pass, which
	// materialises the group union twice — and that is what this reports.
	// Outside a link pass the phase reads "" (none run yet) or "idle" (one has
	// finished), and an idle-tagged sample is itself the finding: it is
	// plateau — memory held but not being worked on. Inert by
	// default: Start returns nil (no goroutine, no file) when
	// GRAFEL_MEMTRACE_DIR is unset. Runs for the lifetime of the process —
	// no Stop() call needed, matching other fire-and-forget daemon-startup
	// singletons (e.g. installCapReloadHandler above).
	memtraceRole := "serve"
	if runMode == daemonRunModeEngine {
		memtraceRole = "engine"
	}
	memtrace.Start(memtraceRole, links.CurrentPhase, func(format string, args ...any) {
		logger.Warn(fmt.Sprintf("memtrace: "+format, args...))
	})

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

		// Split-mode progress bridge (ADR-0024 / epic #5729): the SAME broker the
		// dashboard SSE subscribes to (srv.SetProgressBroker below). In split mode
		// the serve plane's sidecarTailer republishes the engine's per-group
		// progress sidecars into it.
		ProgressBroker: daemonProgressBroker,

		// Phase B — wire the watcher + scheduler. The fast reactive
		// reindex skips Pass 4 (graph algorithms) so a freshly-saved
		// file becomes queryable as soon as the basic graph lands;
		// the algorithm pass is run separately on a 30s debounce.
		ReposToWatch:  daemonReposToWatch,
		GroupsForRepo: daemonGroupsForRepo,

		// #3353/#3354: linked-worktree discovery + working-tree watching.
		// Only groups with track_worktrees or watchers enabled are returned;
		// nil → discovery not started.
		WorktreeParents:    daemonWorktreeParents,
		SchedulerIndex:     daemonSchedulerIndex,
		SchedulerLinks:     daemonSchedulerLinks,
		SchedulerGroupAlgo: daemonSchedulerGroupAlgo,
		// #5403: settled-group overlay-freshness sweep. The scheduler polls this
		// on GRAFEL_OVERLAY_SWEEP_INTERVAL (default 10m; "0" disables) and re-arms
		// a debounced + CPU-capped group-algo pass for each stale group.
		SchedulerStaleGroups: daemonSchedulerStaleGroups,
		// Issue #2406: capture extractorCfg at construction time so the closure
		// owns an immutable pointer — no package-level singleton needed.
		SchedulerIncremental: func(ctx context.Context, repoPath string, ref string) sched.IncrementalResult {
			return daemonSchedulerIncremental(ctx, repoPath, ref, &extractorCfg)
		},
		// #5710 follow-up: cheap entity count for the "indexer: completed" log so
		// a silent 0-entity completion (empty-graph store recreation) is visible.
		// Reads the graph.fb header (no entity materialization); -1 when the
		// materialized graph is absent/unreadable.
		SchedulerEntityCount: func(repoPath string, ref string) int {
			stateDir := daemon.StateDirForRepoRef(repoPath, ref)
			if stateDir == "" {
				stateDir = daemon.StateDirForRepo(repoPath)
			}
			if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
				return ps.Entities
			}
			return -1
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

		// #5690: publish the scheduler's read-only warming accessor so the MCP
		// surface (grafel_whoami / grafel_status) can report whether a group is
		// still warming (post-index enrichment in flight) vs a slow query.
		OnSchedulerReady: setDaemonWarmingFn,

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

	// ADR-0024 Phase 1: dispatch to the serve or engine entrypoint. Both
	// currently execute the identical in-process daemon.Run body while the
	// capability flag is off (the default); PR2 diverges these behind the
	// real process split. See daemon.RunServe / daemon.RunEngine docs.
	switch runMode {
	case daemonRunModeEngine:
		return daemon.RunEngine(ctx, daemon.EngineConfig{Config: cfg})
	default:
		return daemon.RunServe(ctx, daemon.ServeConfig{Config: cfg})
	}
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

// daemonSchedulerStaleGroups returns the names of registered groups whose
// group-algo overlay EXISTS on disk, has gone stale relative to the per-repo
// graph.fb mtimes, AND whose community-input graph actually CHANGED (#5403 +
// #5655). It powers the scheduler's periodic overlay-freshness sweep so a
// SETTLED group (no recent reindex → no link pass → no scheduleGroupAlgo) still
// gets its overlay recomputed — but ONLY when a recompute would change anything.
// OverlayNeedsRecompute applies the content gate (graph.CommunityInputHash): a
// mere mtime drift whose community input is identical (a docs/comment/config
// push, or an idle re-stat) settles the overlay in place and is NOT returned
// here, so the sweep no longer fires a ~½-core Louvain burst on an idle daemon.
//
// FIRST COMPUTE (#6002). It ALSO returns a group that has member graphs on disk
// but NO overlay at all. That case used to be deliberately excluded, on the
// reasoning that "those take the normal first-compute link-pass chain after
// their first reindex" — but that chain does not cover the path a user actually
// takes. `grafel rebuild` / `grafel reset` runs its own IN-PROCESS link pass
// (daemonRebuildFuncCore → daemonForegroundLinks); it never goes through the
// scheduler, so runLinks — the only caller of scheduleGroupAlgo — never runs and
// no pass is ever armed. With the sweep also excluding the group (no overlay
// file), a freshly reset group could sit indefinitely with a complete, queryable
// graph and no communities at all, until some unrelated watcher-driven reindex
// happened to fire the chain. That is exactly the "silently dropped" failure the
// background overlay must not have: taking the pass off the rebuild's critical
// path is only acceptable if something reliably picks it up afterwards.
//
// The extra work is bounded: the sweep skips groups with a pending/in-flight
// pass, and a successful pass writes an overlay, so this fires once per group
// and then falls back to the (content-gated) staleness predicate. A group with
// no indexed member yet is excluded — assembling an empty union would be pure
// waste, and it will be picked up on the sweep after its first index.
//
// Best-effort throughout: any registry error yields an empty list (sweep skips
// this tick) rather than wedging.
func daemonSchedulerStaleGroups() []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	var out []string
	for _, g := range groups {
		if groupAlgoSweepWants(g.Name, groupalgo.OverlayNeedsRecompute, groupHasOverlay, groupHasIndexedMember) {
			out = append(out, g.Name)
		}
	}
	return out
}

// groupAlgoSweepWants is the testable core of the sweep predicate: recompute a
// stale-and-changed overlay, or compute a missing one for a group that has at
// least one indexed member. The three probes are injected so the decision can be
// tested without a registry, a $GRAFEL_HOME, or real graph files.
func groupAlgoSweepWants(group string, needsRecompute, hasOverlay, hasIndexedMember func(string) bool) bool {
	if needsRecompute(group) {
		return true
	}
	if hasOverlay(group) {
		return false // present and not stale-with-changed-content → nothing to do.
	}
	return hasIndexedMember(group)
}

// groupHasOverlay reports whether a group has a USABLE overlay on disk.
//
// Usable, not merely present: a CORRUPT overlay (truncated write from a killed
// process, a hand-edited file) is the last state that could still leave a group
// with no communities forever. OverlayNeedsRecompute returns false on an
// unmarshal failure — deliberately, so the sweep does not thrash on garbage —
// and a bare os.Stat would report the file as present, so both gates would say
// "nothing to do" and the backstop would never fire. OverlayAlgoVersionOnDisk
// reports ok=false for absent AND unparseable, which routes corruption into the
// first-compute path that overwrites it. Still bounded: one pass, after which a
// valid overlay exists.
//
// An unresolvable overlay path reports true — "unknown" must not force-fire a
// heavy pass.
func groupHasOverlay(group string) bool {
	path, err := groupalgo.OverlayPath(group)
	if err != nil || path == "" {
		return true
	}
	_, ok := groupalgo.OverlayAlgoVersionOnDisk(group)
	return ok
}

// groupHasIndexedMember reports whether at least one of a group's repos has a
// materialized graph. CurrentSourceMtimes omits repos with no graph yet, so a
// non-empty map is exactly "something to assemble".
func groupHasIndexedMember(group string) bool {
	mt, err := groupalgo.CurrentSourceMtimes(group)
	if err != nil {
		return false
	}
	return len(mt) > 0
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
	// Memoize git common-dir resolution within this call: a monorepo has many
	// slugs whose paths resolve to the same root, and even distinct paths may
	// repeat, so resolve each abs path at most once.
	commonDir := map[string]string{}
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
			// Dedup on (group, git common-dir), NOT (group, path). In a
			// monorepo the N module slugs are N distinct subdir paths of ONE
			// git root; they all share a single git common-dir and therefore
			// report the SAME `git worktree list`. Keying on the common-dir
			// collapses them to a single representative parent (the first slug
			// seen) instead of N parents that each re-discover the same
			// worktrees — the #5675 reindex storm. Independent repos have
			// distinct common-dirs and so remain distinct parents.
			root, ok := commonDir[abs]
			if !ok {
				root = gitmeta.ResolveCommonDir(abs)
				commonDir[abs] = root
			}
			// Fall back to path-keyed dedup when common-dir resolution fails
			// (path is not a git repo / git unavailable) so nothing is dropped.
			keyBase := root
			if keyBase == "" {
				keyBase = abs
			}
			key := g.Name + "\x00" + keyBase
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
// daemonSchedulerIncremental is the scheduler's S3 incremental-reindex
// callback, lifted out of the config literal so it can be driven directly by a
// test. Keeping it inline made the #5964 seed-verification guard below
// unreachable from any test: deleting the guard left every suite green.
func daemonSchedulerIncremental(ctx context.Context, repoPath string, ref string, extractorCfg *extractor.ExtractorConfig) sched.IncrementalResult {
	// Issue #5719: ref can be "" (unknown at enqueue time). Resolve it to
	// HEAD via ResolveIncrementalStateDir instead of encoding the empty ref
	// as the "refs/_unknown/" sentinel — otherwise the incremental pass can
	// never find the real graph and falls back forever.
	stateDir := daemon.ResolveIncrementalStateDir(repoPath, ref)
	// #5964: a worktree state dir may have been SEEDED from its parent ref's
	// graph. Before that seed is consumed, prove it is still the generation it
	// claims to be — a seed whose graph and diff manifest straddle two parent
	// generations makes files that really differ read as unchanged, so their
	// stale entities survive into the child's graph. That is worse than slow:
	// it is invisible. On any failed check the seed is discarded (leaving
	// nothing resolvable, so a full index runs) and the reason is reported,
	// never swallowed.
	if reason, ok := verifyOrDiscardSeed(stateDir); !ok {
		return sched.IncrementalResult{Done: false, FallbackReason: "seed_" + reason}
	}
	// Use the caller-supplied ctx (the scheduler's shutdownCtx) so that daemon
	// SIGTERM cancels any in-flight incremental subprocess — matching the fix
	// applied to runIndex in issue #2176/#2491. Fixes issue #2495.
	res := extractors.TryIncremental(ctx, repoPath, stateDir, nil, extractorCfg)
	if res.Done {
		invalidateAfterIndex(repoPath)
		tierAfterIndex(repoPath, ref)
	}
	return sched.IncrementalResult{
		Done:           res.Done,
		FallbackReason: res.FallbackReason,
		ChangedFiles:   res.ChangedFiles,
	}
}

// verifyOrDiscardSeed re-checks any #5964 worktree seed sitting in stateDir
// before a pass consumes it. It returns (reason, false) when the seed could
// not be trusted — the caller must then run a FULL index and surface the
// reason — and ("", true) otherwise, which includes the overwhelming common
// case of a state dir that was never seeded at all.
//
// On success the stamp is consumed immediately: from here the pass owns the
// dir and will write a new generation into it, so a stamp left behind would
// make the NEXT verification report generation_moved against the child's own
// legitimate graph.
func verifyOrDiscardSeed(stateDir string) (string, bool) {
	stamp, reason, _ := daemon.VerifySeededGraph(stateDir)
	// Branch on the VERDICT, not on `reason != ""`. A superseded stamp — the
	// dir's pointer already names a generation newer than the seed, i.e. the
	// child built its own graph over it — is benign and must not be discarded,
	// or a full corpus reindex the child paid for is thrown away.
	if !daemon.SeedVerdictIsBenign(reason) {
		if derr := daemon.DiscardSeed(stateDir); derr != nil {
			slog.Default().Warn("worktree: failed to discard an untrusted seed",
				"state_dir", stateDir, "reason", string(reason), "err", derr)
		}
		slog.Default().Warn("worktree: seeded graph rejected — running a full index",
			"state_dir", stateDir,
			"reason", string(reason),
			"parent_ref", seedStampParentRef(stamp),
			"parent_generation", seedStampParentGen(stamp))
		return string(reason), false
	}
	if stamp != nil {
		if cerr := daemon.ConsumeSeedStamp(stateDir); cerr != nil {
			slog.Default().Warn("worktree: failed to consume seed stamp",
				"state_dir", stateDir, "err", cerr)
		}
		if reason == daemon.SeedFallbackSuperseded {
			slog.Default().Info("worktree: stale seed stamp cleared — the child's own graph is newer",
				"state_dir", stateDir,
				"parent_ref", stamp.ParentRef,
				"seed_generation", stamp.ParentPointer)
		} else {
			slog.Default().Info("worktree: seeded graph verified — indexing the delta only",
				"state_dir", stateDir,
				"parent_ref", stamp.ParentRef,
				"parent_generation", stamp.ParentPointer,
				"repo_tag", stamp.RepoTag)
		}
	}
	return "", true
}

func seedStampParentRef(s *daemon.SeedStamp) string {
	if s == nil {
		return ""
	}
	return s.ParentRef
}

func seedStampParentGen(s *daemon.SeedStamp) string {
	if s == nil {
		return ""
	}
	return s.ParentPointer
}

// resolveIndexRepoTag returns the repo tag an index of stateDir's repo must be
// pinned to, or "" to keep Index()'s default (filepath.Base(repoPath)).
//
// #5964: graph.EntityID hashes the repo tag FIRST, and for a worktree the
// default is the WORKTREE directory's own name, which disagrees with the
// parent's slug. A full index of a worktree must use the same tag a seeded
// index would, or the two graphs have different entity ids: parity between
// them becomes untestable and a later seed can never be correct. Empty for
// every non-worktree repo, preserving today's default exactly.
//
// A separate named seam so a test can pin the forwarding without indexing.
func resolveIndexRepoTag(stateDir string) string {
	return daemon.ReadRepoTagPin(stateDir)
}

func daemonSchedulerIndex(ctx context.Context, repoPath string, ref string) error {
	var err error
	stateDir := daemon.ResolveIncrementalStateDir(repoPath, ref)
	repoTag := resolveIndexRepoTag(stateDir)
	// #5964: a FULL index also consumes a seeded state dir — it merges nothing,
	// it simply overwrites — and it is the ONLY path taken when incremental
	// reindexing is disabled (GRAFEL_INCREMENTAL_REINDEX=0), where the
	// scheduler never calls the incremental callback that would otherwise
	// consume the stamp. Left behind, that stamp goes stale the moment this
	// index writes its own generation. Consuming it here closes the second of
	// the two paths that can reach a seeded dir.
	defer func() {
		if cerr := daemon.ConsumeSeedStamp(stateDir); cerr != nil {
			slog.Default().Warn("worktree: failed to consume seed stamp after a full index",
				"state_dir", stateDir, "err", cerr)
		}
	}()
	// #6207 — POST-INDEX RECONCILE. This is a FULL index, and it must stay one:
	// it is reached when the incremental pass declined, i.e. precisely when the
	// delta could not be trusted, so it is never given --incremental /
	// WithIncremental. But a full index establishes ground truth for every file
	// in the repo, which is exactly what the manifest records — so it now
	// records it.
	//
	// Before this, neither branch below asked for a manifest, i.incremental
	// stayed false and index.go skipped the write entirely. The manifest on the
	// scheduler path was therefore advanced only by TryIncremental, which is
	// why its reject path has to carry loop guards (#5667, #5668) to stop the
	// identical changed set re-presenting on every subsequent pass.
	//
	// NEITHER BRANCH NAMES A DESTINATION, and `stateDir` above deliberately is
	// not one. stateDir is resolved from `ref`, the ref captured at ENQUEUE
	// time; the graph is written wherever gitmeta resolves HEAD at INDEX time
	// (the `_ = ref` comment below has always said so). A checkout landing in
	// that window would put the manifest and the graph it describes under
	// different refs — one branch left with no baseline, the other with a
	// manifest stamped from the wrong branch. The indexer writes the manifest
	// beside its own graph instead, so the two cannot diverge by construction.
	//
	// The sweep is O(repo) hashing on a path that has just spent seconds
	// parsing that same repo — noise here, as opposed to ~30% of a 738 ms
	// no-op, which is where the equivalent sweep currently lands (#6206).
	if sched.SubprocessIndexEnabled() {
		// S5 path: fork-exec `grafel index-internal` for memory isolation.
		//
		// THE CHILD WRITES THE MANIFEST, not the parent. The child owns the walk
		// — the gitignore-aware traversal, the extension filters, the skipped
		// files — so it is the only side that knows the set that was actually
		// indexed. Re-deriving that set in the parent means a second walk that
		// has to reproduce every one of those filters and will drift the first
		// time one of them changes, and it would decouple the manifest from the
		// graph generation the child wrote: a manifest claiming files the graph
		// does not contain is worse than no manifest at all. The child is also
		// the only side that can ORDER the manifest write after its own graph
		// write, which is what stops a failed graph write from publishing a
		// manifest for a graph that never landed.
		opts := &sched.SubprocessIndexOptions{PersistManifest: true}
		if repoTag != "" {
			// RepoSlug is forwarded to the child as --repo-tag. No publisher is
			// set, so no other subprocess flag changes.
			opts.RepoSlug = repoTag
		}
		err = subprocessIndexRunner(ctx, repoPath, ref, []string{"graph-algo"}, opts, slog.Default())
	} else {
		// In-process path (opt-out via GRAFEL_SUBPROCESS_INDEXER=0).
		// ADR-0016 flip-day (#808): graph.fb is always written by default now.
		// ref is stored via StateDirForRepo → StateDirForRepoRef at write time;
		// the indexer itself resolves the correct path via gitmeta at index time.
		_ = ref
		err = Index(repoPath, "", repoTag, []string{"graph-algo"}, false, false,
			WithManifestPersist())
	}
	// Drop the cached mmap so the next MCP query reopens against the
	// freshly written graph.fb. Done on both success and failure paths
	// — a stale handle is worse than a cold miss.
	invalidateAfterIndex(repoPath)
	// PH2 (#2090): register / re-activate the tier slot as HOT after index.
	tierAfterIndex(repoPath, ref)
	return err
}

// subprocessIndexRunner is the fork-exec used by daemonSchedulerIndex. A var
// solely so a test can drive the child's real entrypoint with the parent's real
// argv (see sched.SubprocessIndexArgs) instead of forking a binary a test
// binary does not contain — which is the only way to assert on what the child
// WRITES rather than on which flags it was handed.
var subprocessIndexRunner = sched.RunSubprocessIndex

// subprocessLinksRunner is the fork-exec used by daemonSchedulerLinks. A var
// solely so a test can observe that the scheduler path forks (and that the
// foreground path does not) without spawning a real multi-minute child.
var subprocessLinksRunner = sched.RunSubprocessLinks

// daemonSchedulerLinks re-runs the cross-repo link passes for a group.
//
// By default the pass runs in a short-lived child process
// (`grafel links-internal <group>`) so its heap is isolated from the engine and
// reclaimed by the OS on exit (#5954; mirrors group-algo and the subprocess
// indexer). Until this change it was the last heavy stage still running inside
// the long-lived engine, and it showed: per-phase measurement put the pass
// window at ~830MB LIVE heap / ~1.2GB heap_inuse — a plateau the engine then
// held for the rest of its life, on top of everything else it is resident for.
// The child also inherits GOGC=50, GODEBUG=madvdontneed=1 and a GOMAXPROCS cap,
// none of which the in-process pass ever had.
// GRAFEL_SUBPROCESS_INDEXER=0 opts back into the in-process path.
//
// THE STAGE TOKEN SPANS THE FORK. The scheduler calls this while holding the
// daemon-wide EXCLUSIVE heavy-stage token (fireLinks → tryAcquireStageLocked,
// released by a defer around the call), so this function must not return until
// the child has exited — otherwise the token would be released with the heavy
// work still running and the gate would stop serialising the very stage it
// exists for. RunSubprocessLinks waits on the child, so the token covers the
// child's whole lifetime exactly as it does for group-algo.
//
// CANCELLATION. ctx is the scheduler's per-group cancel context (derived from
// shutdownCtx), so daemon shutdown or a CancelGroup on a group delete still
// stops the pass; across the fork the in-process ctx checks become a SIGKILL of
// the child's whole process group (#5999 — not a SIGTERM, and not the
// single-pid kill os/exec would do by default). More prompt than the boundary checks it replaces, at the cost of
// leaking the pass's staging temp dir, which stageGraphsDir sweeps.
func daemonSchedulerLinks(ctx context.Context, group string) error {
	if sched.SubprocessIndexEnabled() {
		return subprocessLinksRunner(ctx, group, slog.Default())
	}
	// In-process fallback (opt-out via GRAFEL_SUBPROCESS_INDEXER=0).
	return runLinksHookWithCtx(ctx, group)
}

// daemonForegroundLinks is the link step of a user-initiated rebuild, run
// synchronously inside daemonRebuildFuncCore.
//
// It deliberately does NOT fork (#5954). The rebuild is foreground work with a
// human waiting on it — the wizard's "Detecting cross-repo links…" phase — and
// it is already covered by the stage gate's barge, so the memory argument that
// justifies the child on the scheduler path does not apply here while the
// latency and progress-plumbing costs of a fork would. Kept as a named function
// rather than an inline closure so that this distinction is testable.
func daemonForegroundLinks(ctx context.Context, group string) error {
	return runLinksHookWithCtx(ctx, group)
}

// groupAlgoCapApply applies the resolved CPU bound around the in-process
// group-algo pass (#6108). It is process.WithGOMAXPROCSCap; a var only so a
// test can observe the cap the fallback ACTUALLY asks for and the GOMAXPROCS
// actually in force when the pass body runs — the assertion #6091 taught us to
// make behaviourally rather than by reading the source.
var groupAlgoCapApply = process.WithGOMAXPROCSCap

// daemonSchedulerGroupAlgo runs the group-scope algorithm pass ONCE over the
// assembled union of a group's per-repo graphs and writes the <group>-algo.json
// overlay (#5349 A3, epic #5350). It replaces the old per-repo daemonSchedulerAlgo:
// the scheduler chains this off the SUCCESS path of the link pass, so N repo
// reindexes coalesce into one link pass and then one group-algo pass.
//
// By default the pass runs in a short-lived child process
// (`grafel group-algo <group> --write`) so the heavy union-graph heap is
// isolated from the daemon and reclaimed on exit (plan Q2; mirrors the v0.1.1
// subprocess indexer). GRAFEL_SUBPROCESS_INDEXER=0 opts into the in-process
// path. The ctx is the scheduler's per-run cancel context (derived from
// shutdownCtx) so daemon SIGTERM or a superseding link pass cancels cleanly.
func daemonSchedulerGroupAlgo(ctx context.Context, group string) error {
	if sched.SubprocessIndexEnabled() {
		// Preferred path: fork-exec for heap isolation under the per-child CPU
		// cap. The child writes the overlay; the MCP apply path picks it up by
		// mtime on the next group load (no daemon-side cache to poke).
		//
		// The heartbeat covers this branch too: the child's own per-phase
		// memtrace is off unless GRAFEL_MEMTRACE is set, so without this the
		// daemon log goes silent between "starting" and the child's exit — which
		// on the real corpus is tens of minutes, and on a loaded host was
		// measured at ~4 hours (#6108).
		defer startGroupAlgoProgress(ctx, group, "child")()
		return sched.RunSubprocessGroupAlgo(ctx, group, slog.Default())
	}
	// In-process fallback (opt-out via GRAFEL_SUBPROCESS_INDEXER=0). Runs under
	// the scheduler's algoSem cap. The union heap is freed when the result goes
	// out of scope; no per-repo graph.fb is rewritten.
	//
	// #6108 — CPU BOUND. There is no child here to hand a GOMAXPROCS to, so the
	// pass used to run at the daemon's own GOMAXPROCS (= every core on the box)
	// while the scheduler logged the CHILD's cap. Resolve the SAME cap the child
	// would have been spawned with and apply it to this process for the duration.
	// Foreground groups resolve to the host core count, and WithGOMAXPROCSCap
	// never raises, so user-awaited work is left uncapped exactly as #5954
	// intends. See internal/process/gomaxprocs.go for why GOMAXPROCS is the
	// right lever for a pass with no goroutine fan-out of its own (it is the
	// GC's parallelism, not the algorithm's, that produced 571.9%).
	capN := sched.GroupAlgoGOMAXPROCSFor(sched.GroupIsForeground(group))
	defer startGroupAlgoProgress(ctx, group, "in-process")()
	return groupAlgoCapApply(capN, func() error {
		// #5309 layer 4 — incremental community detection: when the assembled
		// union's community-input hash matches the prior overlay's, the heavy
		// Louvain + PageRank + betweenness recompute is skipped and the prior
		// overlay is reconstituted verbatim (strict parity by determinism);
		// otherwise a full deterministic recompute runs (CPU-bounded by #5602).
		// Either way the overlay is re-written so its source_mtimes settle the
		// staleness gate.
		res, err := groupalgo.RunGroupAlgorithmsIncremental(group)
		if err != nil {
			return err
		}
		return groupalgo.WriteOverlayFromResult(res)
	})
}

// groupAlgoProgressInterval is how often a running group-algo pass reports
// progress. GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL overrides it; any non-positive
// duration ("0", "0s", "-1s") disables the heartbeat entirely.
//
// 60s is chosen against the failure it exists for: a pass with a ~40 minute
// expectation that ran for ~4 hours and emitted NOTHING after "starting", so an
// operator staring at a daemon holding six cores could not tell slow progress
// from a wedge. At 60s a healthy pass adds a handful of lines and a pathological
// one leaves a per-phase timeline. Anything much finer would spam the ring
// buffer that holds the daemon's recent log.
//
// groupAlgoProgressMin is a hard floor on the override. Without it
// GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL=1ns is accepted and the heartbeat spins a
// core logging — a diagnostic knob that becomes its own CPU incident. 10ms is
// low enough for a test to drive and high enough to cost nothing.
//
// groupAlgoStopBarrier (#6134) is the ceiling on how long stop() will wait for
// the heartbeat goroutine to exit. Generous relative to the goroutine's actual
// work (one CPU read plus one log line) so it never trips in normal operation,
// and short relative to a group-algo pass so a stalled log sink cannot wedge
// one. See the barrier note in startGroupAlgoProgress.
const (
	groupAlgoProgressInterval = time.Minute
	groupAlgoProgressMin      = 10 * time.Millisecond
	groupAlgoStopBarrier      = 2 * time.Second
)

func groupAlgoProgressEvery() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GRAFEL_GROUP_ALGO_PROGRESS_INTERVAL"))
	if raw == "" {
		return groupAlgoProgressInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		// A bare "0" is not a valid Go duration but is the conventional and
		// documented way to disable a knob, so honour it rather than silently
		// falling back to the default the operator was trying to turn off.
		if raw == "0" {
			return 0
		}
		return groupAlgoProgressInterval
	}
	if d <= 0 {
		return 0
	}
	if d < groupAlgoProgressMin {
		return groupAlgoProgressMin
	}
	return d
}

// startGroupAlgoProgress emits a periodic progress line for an in-flight
// group-algo pass and returns a stop function (call it, or defer it, exactly
// once).
//
// WHAT IT REPORTS AND WHY EACH FIELD IS THERE:
//
//   - phase — groupalgo.CurrentPhase(), the same stamp the memtrace sampler
//     reads, so the two can never disagree. Only meaningful on the in-process
//     branch; the child stamps its own copy in its own address space, so the
//     child branch reports the mode instead of a phase it cannot see. A phase
//     that has not advanced in twenty lines is what "wedged" looks like.
//   - elapsed — wall time since the pass started.
//   - cpu — percent of one core, averaged over the interval, derived from the
//     process's cumulative CPU seconds. This is the field that would have made
//     #6108 self-evident from the log alone: `cap=2` beside `cpu=571%` is the
//     whole bug in one line. On the child branch it is the DAEMON's own draw,
//     which is the number that matters for "is the daemon eating my machine".
//
// It never takes a lock the pass can block on and never fails the pass: a CPU
// read error simply drops the cpu field for that tick.
func startGroupAlgoProgress(ctx context.Context, group, mode string) func() {
	every := groupAlgoProgressEvery()
	if every <= 0 {
		return func() {}
	}
	logger := slog.Default()
	pid := os.Getpid()
	start := time.Now()
	lastWall := start
	lastCPU, cpuOK := 0.0, false
	if s, err := process.CPUTimeSeconds(pid); err == nil {
		lastCPU, cpuOK = s, true
	}

	done := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(exited)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case now := <-t.C:
				// #6134 — RE-CHECK THE STOP SIGNAL AFTER A TICK WINS.
				//
				// A Go select chooses UNIFORMLY AT RANDOM among the cases that
				// are ready. Once the ticker has a pending tick, an
				// already-closed `done` therefore wins only about half the
				// time — so the loop above would emit one more line after the
				// pass had ended, reporting elapsed time and a phase for work
				// that was already over. Measured: 3/3 runs, deterministic
				// enough to fail on a cycle-0 iteration, not a load artifact.
				//
				// That is the #6047 class (progress reported after the work it
				// describes has finished), which is why #6134 was NOT closed by
				// loosening the test: the test was right and the heartbeat was
				// wrong. The non-blocking re-check below makes the stop signal
				// win unconditionally once it is set.
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				default:
				}
				attrs := []any{
					"group", group,
					"mode", mode,
					"elapsed", time.Since(start).Truncate(time.Second).String(),
				}
				if phase := groupalgo.CurrentPhase(); phase != "" && mode != "child" {
					attrs = append(attrs, "phase", phase)
				}
				if s, err := process.CPUTimeSeconds(pid); err == nil {
					if wall := now.Sub(lastWall).Seconds(); cpuOK && wall > 0 {
						if pct := 100 * (s - lastCPU) / wall; pct >= 0 {
							attrs = append(attrs, "cpu_pct", int(pct))
						}
					}
					lastCPU, cpuOK = s, true
				}
				lastWall = now
				logger.Info("group-algo: progress", attrs...)
			}
		}
	}()
	// #6134 — stop() IS A BARRIER, NOT A REQUEST.
	//
	// Closing `done` only makes the goroutine eligible to exit; it says nothing
	// about whether it has observed the close, or whether it is mid-log. Callers
	// (and the wizard) treat the return of stop() as "this pass no longer
	// reports", so waiting for the goroutine to actually exit is what makes that
	// true. Combined with the re-check inside the loop, the guarantee is exact:
	// once stop() has returned, no further progress line for this pass can be
	// emitted, ever.
	//
	// THE WAIT IS BOUNDED, and an earlier revision of this comment argued it
	// did not need to be ("the goroutine's only blocking operation is the log
	// write"). That argument is only as good as the sink. Both call sites are
	// `defer startGroupAlgoProgress(...)()`, so an unbounded wait here blocks the
	// group-algo pass itself: a slog sink that stalls — tty flow control under
	// the wizard, a full pipe, a stuck filesystem — would convert what used to be
	// a benign leaked ticker goroutine into a wedged pass. That is a strictly
	// worse failure than the one being fixed, so the barrier takes a ceiling.
	//
	// Timing out only forfeits the barrier, never correctness: the in-loop
	// re-check above has already made a post-stop line impossible independently
	// of this wait, because it returns whenever `done`/ctx is closed and cannot
	// drop a legitimate line. The wait exists to make "stop() returned" mean
	// "the goroutine is gone", and a stalled sink is exactly the case where that
	// promise is not worth blocking a pass for.
	//
	// `exited` is closed by a defer, so a panic in the body releases the wait
	// too. Idempotent: the second call sees the once already fired and receives
	// from an already-closed channel.
	return func() {
		once.Do(func() { close(done) })
		select {
		case <-exited:
		case <-time.After(groupAlgoStopBarrier):
		}
	}
}

// daemonIndexFunc is the IndexFunc handed to daemon.Run. It bridges the
// RPC argument struct onto the existing in-process Index() entrypoint
// defined in this same package.
func daemonIndexFunc(args proto.IndexArgs) (string, string, error) {
	// #5970 FOREGROUND EXEMPTION. This runs Index() IN-PROCESS, inside the
	// daemon, synchronously under an RPC the user's CLI is blocked on — the same
	// judgement WithInteractive(true) below already encodes. The daemon's parse
	// gate is sized for BACKGROUND work (25% of the box, installDaemonParseCap),
	// so without this hold a `grafel index` would be throttled to a quarter of
	// the machine with a human waiting on it. Released on every exit path.
	defer indexstate.BeginForegroundParse()()

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
	// Context-aware so a group delete mid-rebuild cancels the (multi-minute)
	// cross-repo link/phantom pass instead of running it to completion for a
	// group that no longer exists (v0.1.8 leak fix). Runs IN-PROCESS — see
	// daemonForegroundLinks for why the #5954 fork is scoped to the
	// scheduler-driven pass only.
	linksFn := daemonForegroundLinks
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
// gate, when non-nil, is the daemon-wide index-concurrency gate (#5493). Every
// work item additionally acquires a gate slot before running, so concurrent
// indexes are bounded by min(conc, gate.Cap()) — in practice the gate (default
// 2) is the tighter throttle. foreground requests priority + the reserved slot.
// nil disables the gate (tests / legacy paths).
func rebuildWorkerPool(conc int, work []repoWork, workFn func(idx int, rw repoWork) repoResult, gate *daemon.IndexGate, foreground bool) []repoResult {
	results := make([]repoResult, len(work))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i, w := range work {
		wg.Add(1)
		go func(idx int, rw repoWork) {
			defer wg.Done()

			// Acquire the local pool slot. This MUST come before any panic-recovery
			// defer so the defer's <-sem only fires once the slot is actually held.
			sem <- struct{}{}
			defer func() { <-sem }()

			// #5493: also drain through the daemon-wide index-concurrency gate so
			// a many-module group cannot run more than GRAFEL_INDEX_CONCURRENCY
			// indexes at once, regardless of the (larger) local pool size. Excess
			// items block here and run as slots free.
			if gate != nil {
				if err := gate.Acquire(context.Background(), foreground); err != nil {
					results[idx] = repoResult{path: rw.r.Path, slug: rw.r.Slug, err: err}
					return
				}
				defer gate.Release(foreground)
			}

			results[idx] = workFn(idx, rw)
		}(i, w)
	}
	wg.Wait()
	return results
}

// rebuildSubprocessParams carries the per-repo inputs the subprocess rebuild
// path forwards to the `grafel index-internal` child.
type rebuildSubprocessParams struct {
	RepoPath  string
	RepoSlug  string
	GroupSlug string
	// RunToken is the rebuild's per-run identity (RebuildArgs.ProgressToken,
	// #5937), forwarded to the child so every republished progress.Event
	// carries Event.RunToken. Empty for rebuilds with no token.
	RunToken            string
	ProgressPub         progress.Publisher
	Interactive         bool
	IncrementalStateDir string
	// PersistManifest — #6208: forwarded to the child as --persist-manifest
	// whenever this repo's rebuild is NOT diff-aware (IncrementalStateDir ==
	// ""), i.e. a plain `grafel rebuild` or any `--wipe` rebuild. Those runs
	// walk and index every file but, before this, asked the child for neither
	// WithIncremental (which self-persists) nor WithManifestPersist, so they
	// left no baseline for the next incremental pass to diff against — the
	// same class of gap #6207 closed for the scheduler's fallback. See the
	// indexOneInner call site for the incremental-vs-persist decision.
	PersistManifest bool
}

// runRebuildSubprocess indexes one repo for a rebuild via the subprocess indexer
// (`grafel index-internal` child), republishing the child's per-module progress
// into p.ProgressPub. It is a package var so a test can stub the child spawn
// without exec'ing a real binary; production points it at the sched runner.
//
// Unlike the scheduler's reactive reindex it passes NO --skip-pass, so the
// rebuild produces the full graph (including the graph-algo pass) exactly as the
// in-process rebuild path does, and forwards ref="" so the child resolves HEAD
// via gitmeta just like the in-process indexer.
// Routed through subprocessIndexRunner (the same seam daemonSchedulerIndex
// uses) rather than calling sched.RunSubprocessIndex directly, so a test can
// intercept ONE level below this function's own opts-mapping and observe the
// real *sched.SubprocessIndexOptions this closure built — including
// PersistManifest (#6208) — instead of having to replace this whole function
// and reconstruct that mapping itself.
var runRebuildSubprocess = func(ctx context.Context, p rebuildSubprocessParams) error {
	return subprocessIndexRunner(ctx, p.RepoPath, "", nil, &sched.SubprocessIndexOptions{
		ProgressPub:         p.ProgressPub,
		GroupSlug:           p.GroupSlug,
		RepoSlug:            p.RepoSlug,
		RunToken:            p.RunToken,
		Interactive:         p.Interactive,
		IncrementalStateDir: p.IncrementalStateDir,
		PersistManifest:     p.PersistManifest,
	}, slog.Default())
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
	linksFn func(ctx context.Context, group string) error,
) ([]string, string, error) {
	// #5954 FOREGROUND BARGE. This rebuild indexes every repo DIRECTLY (via
	// indexFn → Index(...)) and then runs its OWN in-process cross-repo link
	// pass — it never calls scheduler.Enqueue, so s.inflight stays empty and the
	// heavy write-stage gate would read an IDLE machine for the entire 10–12
	// minutes. That is exactly what the post-#5993 measurement caught: peak rose
	// 4258 → 5430 MB with `index child 2979 MB + group-algo 1618 MB` co-resident.
	//
	// Registering here holds background link/group-algo passes off for the whole
	// rebuild — index batch AND link pass — at zero interactive cost: the barge
	// never waits, whatever the gate's state. It is deliberately the FIRST thing
	// in the function and released via `defer`, so every exit path (unknown
	// group, config error, group deleted mid-rebuild, extractor panic) unwinds
	// it; the closure is idempotent. Inert when no scheduler is running (CLI
	// one-shots, watcher-less daemons, tests) and when GRAFEL_STAGE_GATE=0.
	defer sched.BargeForeground("rebuild:" + args.Group)()

	// #5970 FOREGROUND EXEMPTION, in-process half — ON THE OPT-OUT PATH ONLY.
	// The barge and the per-group foreground registry below resolve caps for the
	// CHILDREN this rebuild spawns; neither touches the daemon's own
	// process-wide parse gate, which is sized for background work. That gate
	// binds this function only when indexFn runs IN-PROCESS, i.e. under
	// GRAFEL_SUBPROCESS_INDEXER=0. On the shipped default the per-repo index
	// forks (runRebuildSubprocess) and the child sizes its own gate from
	// --interactive, and daemonForegroundLinks does no tree-sitter parsing at
	// all (internal/links has no treesitter dependency) — so a hold here would
	// buy this rebuild nothing while suspending the 25% budget process-wide for
	// the whole 10-12 minute window.
	//
	// That asymmetry is the point: BeginForegroundParse lifts the cap for EVERY
	// concurrent acquirer, not just this call tree (see its doc). The
	// watcher-driven reindex keeps running throughout a rebuild — the barge
	// holds heavy write STAGES off, it does not gate index admission — so an
	// unconditional hold here would let background parsing run uncapped for the
	// duration on the one config where the hold does nothing. Bounded either
	// way (the scheduler's worker pool is 2 and TryIncremental extracts
	// sequentially), but "suspend the budget for 12 minutes to no benefit" is
	// not a trade worth making. A per-caller exemption that leaves the cap
	// standing for everyone else is the correct end state; tracked in #6022.
	if !sched.SubprocessIndexEnabled() {
		defer indexstate.BeginForegroundParse()()
	}

	// #5954 WALL TIME. The barge above tells the stage gate "hold background
	// stages off"; this tells every child spawn helper "a human is waiting on
	// THIS group, resolve foreground caps". They are separate signals on
	// purpose — see internal/daemon/sched/foreground.go. The barge lifts when
	// this function returns, but the stages that dominate the user's wall clock
	// (the scheduler's cross-repo link pass, and the debounced group-algo pass
	// that finishes the graph) are spawned minutes AFTER that, so a cap resolved
	// off the barge would still hand the user's rebuild the 2-core background
	// cap for its heaviest stage — the 10min → 30min+ regression.
	//
	// The mark lingers past this release and is closed by the group-algo pass
	// completing (sched.ClearGroupForeground in runGroupAlgo), with a time
	// backstop so a group whose group-algo never runs cannot keep drawing full
	// machine capacity for background churn.
	defer sched.MarkGroupForeground(args.Group)()

	rebuildStart := time.Now()
	fmt.Fprintf(os.Stderr, "grafel: rebuild start group=%s slug=%q wipe=%v incremental=%v\n",
		args.Group, args.Slug, args.Wipe, args.Incremental)
	defer func() {
		fmt.Fprintf(os.Stderr, "grafel: rebuild exit group=%s took=%s\n",
			args.Group, time.Since(rebuildStart).Truncate(time.Millisecond))
	}()

	// Per-group rebuild cancellation (v0.1.8 leak fix): register a cancelable
	// context so a `grafel delete <group>` (Service.DeleteGroup →
	// daemon.CancelGroupRebuild, or the engine's KindCancelGroup drain in split
	// mode) can interrupt this multi-minute loop instead of letting it index
	// every repo and run the cross-repo link/phantom passes to completion after
	// the group is gone. groupCtx roots the per-repo index contexts and is passed
	// to linksFn; it is checked at each repo boundary below. EndGroupRebuild
	// deregisters it on return so the entry never leaks.
	groupCtx, groupCancel, endGroupRebuild := daemon.GroupRebuildContext(args.Group)
	defer groupCancel()
	defer endGroupRebuild()

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

	// Split-mode progress bridge — WRITE side (ADR-0024 / epic #5729). In SPLIT
	// mode this rebuild runs inside the ENGINE process; the dashboard/wizard SSE
	// lives in the separate SERVE process reading serve's own broker, so events
	// published only into daemonProgressBroker here never reach it. Bridge them
	// by teeing the broker with a per-GROUP NDJSON SidecarWriter that serve's
	// sidecarTailer tails and republishes. progressPub is handed to BOTH the
	// per-repo indexer publisher AND the group link tracker so per-module Tick
	// events and the group-phase (cross-repo link) events all reach the sidecar.
	//
	// In MONOLITH mode (escape hatch GRAFEL_SPLIT_MODE=0) the publisher stays the
	// broker directly — no sidecar is created and nothing is written under
	// GRAFEL_HOME/progress, so monolith behavior is byte-for-byte unchanged.
	var progressPub progress.Publisher = daemonProgressBroker
	if daemon.SplitModeEnabled() {
		sidecarWriter, swErr := progress.NewSidecarWriter(args.Group)
		if swErr != nil {
			// Best-effort: a sidecar-writer failure must never fail the rebuild.
			// Fall back to the broker-only publisher (the split-mode dashboard
			// simply won't see live progress for this run).
			fmt.Fprintf(os.Stderr,
				"grafel: rebuild: progress sidecar writer for group=%s failed: %v (dashboard progress bridge disabled for this run)\n",
				args.Group, swErr)
		} else {
			// Flush the terminal + join the goroutine at the end of the rebuild.
			defer sidecarWriter.Close()
			progressPub = progress.NewTeePublisher(daemonProgressBroker, sidecarWriter)
		}
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

	perRepoTimeout := resolvePerRepoRebuildTimeoutWithOverride(args.RepoTimeout)

	// #5328: classify on the interactive-vs-automatic axis, NOT slug-vs-group.
	// A human-awaited request (dashboard wizard index, explicit CLI
	// index/rebuild/wizard/repair) sets args.Interactive, regardless of whether
	// it targets one slug or a whole group. Only the automatic watcher/git-hook
	// reindex leaves it false. Foreground acquisitions get priority + the
	// reserved gate slot and the higher rebuild CPU cap so they aren't starved
	// behind a many-module background storm. (The old `args.Slug != ""` heuristic
	// miscategorised a wizard whole-group index as background and a watcher
	// single-slug reindex as foreground — exactly backwards.)
	foreground := args.Interactive

	// indexOne executes the index function for a single repo and returns its
	// result. It is shared by both the serial and parallel paths so the logic
	// (panic recovery, wipe, incremental opts, progress slugs, slug tag) stays
	// in one place. ctx bounds the SUBPROCESS child's lifetime: indexOne cancels
	// it on the per-repo timeout (and on normal completion), so a wedged child is
	// killed and the repolock claim released rather than leaking as a live
	// process. The in-process indexFn takes no context, so its "orphaned
	// goroutine finishes in the background" semantics are unchanged.
	indexOneInner := func(ctx context.Context, idx int, rw repoWork) repoResult {
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
			var incrementalStateDir string
			if args.Incremental && !args.Wipe {
				incrementalStateDir = daemon.StateDirForRepo(rw.r.Path)
			}
			// #6208 — a rebuild that is NOT diff-aware (plain `grafel rebuild`, or
			// ANY `--wipe` rebuild — args.Wipe forces incrementalStateDir empty
			// above regardless of args.Incremental) still walks and indexes every
			// file, and that is exactly what a manifest records. Before this it
			// asked for neither WithIncremental (which self-persists via
			// i.persistManifest, see index.go) nor WithManifestPersist, so the
			// original daemon.go:2090-2092 gap left the next incremental pass with
			// no baseline to diff against — the same conflation-shaped hole #6207
			// closed on the scheduler's fallback path, left out of that change's
			// scope and filed as #6208.
			//
			// --wipe DECISION (#6208, argued, not defaulted): a manifest is state,
			// and --wipe exists to discard state — so does a wiped rebuild want a
			// fresh manifest or none at all? Fresh wins. The manifest's job is to
			// describe "what got indexed just now" so the NEXT incremental pass has
			// a baseline; it does not describe or preserve anything about the wipe
			// itself, and a wipe that leaves no manifest behind just forces the
			// very next pass (incremental or not) to fall back to a full walk
			// anyway — paying wipe's cost twice for no benefit. Refusing to write
			// one here would not make the wipe "more thorough"; it would only make
			// the next incremental pass redo the walk --wipe already paid for. So
			// persistManifest below is unconditional on incrementalStateDir == "",
			// not narrowed to !args.Wipe.
			//
			// RETRY BUDGETS GO WITH IT (#6209). The manifest a wiped rebuild
			// writes is fresh, so every per-file consecutive-extraction-failure
			// count starts again at whatever THIS run produced. That is the same
			// answer as above for the same reason: the count is state describing
			// previous passes, --wipe exists to discard exactly that, and the run
			// doing the discarding has just re-extracted every file itself, so
			// its own result is better evidence than the history it replaces. The
			// cost of being wrong is bounded — a file that still fails
			// deterministically spends its budget again over the next few passes
			// and goes quiet — and --wipe is user-initiated, never a daemon tick.
			//
			// ORDERING (verified, not assumed): commitManifest (index.go) is called
			// ONLY from the success branch of writeGraphGen inside Index /
			// runIndexInternal — i.e. after wipe already ran (wipe is the very
			// first thing this closure does, above) and after the fresh graph is
			// durably on disk. There is no path from here through
			// WithManifestPersist/PersistManifest that reaches commitManifest
			// before either the wipe or the graph write, so the manifest always
			// describes the POST-wipe index, never one carried across it.
			persistManifest := incrementalStateDir == ""
			// Cross-path mutual exclusion (#5729 concurrency bug): this rebuild
			// indexes the repo DIRECTLY (in-process OR via the subprocess child)
			// and never touches the engine scheduler, so the scheduler's per-repo
			// in-flight guard cannot see it. Without a shared claim, a
			// scheduler-enqueued reindex of the same repo (e.g. from the
			// wizard-installed `grafel watch` process, a git-HEAD switch, or a
			// drained KindReindex request) races this rebuild, both rewriting the
			// same graph.fb — the runaway re-index the live daemon exhibited.
			// ClaimForeground takes PRIORITY: the scheduler yields the repo while
			// we own it; we only block on a background index already mid-write.
			// Acquired/released INSIDE this index goroutine so the claim tracks the
			// index's real completion — a rebuild whose outer per-repo timeout
			// fires but whose goroutine keeps running still holds the claim until
			// the write finishes, and the release is idempotent (safe alongside the
			// panic-recovery defer above). Held across BOTH paths so it also spans
			// the child subprocess lifetime.
			releaseClaim := repolock.DefaultRegistry.ClaimForeground(rw.r.Path)
			defer releaseClaim()
			// Flush the status-plane sidecar immediately on claim acquisition so a
			// reader (statusline widget, wizard live-progress) observes
			// Indexing=true at index START instead of waiting up to a heartbeat
			// interval (defaultStatusHeartbeatInterval, 5s) for the async
			// statusWriter to notice the foreground claim via
			// repolock.HasForegroundClaim. releaseClaim (deferred above) always
			// runs, and flushRebuiltStatus/the error-path flush below always runs
			// AFTER this closure returns (see indexOneInner), so the terminal
			// flush reads the claim as already released and correctly reports
			// Indexing=false.
			daemon.FlushRepoStatusFile(rw.r.Path)

			if sched.SubprocessIndexEnabled() {
				// #5729 follow-up: route the per-repo index through the SAME
				// short-lived `grafel index-internal` child the scheduler uses, so
				// the index runs in a fresh near-empty-heap process (no GC
				// contention with the daemon's resident serving graphs). The child
				// republishes per-module progress over stdout into progressPub (so
				// the wizard bars are preserved), runs at the foreground CPU cap for
				// human-awaited rebuilds, and a child index_error surfaces here as a
				// per-repo failure — identical partial-failure semantics to an
				// in-process error. The PARENT still owns everything around the
				// index: the repolock claim above, the group-level linksFn, and the
				// status-before-ack FlushRepoStatusFile. When the toggle is OFF the
				// in-process path below runs unchanged.
				indexErr = runRebuildSubprocess(ctx, rebuildSubprocessParams{
					RepoPath:            rw.r.Path,
					RepoSlug:            rw.r.Slug,
					GroupSlug:           args.Group,
					RunToken:            args.ProgressToken,
					ProgressPub:         progressPub,
					Interactive:         foreground,
					IncrementalStateDir: incrementalStateDir,
					PersistManifest:     persistManifest,
				})
				return
			}

			var opts []IndexOption
			if incrementalStateDir != "" {
				opts = append(opts, WithIncremental(incrementalStateDir))
			} else if persistManifest {
				// #6208: not diff-aware, but still record what got indexed —
				// WithIncremental above already does this internally
				// (i.persistManifest = true), so this is the branch that needs
				// the explicit ask. Gated on the SAME persistManifest variable
				// the subprocess branch above uses (rather than re-deriving
				// incrementalStateDir == "" here) so the two branches cannot
				// silently diverge on the --wipe decision.
				opts = append(opts, WithManifestPersist())
			}
			// Publish granular per-repo progress into the shared broker so the
			// WebUI Index step renders live rows + file counters (#1531).
			opts = append(opts,
				WithPublisher(progressPub),
				WithProgressSlugs(args.Group, rw.r.Slug),
				WithRunToken(args.ProgressToken))
			// #5328: run at the foreground (higher) CPU cap only for human-awaited
			// rebuilds; an automatic watcher/git-hook-triggered rebuild stays at
			// the throttled background cap. This appears AFTER indexFn's prepended
			// default so it is the effective WithInteractive value.
			opts = append(opts, WithInteractive(foreground))
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
		// Short-circuit if the group was deleted mid-rebuild: skip the remaining
		// repos immediately (each returns a cancelled result) so the loop unwinds
		// within seconds instead of indexing every remaining member of a group
		// that no longer exists (v0.1.8 leak fix).
		if groupCtx.Err() != nil {
			return repoResult{
				path: rw.r.Path,
				slug: rw.r.Slug,
				err:  fmt.Errorf("rebuild cancelled (group deleted): %w", groupCtx.Err()),
			}
		}
		// Per-repo cancellable context, rooted at groupCtx so a group delete
		// (which cancels groupCtx) SIGKILLs the in-flight subprocess child too.
		// Cancelling it (on timeout below, group delete, or normal completion via
		// defer) SIGKILLs a wedged subprocess child so its parent goroutine
		// unblocks from cmd.Wait and releases the repolock claim — no
		// process/claim leak. The in-process path ignores ctx.
		ctx, cancel := context.WithCancel(groupCtx)
		defer cancel()
		if perRepoTimeout <= 0 {
			return indexOneInner(ctx, idx, rw)
		}
		t0 := time.Now()
		done := make(chan repoResult, 1)
		go func() { done <- indexOneInner(ctx, idx, rw) }()
		timer := time.NewTimer(perRepoTimeout)
		defer timer.Stop()
		select {
		case res := <-done:
			return res
		case <-timer.C:
			// Cancel the child so it is killed now and the goroutine + claim are
			// reclaimed promptly (defer cancel() also covers this, but doing it
			// explicitly documents intent). The in-process path's goroutine still
			// finishes in the background — ctx cancellation is a no-op for it.
			cancel()
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
		// --- Serial path ---. Each repo still drains through the daemon-wide
		// gate so that even serial group rebuilds running concurrently (up to
		// MaxConcurrentGroups) cannot collectively exceed GRAFEL_INDEX_CONCURRENCY.
		results = rebuildWorkerPool(1, work, indexOne, daemonIndexGate, foreground)
	} else {
		// --- Parallel path: delegate to rebuildWorkerPool ---
		results = rebuildWorkerPool(conc, work, indexOne, daemonIndexGate, foreground)
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
			// #5822 sub-ask 3 / review follow-up: persist a "last rebuild
			// FAILED" marker so a genuine failure (watchdog SIGKILL, subprocess
			// crash, panic, hard rebuild error) is never silent — previously
			// discarded entirely, leaving `grafel status` showing the stale
			// previous index with no trace beyond daemon.err.
			//
			// An INTENTIONAL cancellation is NOT a failure and must not record
			// the marker: the "group deleted mid-rebuild" short-circuit above
			// wraps groupCtx.Err() (context.Canceled) into its error, and
			// markers key purely by absolute repoPath, so a path shared across
			// two groups would otherwise surface a bogus FAILED line under the
			// OTHER, still-alive group after this one is deleted. This also
			// protects future work that cancels-and-supersedes in-flight
			// rebuilds by design (a clean stop, not a failure).
			//
			// context.DeadlineExceeded is deliberately NOT treated as a
			// cancellation here: the per-repo watchdog timeout above returns a
			// plain (unwrapped) "timed out" error, not one satisfying
			// errors.Is(_, context.DeadlineExceeded) or context.Canceled, so it
			// always falls through and records — a real hang must stay visible.
			if errors.Is(res.err, context.Canceled) {
				continue
			}
			// gitmeta.Capture reports the ref/sha this rebuild WAS targeting
			// (best-effort; empty on a non-git directory).
			gi := gitmeta.Capture(res.path)
			daemon.RecordRebuildFailure(res.path, res.err.Error(), gi.Ref, gi.SHA)
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

	// flushRebuiltStatus synchronously writes each successful repo's status-plane
	// sidecar (fresh graph_fb_mtime, Indexing=false) inline, BEFORE this function
	// returns — i.e. before the split-mode drain writes the request ack. This
	// closes the status-write-vs-ack race (#5729 blocker #5): a wizard keying
	// completion on the rebuild-request ack then sees a FRESH GraphFBMtime on its
	// first classify poll instead of a stale one for up to a heartbeat interval.
	// Best-effort per repo; never affects the rebuild result. Also clears any
	// stale "last rebuild FAILED" marker (#5822 sub-ask 3) — a repo in paths
	// just completed a SUCCESSFUL rebuild, so a prior watchdog/hard-failure
	// marker must not linger and keep reporting FAILED.
	flushRebuiltStatus := func(paths []string) {
		for _, repoPath := range paths {
			daemon.ClearRebuildFailure(repoPath)
			daemon.FlushRepoStatusFile(repoPath)
		}
	}

	// Return a combined error if any repos failed. The rebuilt list still
	// contains all repos that succeeded, so the caller can report partial results.
	if len(errs) > 0 {
		// Flush the SUCCESSFUL repos' fresh status before the (early) error
		// return so the wizard classifies them as indexed-OK, not false-failed.
		flushRebuiltStatus(rebuilt)
		return rebuilt, "", fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	// #5729 wizard "graph queryable" early completion: flush each successful
	// repo's fresh status (graph_fb_mtime + Indexing=false) NOW — after every repo
	// indexed and BEFORE the in-process linksFn tail (~10-12 min) and the ack — so
	// a wizard keying completion on "every repo advanced" (AllAdvanced) sees fresh
	// status at ~6 min instead of waiting the whole rebuild. This flush happens
	// under the repolock claim's completed writes, so a stale/spoofed mtime cannot
	// trip AllAdvanced. Also invalidate the resident cache for the rebuilt repos
	// now so an MCP query issued right after the wizard prints "Done" sees the
	// freshly written graph.fb (deterministic queryability). linksFn still runs
	// below (it writes the MCP-tool sidecars and cross-repo links — must NOT be
	// skipped); the post-linksFn flush + invalidate remain, re-capturing any
	// graph.fb linksFn rewrites in multi-repo groups.
	flushRebuiltStatus(rebuilt)
	for _, repoPath := range rebuilt {
		invalidateAfterIndex(repoPath)
	}

	// #5334 — surface the GROUP-level cross-repo link pass as its own granular
	// phase. This runs after every member repo is indexed (their per-repo rows
	// are already terminal), so without a group-level event the wizard's overall
	// label would sit at "Done" while the link pass is still churning. We emit a
	// group-scoped event (RepoSlug == group) so both the wizard's aggregate
	// label and the CLI's live line advance to "Detecting cross-repo links…".
	// The phantom-edge pass re-runs process-flow inside linksFn, so the same
	// phase also covers the group-level flow recompute.
	linkTrk := progress.NewTracker(progressPub, args.Group, args.Group)
	linkTrk.SetRunToken(args.ProgressToken)
	linkTrk.Phase(progress.PhaseDetectLinks, "cross-repo links", 0)

	// Cross-repo link passes run after every member is indexed. Skip them
	// entirely if the group was deleted mid-rebuild — running the multi-minute
	// phantom-edge / link recompute for a group that no longer exists is exactly
	// the CPU burn the v0.1.8 delete-cancellation fix removes. groupCtx is passed
	// through so a delete that lands DURING the pass also interrupts it.
	warning := ""
	if groupCtx.Err() != nil {
		warning = fmt.Sprintf("link passes skipped: rebuild cancelled (group deleted): %v", groupCtx.Err())
	} else if err := linksFn(groupCtx, args.Group); err != nil {
		// Best-effort — surface as a warning, not a hard failure.
		warning = fmt.Sprintf("link passes failed: %v", err)
	}
	// Terminalize the group-level link row so the wizard's feed-terminal gate
	// (rowsTerminal) is not blocked by a perpetually non-terminal group row.
	// expectedRepos counts member repos only, so the wizard tolerates this
	// extra group row reaching done.
	if warning != "" {
		linkTrk.Fail(warning)
	} else {
		linkTrk.Done(0, 0)
	}

	// Explicitly invalidate the cache for each rebuilt repo (#2607).
	// Belt-and-braces: the LRU cache's mtime safety-net has 1s granularity
	// which can race when rebuild completes faster. Explicit invalidation
	// ensures the next MCP query sees the freshly written graph.fb.
	for _, repoPath := range rebuilt {
		invalidateAfterIndex(repoPath)
	}

	// Flush fresh per-repo status AFTER linksFn (which may rewrite graph.fb) and
	// the cache invalidation, so the sidecar captures the newest graph_fb_mtime,
	// BEFORE returning (and thus before the split-mode ack). See #5729 blocker #5.
	flushRebuiltStatus(rebuilt)

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

// buildAgentsMapStats reads the per-repo graph artefacts produced by the
// just-completed index and assembles the Stats struct passed to
// agents.InjectArchitectureMap. It is intentionally best-effort — any read
// failure yields a zero-valued field rather than an error.
//
// #5954: this was the FOURTH full graph.LoadGraphFromDir per repo per rebuild —
// ~608 MB of materialised heap for a 325 MB graph, to produce six integers. It
// is now a kind-only streaming tally over the mmap (analytics.TallyRepo), which
// materialises nothing. The counts and the kind rules are unchanged; see
// analytics.TallyDocument, the materialised reference the tally is tested
// against.
func buildAgentsMapStats(group, repoPath string) agents.Stats {
	stateDir := daemon.StateDirForRepo(repoPath)

	s := agents.Stats{
		Group:         group,
		DashboardPort: resolveDefaultDashboardPort(),
	}

	if t, err := analytics.TallyRepo(stateDir); err == nil {
		s.Entities = t.Entities
		s.Relationships = t.Relationships
		s.HTTPEndpoints = t.HTTPEndpoints
		s.Queues = t.Queues
		s.Topics = t.Topics
		s.ProcessFlows = t.ProcessFlows
	}

	return s
}

// daemonQualityAuditFunc is the QualityAuditFunc handed to daemon.Run.
// It calls audit.AuditPath (in this process — the daemon process) and
// serialises the result into the wire reply.
//
// #5954: it audits with ONE worker, not audit's CLI default of four. A corpus
// audit's fan-out is peak heap — each worker holds a full graph.Document — and
// this runs inside the long-lived daemon, on behalf of an RPC that is not a
// latency-critical path. The report is byte-identical at any pool size
// (auditMany writes into a pre-sized slice by index).
func daemonQualityAuditFunc(args proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
	rep, err := audit.AuditPathWithWorkers(args.RepoPath, args.Corpus, 1)
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
// $GRAFEL_HOME (or ~/.grafel)/groups/<group>-patterns/. Groups whose
// patterns are stored in a custom MemoryDir (MCP registry config) will be
// found there by the MCP server; the daemon uses the default path which
// covers production deployments.
//
// #6178 round 3: was a plain os.UserHomeDir() join, ignoring GRAFEL_HOME —
// the same shape internal/mcp/patterns.go and
// internal/dashboard/handlers_patterns.go independently hand-rolled too.
// links.PatternsDir is the shared derivation all three use now.
func daemonPatternGroupDirs() map[string]string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(groups))
	for _, g := range groups {
		dir, derr := links.PatternsDir("", g.Name)
		if derr != nil {
			continue
		}
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
