package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cajasmota/grafel/internal/cli"
)

// main wires the daemon entrypoint (which owns indexing + linking +
// MCP) into the cobra dispatch tree owned by internal/cli, then
// delegates. Index, MCP, and rebuild used to be wired here as direct
// hooks; per ADR-0017 they are now thin RPC clients implemented inside
// internal/cli, and the only hook this package contributes is the
// long-running daemon mode (plus the per-group linker, which both the
// daemon and `grafel rebuild` need).
func main() {
	// Hidden verification harness for issue #1409 (not part of the public
	// command surface; intercepted before cobra dispatch).
	if len(os.Args) >= 2 && os.Args[1] == "xrepo-verify" {
		os.Exit(runXRepoVerify(os.Args[2:]))
	}
	// Hidden subprocess-indexer entrypoint (S5 of #2149 / issue #2155).
	// fork-exec'd by the daemon's subprocess runner; not part of the public
	// command surface and intentionally not registered with cobra.
	if len(os.Args) >= 2 && os.Args[1] == "index-internal" {
		// Bound this process's peak footprint before any indexing work starts
		// (#5954). Measured on the real corpus: 4026MB -> 3203MB peak RSS for
		// +1.7% wall time. Applied from inside the process rather than via a
		// GOMEMLIMIT env var so it holds no matter how the child was launched,
		// and applied HERE rather than inside runIndexInternal because
		// debug.SetMemoryLimit is a process-wide side effect and
		// runIndexInternal is also invoked in-process by the package's tests.
		// Never fatal.
		applyIndexMemoryLimit()
		// Bound GC PACING as well (#5954). The soft memory limit above is an
		// absolute target and is therefore floored well clear of the live heap;
		// GOGC is relative (next_gc = live * (1+GOGC/100)) and so does the
		// actual RSS trimming without any risk of the unsatisfiable-target
		// death spiral. os.Args[1] is passed explicitly so the "background
		// only, never interactive" rule is a property of the policy function
		// and not of this call site. Never fatal.
		applyIndexGCPercent(os.Args[1], argvIsInteractive(os.Args[2:]), os.Getenv(indexGCPercentEnv), os.Getenv("GOGC"))
		os.Exit(runIndexInternal(os.Args[2:]))
	}
	// Hidden group-level algorithm harness (#5349 A1 / epic #5350). Assembles
	// the union of a group's per-repo graphs and runs the algorithm pass ONCE
	// at group scope, printing stats. --dry-run writes no files. Not part of the
	// public command surface; intercepted before cobra dispatch.
	if len(os.Args) >= 2 && os.Args[1] == "group-algo" {
		// Bound GC PACING for this child too (#5954). Whole-machine measurement
		// after the index child was optimised showed the peak instant had moved
		// entirely post-index: the index child is at 0MB there while this
		// process is one of the two largest on the machine, running at the
		// GOGC=100 default and otherwise untuned. os.Args[1] is passed
		// explicitly so the "background only, never interactive" rule stays a
		// property of the policy function rather than of this call site.
		//
		// Deliberately NO applyIndexMemoryLimit here. GOMEMLIMIT is an absolute
		// soft target and this child's live heap is not yet measured; an
		// absolute target below live heap is the documented >4x death spiral
		// (see index_memlimit.go). GOGC is relative — live * (1+GOGC/100) — and
		// so cannot enter that regime by construction. Never fatal.
		// foreground=false unconditionally: the batch children keep GOGC=50
		// even when the pass they are running was caused by a foreground
		// rebuild. Their live heap is UNMEASURED, and a looser GC target on an
		// unmeasured heap is the death-spiral risk index_memlimit.go documents.
		// They get the wall-time win from the CPU cap lift, not from GC pacing.
		applyIndexGCPercent(os.Args[1], false, os.Getenv(indexGCPercentEnv), os.Getenv("GOGC"))
		os.Exit(runGroupAlgo(os.Args[2:]))
	}
	// Hidden cross-repo link child (#5954). The daemon's scheduler fork-execs
	// this instead of running the multi-minute link pass inside the long-lived
	// engine, where its ~830MB live / ~1.2GB heap_inuse arena became a plateau
	// the engine held for the rest of its life. Intercepted before cobra
	// dispatch; the public `grafel links pass <group>` is unaffected.
	//
	// Same GC pacing as the other two background children, and for the same
	// reason: nobody is waiting on it, so trading GC CPU for RSS is free.
	// Deliberately NO applyIndexMemoryLimit — GOMEMLIMIT is an absolute soft
	// target and an absolute target below the live heap is the documented >4x
	// death spiral (index_memlimit.go); GOGC is relative and cannot enter that
	// regime. Never fatal.
	if len(os.Args) >= 2 && os.Args[1] == backgroundLinksCommand {
		// foreground=false unconditionally: the batch children keep GOGC=50
		// even when the pass they are running was caused by a foreground
		// rebuild. Their live heap is UNMEASURED, and a looser GC target on an
		// unmeasured heap is the death-spiral risk index_memlimit.go documents.
		// They get the wall-time win from the CPU cap lift, not from GC pacing.
		applyIndexGCPercent(os.Args[1], false, os.Getenv(indexGCPercentEnv), os.Getenv("GOGC"))
		os.Exit(runLinksInternal(os.Args[2:]))
	}
	// Release acceptance ladder (#5224). Intercepted before cobra dispatch
	// because it owns its own argv, lifecycle, and exit code (it boots an
	// isolated in-process daemon and asserts each layer). Lives in cmd/grafel
	// (not internal/cli) since it wires the real Index + MCP function values.
	if len(os.Args) >= 2 && os.Args[1] == "selftest" {
		os.Exit(runSelftest(os.Args[2:]))
	}
	// Quick-doctor hook: cheap binary SHA + daemon /healthz check (#2211).
	// Silent on success; prints one-line warning to stderr on drift.
	// Skipped when the user is explicitly running `grafel doctor` (which
	// runs its own full check) and when GRAFEL_SKIP_QUICK_DOCTOR=1.
	if len(os.Args) < 2 || os.Args[1] != "doctor" {
		runQuickDoctorHook()
	}
	cli.Execute(cli.Hooks{
		RunDaemon:       runDaemon,
		RunServe:        runServe,
		RunEngine:       runEngine,
		RunLinks:        runLinksHook,
		RunDashboard:    runDashboard,
		RunQuality:      runQuality,
		RunExtract:      runExtractSubprocess,
		RunBenchCapture: runBenchCaptureDispatch,
	})
}

// runLinksHook is wired into cli.Hooks so the daemon (Phase B) can re-
// run cross-repo link passes whenever a registered repo's graph.json
// changes. It is also used by the daemon's Rebuild RPC handler.
// This is the non-context version for the CLI hook interface.
func runLinksHook(group string) error {
	return cli.RunLinksForGroup(group)
}

// runLinksHookWithCtx is the context-aware version used by the scheduler's
// daemonSchedulerLinks callback. The ctx is the scheduler's shutdownCtx and
// is available for future use in subprocess handling.
func runLinksHookWithCtx(ctx context.Context, group string) error {
	// Context-aware: RunLinksForGroupCtx checks ctx at each heavy pass boundary
	// so a daemon Stop() OR a group delete landing mid-pass (v0.1.8 leak fix)
	// stops the link/phantom recompute promptly instead of running it to
	// completion.
	return cli.RunLinksForGroupCtx(ctx, group)
}

// fail prints an error and exits non-zero. Convenience for callers
// outside main() that have nowhere else to report.
func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	if len(format) > 0 && format[len(format)-1] != '\n' {
		fmt.Fprintln(os.Stderr)
	}
	os.Exit(1)
}
