// Package homeguard is the fail-closed guard against a test that forgot to
// isolate $HOME writing into the developer's REAL home directory.
//
// # The incident
//
// A dashboard wizard test isolated GRAFEL_HOME but not HOME, resolved the
// developer's real ~/.cursor/mcp.json, and registered the ephemeral
// `dashboard.test` binary path into it — every `go test` run silently rewrote
// the live editor MCP config. internal/install/mcpreg grew a guard in its own
// write path in response (#6240), and internal/registry grew a near-identical
// one for #5443, where a test clobbered the live fleet config and took a group
// to zero entities.
//
// # Why it lives in the write path and not in a TestMain
//
// The opt-in TestMain guard (testsupport.GuardRealHomeMain) only fires when a
// package wires it up AND GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1 is exported. It is
// trivially skipped by a package that never wires it in — which is how both
// incidents happened. A check inside the writers themselves cannot be skipped.
//
// It is inert outside tests (testing.Testing() is false in the shipping binary)
// and inert inside tests that DID isolate, because the write target then lands
// under a TempDir rather than the real home.
//
// # Why this package exists (#6246)
//
// #6246 converted internal/dashboard's writeMCPConfig to FOLLOW symlinks. That
// is the correct fix for the user, and it opens a door for tests: pre-#6246 a
// link planted inside a sandbox was flattened in place, so a badly-isolated test
// stopped at the sandbox boundary; now the write goes THROUGH it. internal/
// dashboard had no analogue of mcpreg's guard, and its config paths are
// $HOME-derived. That is one careless test away from the original incident.
//
// A third hand-rolled copy was the wrong answer, so the logic is here and the
// packages that need it keep a four-line file naming their own destinations. The
// alternative — putting it in internal/testsupport — is not available: that is a
// test-helper package, and this has to be callable from production write paths.
//
// # What is deliberately NOT unified here
//
// internal/registry's copy additionally EXEMPTS os.TempDir(), because on Windows
// t.TempDir() lives below %USERPROFILE%\AppData\Local\Temp and a raw "under the
// real home" check rejects correctly-isolated tests there. mcpreg's copy has no
// such exemption, and this package reproduces mcpreg's semantics EXACTLY —
// adding the exemption would be a behaviour change to mcpreg on Windows, and
// mcpreg's untouched #6240 suite is the regression gate for the change that
// created this package. registry is therefore left on its own copy for now.
// Unifying the third caller means deciding the temp-root question on purpose,
// with mcpreg's Windows behaviour as the thing to argue about; it is a separate
// change from moving code.
package homeguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realUserHome captures the home the same way internal/testsupport and
// internal/registry capture it. Kept independent of testsupport on purpose:
// testsupport is a test-helper package and must not be imported from production
// write paths, which is exactly where this guard has to sit.
func realUserHome() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Clean(h)
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Clean(h)
	}
	return ""
}

// RealUserHome is the developer's home directory, captured at package-init time
// — BEFORE any test has had a chance to call t.Setenv("HOME", ...). It is the
// home a test must never write into.
//
// Empty when it could not be determined, in which case the guard is inert (there
// is nothing to compare against, and guessing would produce false panics).
var RealUserHome = realUserHome()

// Escapes reports whether resolved lands inside home. Split out of Guard so both
// answers are testable without needing a process whose real home is arranged for
// the occasion.
//
// The comparison is on the path STRING after Clean/Abs; it does not itself
// resolve symlinks. Callers must pass an ALREADY-RESOLVED path — see Guard.
func Escapes(resolved, home string) bool {
	if home == "" || resolved == "" {
		return false
	}
	abs := resolved
	if a, err := filepath.Abs(resolved); err == nil {
		abs = a
	}
	abs = filepath.Clean(abs)
	home = filepath.Clean(home)
	if abs == home {
		return true
	}
	return strings.HasPrefix(abs+string(filepath.Separator), home+string(filepath.Separator))
}

// Guard panics if, while running under `go test`, the path a caller is about to
// WRITE lands inside the REAL user home.
//
// component names the calling package ("mcpreg", "dashboard") and what describes
// the file ("MCP host config"); remediation is appended verbatim so each caller
// can name the files it would have clobbered and the isolation helper to call.
// The message always contains "TEST SANDBOX ESCAPE".
//
// Pass a RESOLVED path. Once a package follows symlinks, a link inside a
// properly-isolated t.TempDir() pointing at the real $HOME makes the NAMED path
// look safe while the operation is anything but — the guard decides by looking
// at a string, so it must be handed the string the write will actually use.
//
// Guard once per OPERATION that touches a resolved path, not once per exported
// entry point. #6240 found that collapsing several operations onto one shared
// check left the suite green when either of two overlapping checks was deleted:
// a guard nothing can independently kill is a guard nobody can trust.
func Guard(component, what, resolved, remediation string) {
	if !testing.Testing() {
		return
	}
	if !Escapes(resolved, RealUserHome) {
		return
	}
	abs := resolved
	if a, err := filepath.Abs(resolved); err == nil {
		abs = filepath.Clean(a)
	}
	panic(fmt.Sprintf(
		"%s: TEST SANDBOX ESCAPE — about to WRITE %s to %q, which is inside the "+
			"REAL user home %q. %s",
		component, what, abs, RealUserHome, remediation,
	))
}
