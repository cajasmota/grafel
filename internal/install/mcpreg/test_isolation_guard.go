package mcpreg

// test_isolation_guard.go — fail-closed guard against the incident where a
// test that forgot to isolate $HOME (e.g. the dashboard wizard test in
// internal/dashboard/v2_wizard_test.go, which only isolated GRAFEL_HOME)
// resolved the developer's REAL ~/.cursor/mcp.json (and potentially
// ~/.claude.json, ~/.codeium, ~/.kiro) and REGISTERED the ephemeral
// `dashboard.test` binary path into it — every `go test` run silently
// rewrote the live editor MCP config.
//
// The opt-in TestMain guard (testsupport.GuardRealHomeMain) only fires when a
// package wires it up AND GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1 is exported —
// it is trivially skipped by a package (like internal/dashboard) that never
// wires it in. This guard lives in the WRITE path of mcpreg's config writers
// themselves, so it cannot be skipped: any test that is about to PERSIST a
// grafel MCP entry (or delete/restore one) into a REAL host config file under
// the REAL user home panics LOUDLY before a single byte is written.
//
// Why guard the writers and not homeDir()/SettingsPath(): a read-only path
// resolution is harmless and extremely common (used to detect whether a tool
// is installed); only a WRITE clobbers the developer's live editor config.
// Guarding RegisterPath/UnregisterPath/RestorePath — BEFORE backupOnce, which
// itself writes a `.grafel.bak` sidecar and an audit copy under
// ~/.grafel/backups/mcpreg/ — catches every mutating entry point in this
// package.
//
// It is inert outside tests (`testing.Testing()` is false in the shipping
// binary) and inert inside tests that DID isolate (the write target no longer
// lands under the real home, so the panic condition is never met).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realUserHomeAtInit is captured at package-init time, BEFORE any test has had a
// chance to call t.Setenv("HOME", ...). It is the home we must never write to
// from a test. Captured the same way internal/testsupport and internal/registry
// capture it (kept independent to avoid an import cycle: testsupport is a test
// helper package that must not be imported from non-test production code).
var realUserHomeAtInit = func() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Clean(h)
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Clean(h)
	}
	return ""
}()

// guardResolvedConfigPath panics if, while running under `go test`, the path we
// are about to WRITE an MCP host config file to lands inside the REAL user
// home directory. That can only happen when the test failed to redirect
// HOME (and, for XDG-based hosts like Zed, XDG_CONFIG_HOME) into a TempDir —
// i.e. the dashboard-wizard-leaking-into-~/.cursor/mcp.json bug.
//
// It is a no-op in the shipping binary (testing.Testing() == false) and a no-op
// for any test that correctly isolated (the target path is then under a
// TempDir, not the real home).
func guardResolvedConfigPath(resolved, what string) {
	if !testing.Testing() {
		return
	}
	if realUserHomeAtInit == "" || resolved == "" {
		return
	}
	abs := resolved
	if a, err := filepath.Abs(resolved); err == nil {
		abs = a
	}
	abs = filepath.Clean(abs)
	home := realUserHomeAtInit
	if abs == home || strings.HasPrefix(abs+string(filepath.Separator), home+string(filepath.Separator)) {
		panic(fmt.Sprintf(
			"mcpreg: TEST SANDBOX ESCAPE — about to WRITE %s to %q, which is inside the "+
				"REAL user home %q. This test would clobber the developer's live "+
				"~/.cursor/mcp.json / ~/.claude.json / ~/.codeium / ~/.kiro MCP config. "+
				"Call testsupport.IsolateHome(t) at the top of the test before registering, "+
				"unregistering, or restoring any MCP host config file.",
			what, abs, home,
		))
	}
}
