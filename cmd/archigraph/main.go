package main

import (
	"fmt"
	"os"

	"github.com/cajasmota/archigraph/internal/cli"
)

// main wires the daemon entrypoint (which owns indexing + linking +
// MCP) into the cobra dispatch tree owned by internal/cli, then
// delegates. Index, MCP, and rebuild used to be wired here as direct
// hooks; per ADR-0017 they are now thin RPC clients implemented inside
// internal/cli, and the only hook this package contributes is the
// long-running daemon mode (plus the per-group linker, which both the
// daemon and `archigraph rebuild` need).
func main() {
	cli.Execute(cli.Hooks{
		RunDaemon:    runDaemon,
		RunLinks:     runLinksHook,
		RunDashboard: runDashboard,
		RunQuality:   runQuality,
		RunExtract:   runExtractSubprocess,
	})
}

// runLinksHook is wired into cli.Hooks so the daemon (Phase B) can re-
// run cross-repo link passes whenever a registered repo's graph.json
// changes. It is also used by the daemon's Rebuild RPC handler.
func runLinksHook(group string) error {
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
