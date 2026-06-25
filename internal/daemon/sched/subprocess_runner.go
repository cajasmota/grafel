package sched

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// backgroundYieldGOMAXPROCSDefault is the per-child GOMAXPROCS a BACKGROUND
// (watcher/git-hook) reindex drops to WHILE a foreground (interactive) index is
// running (#5328). A human is waiting on the foreground index, so background
// work yields its core share to it instead of adding to it: 1 core keeps the
// background reindex making slow progress without competing for the foreground
// index's cores, so foreground+background together stay within the machine's
// budget. When no foreground index is active the background reindex runs at its
// normal cap (the child's own GRAFEL_EXTRACT_GOMAXPROCS, default 2). Restored
// automatically the moment the foreground index finishes — the decision is made
// per-subprocess at launch and re-evaluated for each subsequent reindex.
const backgroundYieldGOMAXPROCSDefault = 1

// BackgroundYieldGOMAXPROCS resolves the GOMAXPROCS a background reindex yields
// to while a foreground index is active, honouring
// GRAFEL_BACKGROUND_YIELD_GOMAXPROCS (a strictly-positive integer; 1 is valid).
// Unset, empty, non-numeric, or <= 0 → backgroundYieldGOMAXPROCSDefault.
func BackgroundYieldGOMAXPROCS() int {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_BACKGROUND_YIELD_GOMAXPROCS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return backgroundYieldGOMAXPROCSDefault
}

// backgroundYieldGOMAXPROCS returns the GOMAXPROCS the next background reindex
// subprocess should run under, given the live foreground-index state (#5328).
// It returns (n, true) — meaning "cap the child at n cores" — only when a
// foreground index is currently active; otherwise (0, false), and the child
// resolves its own normal background cap. Reading the published gate state keeps
// the sched package free of any cmd/grafel import cycle.
func backgroundYieldGOMAXPROCS() (int, bool) {
	if indexstate.GetIndexConcurrency().ForegroundActive > 0 {
		return BackgroundYieldGOMAXPROCS(), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Daemon-wide reindex CPU ceiling for the graph-wide phases (#5602).
//
// PROBLEM. A per-repo reindex runs as `grafel index-internal` (subprocess, S5).
// The extract sub-subprocesses it spawns are bounded by GRAFEL_EXTRACT_GOMAXPROCS
// (default 2), but the GRAPH-WIDE PHASES that run IN the index-internal process
// itself — resolution, cross-repo links, flow, buildIndex/classification, plus
// the Go GC that scales with GOMAXPROCS — run at the child's GOMAXPROCS. Today
// RunSubprocessIndex sets the child's GOMAXPROCS only when YIELDING to a
// foreground index (#5328); in the normal background case it sets nothing, so
// the child inherits the host core count (the daemon caps ITS OWN GOMAXPROCS via
// runtime.GOMAXPROCS, not via the env var the child reads).
//
// The IndexGate (#5493) bounds CONCURRENT reindexes to GRAFEL_INDEX_CONCURRENCY
// (default 2), but each admitted child then runs its graph-wide phases at the
// FULL host core count — so the ceiling is per-child, not daemon-wide:
//
//	total reindex CPU ≈ indexConcurrency × hostCores
//
// On a 12-core host with cap=2 that is ~24 cores — the live 200–1011% (#5602).
//
// FIX. Derive a single daemon-wide reindex CPU BUDGET (≈ half the host cores,
// the same ½-core policy as the daemon's own GOMAXPROCS and #5326) and split it
// across the concurrency slots, so the SUM over all concurrent reindexes of the
// per-child graph-phase GOMAXPROCS stays under the one budget regardless of how
// many children the IndexGate admits:
//
//	perChild = max(1, budget / indexConcurrency)
//
// With budget = hostCores/2 and indexConcurrency = 2 on a 12-core host this is
// max(1, 6/2) = 3 cores per child × 2 children = 6 cores total — a ceiling, not
// a per-child grant. Single-group reindex (one child) gets the whole budget, so
// throughput is not crippled. The foreground-yield cap (#5328) still takes
// precedence when a human-awaited index is running.

// ReindexBudgetEnv overrides the daemon-wide reindex CPU budget (total cores the
// graph-wide phases of ALL concurrent reindexes may use). A strictly-positive
// integer; unset/invalid → the ½-host-core default.
const ReindexBudgetEnv = "GRAFEL_REINDEX_CPU_BUDGET"

// reindexCPUBudget resolves the daemon-wide reindex CPU budget (#5602): the
// total cores the in-process graph-wide phases of ALL concurrent reindex
// children may collectively use. GRAFEL_REINDEX_CPU_BUDGET wins; otherwise the
// resource-safe default is ~half the host cores (floored at 1), matching the
// daemon's own GOMAXPROCS default policy.
func reindexCPUBudget() int {
	if raw := strings.TrimSpace(os.Getenv(ReindexBudgetEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	return n
}

// reindexConcurrency mirrors the daemon-package IndexGate cap
// (GRAFEL_INDEX_CONCURRENCY, default 2). The sched package cannot import
// internal/daemon (import cycle: daemon → sched), so the env knob is resolved
// here too — both read the SAME variable, so the value the budget is divided by
// matches the actual number of concurrent reindex slots the gate admits.
func reindexConcurrency() int {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_INDEX_CONCURRENCY")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 {
			return n
		}
	}
	return 2
}

// ReindexGraphPhaseGOMAXPROCS returns the per-child GOMAXPROCS to apply to a
// background reindex subprocess so the SUM of the graph-wide-phase CPU across all
// concurrent reindexes stays under the daemon-wide budget (#5602):
//
//	perChild = max(1, reindexCPUBudget() / reindexConcurrency())
//
// This is the daemon-wide ceiling: with the IndexGate admitting at most
// reindexConcurrency() children, total graph-phase parallelism is bounded by
// perChild × concurrency ≈ budget, instead of concurrency × hostCores.
func ReindexGraphPhaseGOMAXPROCS() int {
	budget := reindexCPUBudget()
	conc := reindexConcurrency()
	if conc < 1 {
		conc = 1
	}
	n := budget / conc
	if n < 1 {
		n = 1
	}
	return n
}

// groupAlgoGOMAXPROCSDefault is the per-child CPU cap (Go GOMAXPROCS) for the
// background group-algorithm subprocess. The pass (Louvain + PageRank +
// betweenness over the whole group union) is the heaviest analytics job the
// daemon runs. Without a cap the child inherits the daemon's GOMAXPROCS (= host
// core count) and the Go runtime spins one worker thread per core — the v0.1.3
// CPU regression where it pinned a 12-core machine at 500–1000% for hours.
//
// Default 2 mirrors GRAFEL_EXTRACT_GOMAXPROCS: "the less the better" for a
// background job. The user can set GRAFEL_GROUP_ALGO_CPU=1 to throttle it to a
// single core.
const groupAlgoGOMAXPROCSDefault = 2

// GroupAlgoGOMAXPROCS resolves the GOMAXPROCS cap applied to the group-algo
// subprocess, honouring GRAFEL_GROUP_ALGO_CPU (a strictly-positive integer; 1
// is valid). Unset, empty, non-numeric, or <= 0 → groupAlgoGOMAXPROCSDefault.
func GroupAlgoGOMAXPROCS() int {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_GROUP_ALGO_CPU")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return groupAlgoGOMAXPROCSDefault
}

// SubprocessIndexEnabled reports whether the daemon should run each
// reindex job as a short-lived child process (S5 of issue #2155) instead
// of calling the Index function in-process.
//
// Default logic (resource-safe defaults, v0.1.1):
//   - GRAFEL_SUBPROCESS_INDEXER=false/0/no → always OFF (opt-out)
//   - GRAFEL_SUBPROCESS_INDEXER=true/1/yes → always ON
//   - unset                                 → ON (default)
//
// Why default ON: the in-process path runs the reindex at the daemon's own
// GOMAXPROCS (= host core count) with no per-job CPU bound — the runaway the
// dogfooding report observed (300–998% CPU, ~10 cores, for 10–20 min per
// push). The subprocess path forks `grafel index-internal`, which the
// extract coordinator bounds to GRAFEL_EXTRACT_GOMAXPROCS (default 2) cores
// per child, so background reindexes cannot saturate the host on a fresh
// `curl|bash` install that sets no env vars. It also keeps the daemon heap
// flat (the original #2155 motivation). Operators who need the legacy
// in-process behaviour can still force it with GRAFEL_SUBPROCESS_INDEXER=0.
//
// The env var is read once at program start via init() to avoid per-call
// os.Getenv overhead in the hot admission loop.
var subprocessIndexerEnabled atomic.Bool

func init() {
	subprocessIndexerEnabled.Store(subprocessIndexEnabledFromEnv())
}

// subprocessIndexEnabledFromEnv resolves the default-on toggle from the
// process environment. Unset → ON; an explicit falsy value → OFF; any other
// value → ON. Exposed (lower-case) so tests can re-resolve after t.Setenv.
func subprocessIndexEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("GRAFEL_SUBPROCESS_INDEXER"))
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		// "", "1", "true", "yes", or anything else → default ON.
		return true
	}
}

// SubprocessIndexEnabled returns the current subprocess-indexer toggle
// value. Exposed for testing and the daemon status endpoint.
func SubprocessIndexEnabled() bool {
	return subprocessIndexerEnabled.Load()
}

// ipcEvent is one JSON line emitted by the child process on stdout.
type ipcEvent struct {
	Event string `json:"event"`
	Repo  string `json:"repo,omitempty"`
	Ref   string `json:"ref,omitempty"`
	Error string `json:"error,omitempty"`
}

// RunSubprocessIndex forks `grafel index --internal` for a single
// reindex job and waits for it to exit. The daemon stays at ~5MB extra
// overhead per in-flight reindex (IPC reader goroutine + wait state).
//
// Arguments:
//
//	ctx        — cancelled when the daemon wants to abort the job
//	repoPath   — absolute path of the repository
//	ref        — git ref captured at enqueue time (may be "")
//	skipPasses — pass names forwarded via --skip-pass
//	logger     — daemon's slog.Logger for structured event lines
//
// The child's stderr is copied line-by-line to logger (prefixed with the
// repo basename) so the daemon log file includes child extractor output
// without growing the daemon's own heap.
//
// Cancellation: ctx.Done() sends SIGTERM to the child. The child is
// expected to exit on SIGTERM; if it does not, the parent waits and the
// context timeout (if any) will eventually unblock the caller.
func RunSubprocessIndex(ctx context.Context, repoPath, ref string, skipPasses []string, logger *slog.Logger) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("subprocess-indexer: resolve binary: %w", err)
	}

	args := []string{
		"index-internal",
		"--repo=" + repoPath,
		"--ref=" + ref,
	}
	if len(skipPasses) > 0 {
		args = append(args, "--skip-pass="+strings.Join(skipPasses, ","))
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	// Daemon's state dirs are inherited via the env (GRAFEL_DAEMON_ROOT,
	// GRAFEL_HOME). Start from the daemon's full environment so the child
	// resolves the same state dirs and caps.
	cmd.Env = os.Environ()
	// #5328: if a FOREGROUND (interactive) index is running right now, make this
	// BACKGROUND reindex yield its core share to it by capping the child's Go
	// runtime to BackgroundYieldGOMAXPROCS() cores (default 1). GOMAXPROCS is
	// appended last so it wins over any inherited value; it is omitted entirely
	// when no foreground index is active, so the child falls back to its normal
	// background cap (GRAFEL_EXTRACT_GOMAXPROCS, default 2). This is re-evaluated
	// per subprocess launch, so background concurrency restores automatically the
	// moment the foreground index finishes. fg preempts bg's share rather than
	// adding to it, keeping fg+bg within the machine's core budget.
	if n, yield := backgroundYieldGOMAXPROCS(); yield {
		cmd.Env = append(cmd.Env, "GOMAXPROCS="+strconv.Itoa(n))
		if logger != nil {
			logger.Info("subprocess-indexer: yielding to foreground index",
				"gomaxprocs", n, "repo", repoPath)
		}
	} else {
		// #5602: daemon-wide reindex CPU ceiling. The child runs the in-process
		// graph-wide phases (resolution / links / flow / buildIndex) at ITS
		// GOMAXPROCS; without this it inherits the host core count, so N
		// concurrent reindexes admitted by the IndexGate draw N × hostCores and
		// blow past the intended ceiling (the live 200–1011%, #5602). Cap the
		// child at the budget-per-slot share so the SUM across all concurrent
		// reindexes stays under one daemon-wide budget. GOMAXPROCS is appended
		// last so it wins over any inherited value.
		n := ReindexGraphPhaseGOMAXPROCS()
		cmd.Env = append(cmd.Env, "GOMAXPROCS="+strconv.Itoa(n))
		if logger != nil {
			logger.Info("subprocess-indexer: daemon-wide reindex CPU ceiling",
				"gomaxprocs", n, "repo", repoPath)
		}
	}

	// Pipe child stdout for IPC JSON lines.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("subprocess-indexer: stdout pipe: %w", err)
	}
	// Pipe child stderr for log forwarding.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("subprocess-indexer: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("subprocess-indexer: start: %w", err)
	}

	pid := cmd.Process.Pid
	if logger != nil {
		logger.Info("subprocess-indexer: started", "pid", pid, "repo", repoPath, "ref", ref)
	}

	// Drain child stderr in a goroutine — each line forwarded to the daemon
	// log. This goroutine exits naturally when the child closes stderr (on
	// normal exit or crash).
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderrPipe)
		for sc.Scan() {
			if logger != nil {
				logger.Info("[child]", "pid", pid, "line", sc.Text())
			}
		}
	}()

	// Drain child stdout for IPC events.
	var lastEvent ipcEvent
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		sc := bufio.NewScanner(stdoutPipe)
		for sc.Scan() {
			var ev ipcEvent
			if jerr := json.Unmarshal([]byte(sc.Text()), &ev); jerr != nil {
				continue // not a JSON line — ignore
			}
			lastEvent = ev
			if logger != nil {
				logger.Info("subprocess-indexer: event", "event", ev.Event, "repo", ev.Repo, "ref", ev.Ref)
			}
		}
	}()

	// Wait for both pipe goroutines and the process itself.
	<-stdoutDone
	<-stderrDone
	waitErr := cmd.Wait()

	if logger != nil {
		if waitErr != nil {
			logger.Error("subprocess-indexer: exited with error", "pid", pid, "err", waitErr)
		} else {
			logger.Info("subprocess-indexer: completed successfully", "pid", pid)
		}
	}

	if waitErr != nil {
		// Distinguish context-cancellation (SIGTERM was sent by us) from a
		// genuine child failure.
		if ctx.Err() != nil {
			return fmt.Errorf("subprocess-indexer: cancelled: %w", ctx.Err())
		}
		if lastEvent.Error != "" {
			return errors.New(lastEvent.Error)
		}
		return fmt.Errorf("subprocess-indexer: child exit: %w", waitErr)
	}
	return nil
}

// RunSubprocessGroupAlgo forks `grafel group-algo <group> --write` for one
// group-scope algorithm pass and waits for it to exit (#5349 A3). Running the
// pass in a short-lived child isolates the heavy union-graph heap (gonum graph
// + betweenness scratch, ~300–600MB on a 28k-entity union per plan §2.2) from
// the daemon: the OS reclaims it on child exit, mirroring the v0.1.1 subprocess
// indexer (S5). The child writes the <group>-algo.json overlay; the daemon's
// MCP apply path picks up the fresh overlay by mtime on the next group load.
//
// Cancellation: ctx.Done() (daemon shutdown, or a newer link pass superseding
// this one) sends SIGTERM to the child via exec.CommandContext.
//
// The child inherits the daemon's full environment (GRAFEL_HOME /
// GRAFEL_DAEMON_ROOT) so it resolves the same group config + state dirs and
// writes the overlay into the same ~/.grafel/groups directory.
func RunSubprocessGroupAlgo(ctx context.Context, group string, logger *slog.Logger) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("subprocess-group-algo: resolve binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, binary, "group-algo", group, "--write")
	// Bound the child's Go runtime to GroupAlgoGOMAXPROCS() cores (default 2,
	// env GRAFEL_GROUP_ALGO_CPU) so the background analytics pass cannot scale
	// its worker pool to the full host core count — the v0.1.3 CPU regression.
	// GOMAXPROCS is appended last so it wins over any inherited value. Mirrors
	// the extract subprocess (GRAFEL_EXTRACT_GOMAXPROCS).
	gomaxprocs := GroupAlgoGOMAXPROCS()
	cmd.Env = append(os.Environ(), "GOMAXPROCS="+strconv.Itoa(gomaxprocs))
	// Lower the child's OS scheduling priority (nice +10) so even its capped
	// cores yield to foreground work (a consumer's CI / dev harness). No-op /
	// guarded off on platforms without setpriority (e.g. Windows). See
	// applyGroupAlgoNice (platform-split files).
	applyGroupAlgoNice(cmd)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("subprocess-group-algo: stderr pipe: %w", err)
	}
	// group-algo prints stats to stdout; forward it to the daemon log too.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("subprocess-group-algo: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("subprocess-group-algo: start: %w", err)
	}
	pid := cmd.Process.Pid
	if logger != nil {
		logger.Info("subprocess-group-algo: started", "pid", pid, "group", group)
	}

	drain := func(r interface{ Read([]byte) (int, error) }, tag string, done chan struct{}) {
		defer close(done)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			if logger != nil {
				logger.Info(tag, "pid", pid, "line", sc.Text())
			}
		}
	}
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go drain(stdoutPipe, "[group-algo]", stdoutDone)
	go drain(stderrPipe, "[group-algo]", stderrDone)

	<-stdoutDone
	<-stderrDone
	waitErr := cmd.Wait()

	if waitErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("subprocess-group-algo: cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("subprocess-group-algo: child exit: %w", waitErr)
	}
	if logger != nil {
		logger.Info("subprocess-group-algo: completed", "pid", pid, "group", group)
	}
	return nil
}
