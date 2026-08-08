package mcpreg

// test_isolation_guard.go — this package's binding of the shared fail-closed
// guard against a test that forgot to isolate $HOME rewriting the developer's
// live editor MCP config.
//
// The mechanism, the incident it was written for, and why it lives in the WRITE
// path rather than in a TestMain are all documented on internal/homeguard. What
// stays here is the part that is genuinely this package's: WHICH files would be
// clobbered, and what the author of a failing test has to do about it.
//
// Why guard the writers and not homeDir()/SettingsPath(): a read-only path
// resolution is harmless and extremely common (used to detect whether a tool is
// installed); only a WRITE clobbers the developer's live editor config. Guarding
// RegisterPath/UnregisterPath/RestoreSnapshot — BEFORE backupOnce, which itself
// writes a `.grafel.bak` sidecar and an audit copy under
// ~/.grafel/backups/mcpreg/ — catches every mutating entry point in this
// package, and guardWriteTarget then covers the two operations that act on a
// RESOLVED path.
//
// Lifted into internal/homeguard by #6246 with NO behaviour change: the
// comparison, the "TEST SANDBOX ESCAPE" marker and the absence of a temp-root
// exemption are all reproduced exactly, because this package's #6240 suite is
// the regression gate for that change. See the homeguard package doc for the
// temp-root question that was deliberately left open.

import "github.com/cajasmota/grafel/internal/homeguard"

// realUserHomeAtInit is the home captured at package-init time, BEFORE any test
// has had a chance to call t.Setenv("HOME", ...). Several tests in this package
// read it to build a canary path or to skip when there is nothing to guard
// against.
var realUserHomeAtInit = homeguard.RealUserHome

// guardMCPRemediation names the files a leaked write would clobber and the fix.
// It is appended verbatim to the panic message; the tests assert on
// "IsolateHome".
const guardMCPRemediation = "This test would clobber the developer's live " +
	"~/.cursor/mcp.json / ~/.claude.json / ~/.codeium / ~/.kiro MCP config. " +
	"Call testsupport.IsolateHome(t) at the top of the test before registering, " +
	"unregistering, or restoring any MCP host config file."

// guardResolvedConfigPath panics if, while running under `go test`, the path we
// are about to WRITE an MCP host config file to lands inside the REAL user home
// directory. That can only happen when the test failed to redirect HOME (and,
// for XDG-based hosts like Zed, XDG_CONFIG_HOME) into a TempDir — i.e. the
// dashboard-wizard-leaking-into-~/.cursor/mcp.json bug.
//
// It is a no-op in the shipping binary and a no-op for any test that correctly
// isolated.
func guardResolvedConfigPath(resolved, what string) {
	homeguard.Guard("mcpreg", what, resolved, guardMCPRemediation)
}
