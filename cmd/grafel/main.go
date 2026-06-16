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
		os.Exit(runIndexInternal(os.Args[2:]))
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
	// NOTE: ctx is not yet used by RunLinksForGroup, but is threaded through
	// for future context-aware operations.
	_ = ctx
	return cli.RunLinksForGroup(group)
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
