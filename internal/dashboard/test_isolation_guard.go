package dashboard

// test_isolation_guard.go — this package's binding of the shared fail-closed
// guard (internal/homeguard) against a test that forgot to isolate $HOME.
//
// This package is where the incident the guard exists for actually HAPPENED:
// v2_wizard_test.go isolated GRAFEL_HOME but not HOME, so it resolved the
// developer's real ~/.cursor/mcp.json and registered the ephemeral
// `dashboard.test` binary path into it — every `go test` run silently rewrote a
// live editor MCP config. internal/install/mcpreg grew a guard in response
// (#6240) and named this package in its doc; this package never got one.
//
// #6246 made that omission matter. writeMCPConfig now FOLLOWS symlinks, which is
// the right fix for the user and a new door for tests: before, a link planted
// inside a sandbox was flattened where it stood, so a badly-isolated test could
// not reach past the sandbox boundary; now the write goes THROUGH it. Combined
// with $HOME-derived configPathFor, that is the original incident one careless
// test away.
//
// Guard per OPERATION, not per entry point. There are two writes here and each
// has its own call — the `<path>.bak` sidecar (which runs FIRST, so a guard only
// on the config write would let a verbatim copy of an OAuth token land in the
// real home before panicking) and the resolved config target. #6240 established
// why they are not collapsed: two overlapping checks left the suite green when
// either was deleted. TestWriteMCPConfig_BackupIsGuarded and
// TestWriteMCPConfig_SymlinkTargetIsGuarded kill exactly one each.
//
// Inert in the shipping binary, and inert for a test that correctly isolated.

import "github.com/cajasmota/grafel/internal/homeguard"

// guardDashboardRemediation names what a leaked write would clobber and the fix.
const guardDashboardRemediation = "This test would clobber the developer's live " +
	"~/.claude/mcp.json / Cursor / Windsurf MCP config. Call " +
	"testsupport.IsolateHome(t) at the top of the test before invoking the MCP " +
	"setup handlers or writeMCPConfig."

// guardWriteTarget panics if, while running under `go test`, a path this package
// is about to WRITE lands inside the REAL user home. Pass an ALREADY-RESOLVED
// path: the guard decides by looking at the string, so a link inside an isolated
// t.TempDir() pointing at the real $HOME makes the NAMED path look safe while
// the write is anything but.
func guardWriteTarget(target, what string) {
	homeguard.Guard("dashboard", what, target, guardDashboardRemediation)
}
