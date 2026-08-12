package sched

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/cajasmota/grafel/internal/executil"
	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/progress"
)

// backgroundYieldGOMAXPROCSDefault is the per-child GOMAXPROCS a BACKGROUND
// (watcher/git-hook) reindex drops to WHILE a foreground (interactive) index is
// running (#5328). A human is waiting on the foreground index, so background
// work yields its core share to it instead of adding to it: 1 core keeps the
// background reindex making slow progress without competing for the foreground
// index's cores, so foreground+background together stay within the machine's
// budget. When no foreground index is active the background reindex runs at its
// normal cap (the child's own GOMAXPROCS, i.e. ReindexGraphPhaseGOMAXPROCS).
// Restored automatically the moment the foreground index finishes — the
// decision is made per-subprocess at launch and re-evaluated for each
// subsequent reindex.
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
// The extract sub-subprocesses it spawns — only on the opt-in
// GRAFEL_SUBPROC_EXTRACT path — are bounded by GRAFEL_EXTRACT_GOMAXPROCS
// (default 1 since #5960), but the GRAPH-WIDE PHASES that run IN the
// index-internal process itself (and, by default, the extraction too) —
// resolution, cross-repo links, flow, buildIndex/classification, plus
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

// ForegroundReindexGOMAXPROCS resolves the child-process GOMAXPROCS a
// human-awaited (interactive) rebuild / wizard first-index runs under. Because a
// user is actively waiting on it, it runs at host speed rather than the
// throttled background reindex budget: GRAFEL_REBUILD_GOMAXPROCS wins (a
// strictly-positive integer), otherwise the host core count. It mirrors the
// extract coordinator's rebuildGOMAXPROCS() default so the child process ceiling
// (graph-wide phases + GC) matches the foreground extract cap the child spawns
// its sub-subprocesses at.
func ForegroundReindexGOMAXPROCS() int {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_REBUILD_GOMAXPROCS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return n
}

// resolveChildGOMAXPROCS decides the GOMAXPROCS the index-internal child process
// runs under, and a short reason string for the daemon log.
//
//   - interactive (human-awaited rebuild): the FOREGROUND cap so the child's
//     graph-wide phases + GC run at host speed. foregroundCap wins when > 0,
//     else ForegroundReindexGOMAXPROCS(). The #5328 yield / #5602 budget only
//     apply to BACKGROUND reindexes, never to the index the user is waiting on.
//   - background reindex: unchanged — the #5328 foreground-yield cap while a
//     foreground index is active, otherwise the #5602 daemon-wide budget-per-slot.
func resolveChildGOMAXPROCS(interactive bool, foregroundCap int) (n int, reason string) {
	if interactive {
		if foregroundCap <= 0 {
			foregroundCap = ForegroundReindexGOMAXPROCS()
		}
		return foregroundCap, "foreground rebuild"
	}
	if y, yield := backgroundYieldGOMAXPROCS(); yield {
		return y, "yielding to foreground index"
	}
	return ReindexGraphPhaseGOMAXPROCS(), "daemon-wide reindex CPU ceiling"
}

// backgroundBatchCoreDivisor / backgroundBatchCoreFloor define the BACKGROUND
// per-child CPU cap shared by the two heavy batch children (group-algo, links):
//
//	cap = clamp(NumCPU/8, floor 2, NumCPU)
//
//	 1-core → 1     4-core → 2    12-core → 2
//	 8-core → 2    32-core → 4    64-core → 8
//
// WHY PROPORTIONAL. Both children used to be a hardcoded 2. That number is
// defensible on a 12-core laptop and absurd on a 64-core server, and it
// contradicts the standing policy, which is explicitly proportional: "cap it at
// max of 25% of the machine capacity, so it will not be a static number and
// bigger machines will get better times" (#5960, the same reasoning as
// process.IndexCoreBudget).
//
// WHY /8 AND NOT /4. process.IndexCoreBudget's 25% is the budget for the whole
// INDEXING fanout, which is many processes sharing it. These are single
// children, and the divisor was chosen so the value is UNCHANGED at 12 cores —
// the configuration the current caps were measured on — so this cannot regress
// the background behaviour on the machine that reported the problem.
//
// WHAT THIS IS NOT. It is not "the 25% rule". Be precise, because the user's
// directive was specific and this deviates from it in BOTH directions:
//
//   - Above 8 cores it is HALF of what the directive asks. At 32 cores the 25%
//     rule would give 8; this gives 4. That is a deliberate conservatism for a
//     single always-on background child, not a fulfilment of the policy, and if
//     the user wants literal 25% here the divisor is the one thing to change.
//   - Below 8 cores the FLOOR EXCEEDS 25%. On a 4-core box 2 cores is 50%; on a
//     2-core box it is 100%. A floor means exactly that — the proportional rule
//     is overridden at the small end so a background pass can still finish.
//
// So: proportional above 8 cores at 12.5%, floored at 2 below that.
//
// WHY A FLOOR OF 2. Below 2 the pass loses GC/runtime parallelism entirely and
// a background pass that never finishes is its own kind of failure. On a
// 1-core host the floor yields to the machine (clamped to NumCPU).
//
// WHAT THIS NUMBER ACTUALLY BUYS — CORRECTED (#6108). Both passes really are
// SERIAL: internal/links, internal/graph (including louvain.go),
// internal/graph/groupalgo and every gonum package in the dependency closure
// have no goroutine fan-out, and gonum's network.Betweenness even carries a
// literal "TODO: Consider using the parallel algorithm when GOMAXPROCS != 1". A
// live group-algo child shows one thread running and the rest asleep.
//
// This comment used to draw the inference that the cap therefore "buys very
// little" and should not be cited as a throughput control. THAT INFERENCE IS
// WRONG, and it is what let an unbounded in-process pass sustain 571.9% CPU
// inside the daemon while the scheduler logged cap=2 (#6108).
//
// "This value mostly sizes the GC's mark workers, not the pass" is the true
// half — and on this workload the GC's mark workers ARE the CPU. GC mark
// parallelism is sized by GOMAXPROCS, and idle mark workers are scheduled on
// every otherwise-idle P, so a single-threaded mutator on a 12-P runtime hands
// the collector 11 Ps to fill. Measured on a serial mutator over a
// pointer-dense retained heap (getrusage, total CPU seconds):
//
//	GOMAXPROCS=12   cpu=0.55s   311.1%
//	GOMAXPROCS= 3   cpu=0.38s   188.0%
//	GOMAXPROCS= 1   cpu=0.38s    99.2%
//
// The CPU SECONDS fall — the excess is idle-mark-worker overhead the cap
// deletes, not work redistributed. Serial does not imply cheap when the live
// set is a dense pointer graph, which is exactly what these passes retain.
//
// So this number binds TODAY, on real machines, and is load-bearing for the
// in-process caps in internal/process/gomaxprocs.go as well as for the children.
// It is a CPU control, not a throughput control: raising it does not make a
// serial pass faster, but lowering it genuinely reduces what the host gives up.
const (
	backgroundBatchCoreDivisor = 8
	backgroundBatchCoreFloor   = 2
)

// backgroundBatchGOMAXPROCSFor is the pure form of the background batch cap,
// taking the host core count explicitly so the policy stays unit-testable
// across core counts. Always >= 1.
func backgroundBatchGOMAXPROCSFor(numCPU int) int {
	if numCPU < 1 {
		numCPU = 1
	}
	n := numCPU / backgroundBatchCoreDivisor
	if n < backgroundBatchCoreFloor {
		n = backgroundBatchCoreFloor
	}
	if n > numCPU {
		n = numCPU
	}
	return n
}

// BackgroundBatchGOMAXPROCS is backgroundBatchGOMAXPROCSFor for this host.
func BackgroundBatchGOMAXPROCS() int {
	return backgroundBatchGOMAXPROCSFor(runtime.NumCPU())
}

// GroupAlgoGOMAXPROCSFor resolves the GOMAXPROCS cap applied to the group-algo
// subprocess, dispatching on whether a human is waiting on the result.
//
// Precedence, highest first:
//
//  1. GRAFEL_GROUP_ALGO_CPU (strictly-positive; 1 is valid) — an explicit
//     operator override is an escape hatch and is honoured in BOTH modes,
//     never clamped to either policy.
//  2. foreground — ForegroundReindexGOMAXPROCS(): GRAFEL_REBUILD_GOMAXPROCS
//     if set, else the host core count. Deliberately the SAME knob the extract
//     coordinator and the index child already use for foreground rebuilds
//     (#5135) rather than a fourth env var, so an operator has one dial for
//     "how much machine may a rebuild take".
//  3. background — BackgroundBatchGOMAXPROCS().
//
// The pass (Louvain + PageRank + betweenness over the whole group union) is the
// heaviest analytics job the daemon runs; without any cap the child inherits
// the daemon's GOMAXPROCS and the Go runtime spins one worker thread per core —
// the v0.1.3 CPU regression where it pinned a 12-core machine at 500–1000% for
// hours. That regression was BACKGROUND churn, which is why the cap stays there
// and lifts only for work the user explicitly asked for and is blocking on.
func GroupAlgoGOMAXPROCSFor(foreground bool) int {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_GROUP_ALGO_CPU")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if foreground {
		return ForegroundReindexGOMAXPROCS()
	}
	return BackgroundBatchGOMAXPROCS()
}

// GroupAlgoGOMAXPROCS is the BACKGROUND resolution. Kept as the no-argument
// spelling for callers that are unambiguously background work.
func GroupAlgoGOMAXPROCS() int { return GroupAlgoGOMAXPROCSFor(false) }

// groupAlgoChildGOMAXPROCS is the spawn-site resolution for one group: it reads
// the foreground registry (see foreground.go) rather than taking a bool, so the
// signal reaches the fork-exec without threading a parameter through the
// scheduler's timer/gate machinery — which has no idea who triggered the work.
func groupAlgoChildGOMAXPROCS(group string) int {
	return GroupAlgoGOMAXPROCSFor(GroupIsForeground(group))
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
// push). The subprocess path forks `grafel index-internal`, whose GOMAXPROCS
// RunSubprocessIndex sets from resolveChildGOMAXPROCS — the #5328 yield cap
// while a foreground index is active, else the #5602 daemon-wide reindex
// ceiling (ReindexGraphPhaseGOMAXPROCS). NOT GRAFEL_EXTRACT_GOMAXPROCS: that
// env var is read only by the extract coordinator, which this path does not
// invoke unless the opt-in GRAFEL_SUBPROC_EXTRACT=1 is set (see
// cmd/grafel/index.go). By default index-internal extracts IN-PROCESS across
// i.workers goroutines, so its GOMAXPROCS plus the in-process tree-sitter
// parse gate — not the extract fanout caps — are what bound it. Either way
// background reindexes cannot saturate the host on a fresh `curl|bash`
// install that sets no env vars. It also keeps the daemon heap
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

// SetSubprocessIndexEnabled overrides the toggle at runtime and returns the
// previous value so a caller can restore it. Exposed for tests that need to
// force one path or the other (the rebuild reroute is gated on this toggle, so
// the in-process iteration tests force it OFF and the subprocess-reroute test
// forces it ON, each restoring the prior value on cleanup).
func SetSubprocessIndexEnabled(v bool) (previous bool) {
	return subprocessIndexerEnabled.Swap(v)
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
// Cancellation: ctx.Done() SIGKILLs the child's whole process group — the
// child plus every `grafel extract` subprocess its coordinator forked (#5999).
// It is NOT a SIGTERM: an earlier version of this comment said so and was
// simply wrong, and nothing on this path may depend on the child getting a
// chance to run deferred cleanup.
// SubprocessIndexArgs renders the complete argv (argv[0] == "index-internal")
// that RunSubprocessIndex fork-execs for the given job. It is a separate,
// exported function purely so the child's contract can be exercised without a
// fork: a test can hand this argv straight to the child entrypoint and observe
// what the child actually does with it, instead of asserting on flag strings —
// which proves only that a string was built, never that it was honoured.
//
// A nil opts (the scheduler background reindex before #6207) yields exactly
// argv{index-internal, --repo, --ref, --skip-pass}.
func SubprocessIndexArgs(repoPath, ref string, skipPasses []string, opts *SubprocessIndexOptions) []string {
	args := []string{
		"index-internal",
		"--repo=" + repoPath,
		"--ref=" + ref,
	}
	if len(skipPasses) > 0 {
		args = append(args, "--skip-pass="+strings.Join(skipPasses, ","))
	}
	// Progress-republish channel (rebuild / wizard first-index). When the caller
	// supplies a Publisher the child is told to STREAM per-module progress on
	// stdout (--emit-progress) and to stamp events with the rebuild's group/repo
	// slugs so republished rows key the same (group, repo, module) identity the
	// in-process path emits.
	if opts != nil {
		if opts.RepoSlug != "" {
			args = append(args, "--repo-tag="+opts.RepoSlug)
		}
		if opts.ProgressPub != nil {
			args = append(args, "--emit-progress")
			if opts.GroupSlug != "" {
				args = append(args, "--group-slug="+opts.GroupSlug)
			}
			// #5937: forward the per-run identity so every republished
			// progress.Event carries RunToken. Only meaningful alongside
			// --emit-progress (no publisher, no point tagging events nobody
			// reads); empty RunToken (no ProgressToken on this run) adds
			// nothing, matching --group-slug's own emptiness guard.
			if opts.RunToken != "" {
				args = append(args, "--run-token="+opts.RunToken)
			}
		}
		if opts.IncrementalStateDir != "" {
			args = append(args, "--incremental="+opts.IncrementalStateDir)
		}
		// #6207: manifest persistence WITHOUT diff-aware behaviour, and with no
		// destination — the child writes it beside the graph it produces.
		// --incremental already implies persistence, so setting both is
		// harmless and redundant rather than contradictory.
		if opts.PersistManifest {
			args = append(args, "--persist-manifest")
		}
		if opts.Interactive {
			args = append(args, "--interactive")
		}
	}
	return args
}

func RunSubprocessIndex(ctx context.Context, repoPath, ref string, skipPasses []string, opts *SubprocessIndexOptions, logger *slog.Logger) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("subprocess-indexer: resolve binary: %w", err)
	}

	args := SubprocessIndexArgs(repoPath, ref, skipPasses, opts)
	var progressPub progress.Publisher
	if opts != nil {
		progressPub = opts.ProgressPub
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	// Daemon's state dirs are inherited via the env (GRAFEL_DAEMON_ROOT,
	// GRAFEL_HOME). Start from the daemon's full environment so the child
	// resolves the same state dirs and caps. This is also how #5956
	// GRAFEL_MEMTRACE_DIR (+ GRAFEL_MEMTRACE_INTERVAL) reaches the
	// index-internal child: no dedicated flag is needed because the child
	// already inherits the parent's complete environment here.
	cmd.Env = os.Environ()
	// Resolve the child-process GOMAXPROCS. resolveChildGOMAXPROCS dispatches on
	// interactive-vs-background:
	//   - interactive (human-awaited rebuild / wizard first-index): the FOREGROUND
	//     cap (GRAFEL_REBUILD_GOMAXPROCS / host cores) so the child's graph-wide
	//     phases + GC run at host speed and the user is not throttled to the
	//     background reindex budget.
	//   - background reindex: unchanged — the #5328 foreground-yield cap while a
	//     foreground index is active, else the #5602 daemon-wide budget-per-slot.
	// GOMAXPROCS is appended last so it wins over any inherited value.
	interactive := opts != nil && opts.Interactive
	var foregroundCap int
	if opts != nil {
		foregroundCap = opts.ForegroundGOMAXPROCS
	}
	gmp, reason := resolveChildGOMAXPROCS(interactive, foregroundCap)
	cmd.Env = append(cmd.Env, "GOMAXPROCS="+strconv.Itoa(gmp))
	// #5954: GODEBUG is read once at process start, so the child cannot set
	// madvdontneed for itself the way it sets its own GOMEMLIMIT
	// (applyIndexMemoryLimit). Returning freed pages with MADV_DONTNEED rather
	// than MADV_FREE makes the RSS drop from that soft limit visible to the OS
	// (and to the operator) immediately instead of lazily under pressure.
	cmd.Env = withMadvDontNeed(cmd.Env)
	if logger != nil {
		logger.Info("subprocess-indexer: "+reason, "gomaxprocs", gmp, "repo", repoPath)
	}
	// Own process group + group-wide kill on cancellation (#5999). This child is
	// the one that actually fans out: the extract coordinator forks a `grafel
	// extract` subprocess per batch. Under the os/exec default Cancel — a
	// single-pid SIGKILL — every one of those extract processes survived the
	// cancellation, kept the inherited stderr pipe open, and so also kept this
	// runner blocked in its drain loop. Both call sites are inside the daemon
	// (no controlling terminal), so moving the child out of the daemon's process
	// group costs no signal delivery that anything relies on.
	applyGroupAlgoNice(cmd)
	// On Windows, prevent a console window from flashing when the daemon
	// (running as a Task Scheduler task) spawns this subprocess.
	executil.NoWindow(cmd)

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

	// Drain child stdout for IPC events. parseSubprocessStdout demuxes the two
	// line types: coarse lifecycle events (index_start/done/error → lastEvent for
	// exit classification) and tagged per-module progress lines, which it
	// republishes into progressPub so the rebuild's broker / split-mode sidecar
	// sees the same live rows the in-process indexer would have published.
	var lastEvent ipcEvent
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		lastEvent = parseSubprocessStdout(stdoutPipe, progressPub, pid, logger)
	}()

	// Wait for both pipe goroutines and the process itself. The drain ends at
	// EOF, which needs EVERY holder of the inherited descriptors gone; a
	// descriptor that escaped the killed process group would otherwise block
	// this runner forever. See boundPostCancelDrain.
	stopDrainBound := boundPostCancelDrain(ctx, stdoutPipe, stderrPipe)
	<-stdoutDone
	<-stderrDone
	stopDrainBound()
	waitErr := cmd.Wait()

	// #6107 / epic #5954: the index heap lives entirely in this child, so its
	// kernel high-water RSS is the ONLY honest peak figure for the run. Hand
	// it to runIndex (which writes the "indexer: completed" line) before
	// returning; the daemon's own sampler measures the wrong process on this
	// path and reports a near-zero delta. Recorded on the error path too — a
	// child that OOMed or was killed is precisely the run whose peak matters.
	recordChildPeakFromProcessState(repoPath, cmd.ProcessState)
	if logger != nil {
		if b, ok := maxRSSBytes(cmd.ProcessState); ok {
			logger.Info("subprocess-indexer: child peak RSS", "pid", pid,
				"repo", repoPath, "peak_rss_mb", int64(b/(1<<20)), "peak_rss_src", peakSrcChildMaxRSS)
		}
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
// this one) SIGKILLs the child's process group — see applyProcessGroupCancel,
// wired by applyGroupAlgoNice below. It is not a SIGTERM: an earlier version of
// this comment said SIGTERM and was simply wrong. The child gets no chance to run deferred
// cleanup, so nothing on the group-algo path may depend on a graceful shutdown;
// the overlay write is a temp+rename swap, so a kill mid-write leaves an orphan
// temp file and never a torn overlay.
//
// The child inherits the daemon's full environment (GRAFEL_HOME /
// GRAFEL_DAEMON_ROOT) so it resolves the same group config + state dirs and
// writes the overlay into the same ~/.grafel/groups directory.
// groupAlgoChildEnv builds the environment for the group-algo child: the
// GOMAXPROCS bound plus the madvdontneed reclaim setting, merged into any
// inherited GODEBUG. Extracted as a pure function so the constructed env is
// assertable without fork-execing anything.
//
// The GODEBUG entry is why this exists. GODEBUG is read ONCE at process start,
// so — unlike GOGC, which the child sets on itself in main() — madvdontneed can
// only be delivered through the child's environment. Without it the runtime
// returns freed pages with MADV_FREE and any RSS group-algo gives back stays
// invisible to the OS until the kernel comes under pressure, which is precisely
// what whole-machine peak measurement reads (#5954). The index child has had
// this since RunSubprocessIndex; group-algo was simply never given it.
//
// foreground also crosses here (#5954). The child renices ITSELF at startup, so
// the only way to stop a user-awaited pass running below the user's editor is
// to tell the child; see nice_foreground.go.
func groupAlgoChildEnv(base []string, gomaxprocs int, foreground bool) []string {
	env := make([]string, 0, len(base)+3)
	env = append(env, base...)
	env = append(env, "GOMAXPROCS="+strconv.Itoa(gomaxprocs))
	env = append(env, childForegroundEnvEntry(foreground))
	return withMadvDontNeed(env)
}

// groupAlgoChildBinary resolves the executable to fork for the group-algo
// child. A var for the same reason as linksChildBinary: a test can substitute a
// stand-in and exercise the fork / cancel contract without running the real
// pass.
var groupAlgoChildBinary = os.Executable

func RunSubprocessGroupAlgo(ctx context.Context, group string, logger *slog.Logger) error {
	binary, err := groupAlgoChildBinary()
	if err != nil {
		return fmt.Errorf("subprocess-group-algo: resolve binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, binary, "group-algo", group, "--write")
	// Bound the child's Go runtime so a BACKGROUND analytics pass cannot scale
	// its worker pool to the full host core count — the v0.1.3 CPU regression.
	// groupAlgoChildGOMAXPROCS resolves that from the foreground registry: a
	// pass the user is waiting on (their rebuild's follow-on group-algo) runs at
	// host speed, background churn stays on the proportional background cap.
	// GOMAXPROCS is appended last so it wins over any inherited value.
	// groupAlgoChildEnv also merges GODEBUG=madvdontneed=1 — see its doc.
	//
	// Resolved ONCE here and used for both the cap and the nice signal, so the
	// two cannot disagree about what kind of work this is.
	foreground := GroupIsForeground(group)
	gomaxprocs := GroupAlgoGOMAXPROCSFor(foreground)
	cmd.Env = groupAlgoChildEnv(os.Environ(), gomaxprocs, foreground)
	// Put the child in its own process group so it is independently schedulable.
	// This hook does NOT set priority — despite its name it only sets Setpgid;
	// the nice increment is applied by the CHILD to itself at startup, and since
	// #5954 only when it was not told it is foreground
	// (sched.NiceSelfUnlessForeground, delivered via childForegroundEnv above).
	// No-op on platforms without process groups (Windows).
	applyGroupAlgoNice(cmd)
	// On Windows, prevent a console window from flashing when the daemon
	// (running as a Task Scheduler task) spawns this subprocess.
	executil.NoWindow(cmd)

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

	// A descriptor that escaped the killed process group would otherwise hold
	// this drain — and the heavy write-stage token — open forever. See
	// boundPostCancelDrain.
	stopDrainBound := boundPostCancelDrain(ctx, stdoutPipe, stderrPipe)
	<-stdoutDone
	<-stderrDone
	stopDrainBound()
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

// LinksGOMAXPROCSFor resolves the GOMAXPROCS cap applied to the cross-repo link
// child. Mirrors GroupAlgoGOMAXPROCSFor exactly — same precedence, same
// foreground/background split — with GRAFEL_LINKS_CPU as its operator override.
//
// The background default used to be a hardcoded 2. That value shipped with ZERO
// measured wall-time delta behind it (it was carried over from the group-algo
// child by analogy), so it is replaced here by the proportional
// BackgroundBatchGOMAXPROCS, which is identical at 12 cores and better on
// larger hosts. Until #5954 this pass ran IN-PROCESS in the engine at the
// engine's full GOMAXPROCS with no cap at all, so the background cap remains a
// tightening, not a loosening.
func LinksGOMAXPROCSFor(foreground bool) int {
	if raw := strings.TrimSpace(os.Getenv("GRAFEL_LINKS_CPU")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if foreground {
		return ForegroundReindexGOMAXPROCS()
	}
	return BackgroundBatchGOMAXPROCS()
}

// LinksGOMAXPROCS is the BACKGROUND resolution.
func LinksGOMAXPROCS() int { return LinksGOMAXPROCSFor(false) }

// linksChildGOMAXPROCS is the spawn-site resolution for one group — see
// groupAlgoChildGOMAXPROCS.
func linksChildGOMAXPROCS(group string) int {
	return LinksGOMAXPROCSFor(GroupIsForeground(group))
}

// linksChildEnv builds the environment for the links child: the GOMAXPROCS
// bound plus the madvdontneed reclaim setting, merged into any inherited
// GODEBUG. Extracted as a pure function so the constructed env is assertable
// without fork-execing anything.
//
// The GODEBUG entry is the reason this is a function and not an inline append.
// GODEBUG is read ONCE at process start, so — unlike GOGC, which the child sets
// on itself in main() — madvdontneed can only be delivered through the child's
// environment. Without it the runtime hands freed pages back with MADV_FREE and
// the RSS the child gives up stays invisible to the OS until the kernel comes
// under pressure, which is exactly what whole-machine peak measurement reads
// (#5954). group-algo shipped without it and needed a follow-up; this child has
// it from its first commit.
//
// foreground crosses here too — see groupAlgoChildEnv.
func linksChildEnv(base []string, gomaxprocs int, foreground bool) []string {
	env := make([]string, 0, len(base)+3)
	env = append(env, base...)
	env = append(env, "GOMAXPROCS="+strconv.Itoa(gomaxprocs))
	env = append(env, childForegroundEnvEntry(foreground))
	return withMadvDontNeed(env)
}

// linksChildBinary resolves the executable to fork for the links child. It is a
// var solely so a test can substitute a stand-in and exercise the fork /
// cancel / argv contract without running the real multi-minute pass.
var linksChildBinary = os.Executable

// RunSubprocessLinks forks `grafel links-internal <group>` for one cross-repo
// link pass and waits for it to exit (#5954). It is the exact analogue of
// RunSubprocessGroupAlgo, for the same reason: the pass materialises the whole
// group union TWICE in sequence — once in links.RunAllPasses (every repo's
// Document, held live across ~19 passes) and again in the phantom-edge pass's
// docs map — and while it ran in-process the engine kept that arena for the
// rest of its life. Measured on the real corpus the pass window alone peaks at
// ~830MB LIVE heap (~1.2GB heap_inuse), and the engine held ~1.9GB for minutes
// after the index child had already exited. In a child the OS takes it all back
// on exit.
//
// SCOPE: the SCHEDULER-DRIVEN background pass only. The foreground rebuild's
// link step stays in-process — it is user-initiated and latency-sensitive, and
// is already covered by the stage gate's barge.
//
// STATE HANDED OVER: the group name, and nothing else. Everything the pass
// needs is re-derived in the child from the registry — the group config, each
// repo's on-disk path (which is what links.SetRepoSourcePaths is populated
// from, inside RunLinksForGroupCtx, at the start of every pass), and each
// repo's state dir. That works because the child inherits the daemon's full
// environment (GRAFEL_HOME / GRAFEL_DAEMON_ROOT) and therefore resolves the
// same registry and the same state dirs. The pass's process-global
// SetRepoSourcePaths is written before use on every entry, so the child's fresh
// globals are not a hazard — they are one less thing to invalidate.
//
// CONCURRENCY: the pass's temp files are now uniquely named per writer
// (#5978 — links.writeFileAtomic), so two concurrent writers to one
// destination no longer collide. Independently of that, the pass is
// serialised by the daemon's EXCLUSIVE heavy-stage token, which is held across
// this call for the child's whole lifetime, so at most one background link pass
// exists at a time — the same guarantee the in-process path had.
//
// CANCELLATION IS SIGKILL, NOT SIGTERM. ctx.Done() (daemon shutdown, or
// CancelGroup on a group delete) SIGKILLs the child's whole process GROUP:
// applyGroupAlgoNice below sets Setpgid and overrides cmd.Cancel accordingly
// (#5999), replacing os/exec's default single-pid cmd.Process.Kill() — which
// left anything the child forked alive, holding this runner's pipes open past
// the child's own death. That is more prompt than
// the in-process ctx checks it replaces (those only took effect at a pass
// boundary, which on this pass can be minutes away), and it is why the child
// installs no signal handler: under SIGKILL no handler could run.
//
// What that costs, precisely: the child cannot run deferred cleanup, so the
// pass's staging dir (os.MkdirTemp "grafel-links-*", a symlink/hardlink farm)
// is LEAKED on every cancellation. This is a real regression against the
// in-process path, where cancellation was cooperative and the deferred cleanup
// ran; it is not merely the pre-existing daemon-kill case. stageGraphsDir
// sweeps old strays at pass start to bound the accumulation.
//
// What it does NOT cost: output integrity. Every sink the pass writes —
// flows.WriteTo and graph.WriteLinkStats (writeJSONAtomic) — is
// write-temp-then-rename, and the flows temp name is pid-suffixed, so a killed
// child leaves at most an orphan .tmp and never a truncated or torn side-table.
func RunSubprocessLinks(ctx context.Context, group string, logger *slog.Logger) error {
	binary, err := linksChildBinary()
	if err != nil {
		return fmt.Errorf("subprocess-links: resolve binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, binary, "links-internal", group)
	// Foreground/background split — see groupAlgoChildGOMAXPROCS. One resolution
	// feeds both the CPU cap and the OS-priority signal.
	foreground := GroupIsForeground(group)
	gomaxprocs := LinksGOMAXPROCSFor(foreground)
	cmd.Env = linksChildEnv(os.Environ(), gomaxprocs, foreground)
	// Put the child in its own process group so it is independently schedulable;
	// the nice increment itself is applied by the child at startup, and only
	// when it was not told it is foreground (sched.NiceSelfUnlessForeground,
	// delivered via childForegroundEnv above). applyGroupAlgoNice is the shared
	// spawn-side hook for both background batch children — it is named for the
	// first one that needed it, not scoped to it.
	applyGroupAlgoNice(cmd)
	// On Windows, prevent a console window from flashing when the daemon
	// (running as a Task Scheduler task) spawns this subprocess.
	executil.NoWindow(cmd)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("subprocess-links: stderr pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("subprocess-links: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("subprocess-links: start: %w", err)
	}
	pid := cmd.Process.Pid
	if logger != nil {
		logger.Info("subprocess-links: started", "pid", pid, "group", group, "gomaxprocs", gomaxprocs)
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
	go drain(stdoutPipe, "[links]", stdoutDone)
	go drain(stderrPipe, "[links]", stderrDone)

	// A descriptor that escaped the killed process group would otherwise hold
	// this drain — and the heavy write-stage token — open forever. See
	// boundPostCancelDrain.
	stopDrainBound := boundPostCancelDrain(ctx, stdoutPipe, stderrPipe)
	<-stdoutDone
	<-stderrDone
	stopDrainBound()
	waitErr := cmd.Wait()

	if waitErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("subprocess-links: cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("subprocess-links: child exit: %w", waitErr)
	}
	if logger != nil {
		logger.Info("subprocess-links: completed", "pid", pid, "group", group)
	}
	return nil
}
