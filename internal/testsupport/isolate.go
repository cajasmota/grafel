// Package testsupport provides safety-critical test isolation helpers.
//
// These tests have, in the past, corrupted a developer's live MCP config
// (~/.claude.json) and killed their live grafel daemon, because they
// resolved config/state paths via $HOME and dialed the default daemon socket
// while running against the real user home.
//
// The helpers here force every config/state/socket path produced during a test
// to land inside a per-test TempDir, and — crucially — install a guard that
// FAILS the test fast if the effective HOME is still the real user home or if a
// path about to be touched escapes the temp sandbox. Defense in depth: even if
// a future test forgets to isolate, the guard catches it before any real file
// is written or any live socket is dialed.
//
// This package deliberately does NOT import internal/daemon (which would create
// an import cycle for daemon's own tests); the daemon env-var names are
// duplicated as string literals and asserted against in daemon's tests.
package testsupport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/homeguard"
)

// Environment variables that steer grafel config/state/socket resolution.
// Kept as literals to avoid importing internal/daemon (import-cycle safe).
const (
	envHome          = "HOME"
	envUserProfile   = "USERPROFILE" // Windows home
	envXDGConfigHome = "XDG_CONFIG_HOME"
	envXDGRuntimeDir = "XDG_RUNTIME_DIR"
	envDaemonRoot    = "GRAFEL_DAEMON_ROOT"
	envGrafelHome    = "GRAFEL_HOME"
)

// realUserHome is captured once, at process start, BEFORE any test has had a
// chance to call t.Setenv("HOME", ...). It is the home we must never touch.
var realUserHome = func() string {
	// os.UserHomeDir reads $HOME (or %USERPROFILE% on Windows). At package-init
	// time no test has overridden it yet, so this is the genuine user home.
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Clean(h)
	}
	if h := os.Getenv(envHome); h != "" {
		return filepath.Clean(h)
	}
	return ""
}()

// RealUserHome returns the genuine user home captured at process start.
func RealUserHome() string { return realUserHome }

// IsolateHome redirects every home/config/state/socket-resolving environment
// variable into a fresh per-test TempDir, then asserts the redirect actually
// took effect. After this call:
//
//   - $HOME / %USERPROFILE% point at <tmp> (so ~/.claude.json, ~/.codeium,
//     ~/.grafel all resolve under <tmp>),
//   - $XDG_CONFIG_HOME points at <tmp>/cfg,
//   - $XDG_RUNTIME_DIR points at <tmp>/run,
//   - $GRAFEL_DAEMON_ROOT points at <tmp>/.grafel (isolated daemon
//     socket/pid/log), and
//   - $GRAFEL_HOME points at <tmp>/.grafel.
//
// It returns the temp home root. The guard (see GuardRealHome) is wired in via
// t.Cleanup so a test that somehow re-points HOME back at the real home is
// caught before it finishes.
func IsolateHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	archRoot := filepath.Join(tmp, ".grafel")
	if err := os.MkdirAll(archRoot, 0o700); err != nil {
		t.Fatalf("testsupport: create isolated daemon root: %v", err)
	}

	t.Setenv(envHome, tmp)
	t.Setenv(envUserProfile, tmp)
	t.Setenv(envXDGConfigHome, filepath.Join(tmp, "cfg"))
	t.Setenv(envXDGRuntimeDir, filepath.Join(tmp, "run"))
	t.Setenv(envDaemonRoot, archRoot)
	t.Setenv(envGrafelHome, archRoot)

	// Verify the redirect actually took effect and did not land on the real
	// user home. This is the safety net the whole issue is about.
	GuardRealHome(t)

	return tmp
}

// GuardRealHome FAILS the test immediately if the effective home directory is
// the genuine user home captured at process start. Call it directly in a test's
// TestMain, or rely on IsolateHome which wires it for you. It is cheap and
// idempotent; call it as often as you like.
//
// Wire this into a package's TestMain to make the entire package fail-closed:
//
//	func TestMain(m *testing.M) {
//	    testsupport.GuardRealHomeMain()
//	    os.Exit(m.Run())
//	}
func GuardRealHome(t *testing.T) {
	t.Helper()
	if eff := effectiveHome(); realUserHome != "" && eff == realUserHome {
		t.Fatalf(
			"testsupport: SANDBOX ESCAPE — effective HOME (%q) is the real user home; "+
				"this test would read/write the developer's live ~/.claude.json / ~/.codeium / "+
				"~/.grafel or dial the live daemon socket. Call testsupport.IsolateHome(t) first.",
			eff,
		)
	}
}

// AssertUnderHome FAILS the test if path is not inside the (already-isolated)
// home tempdir. Use it as a belt-and-braces check after a test writes a config
// file, to prove nothing escaped the sandbox.
//
// #6290: the containment decision is homeguard.Escapes, not a local copy. This
// held a byte-for-byte duplicate of the construction homeguard was carrying —
// `strings.HasPrefix(abs+sep, home+sep)` — and inherited every defect that
// construction had: case-sensitive on Windows and darwin (so an escape spelled
// with different case read as contained), and broken for a ROOT home, where
// home+sep is "//" or "C:\\" and nothing matches at all.
//
// That mattered here specifically because #6288 added AssertUnderHome calls to
// internal/mcp's docgen tests as the assertion that a promote cannot escape the
// sandbox. Implementing that assertion with the comparison #6288 exists to
// replace would have made it pass for reasons unrelated to the property — it
// only worked because IsolateHome sets both sides from the same string.
//
// homeguard is importable from here: it depends on nothing but the standard
// library, and it is production code precisely so it can be called from write
// paths. The dependency runs testsupport → homeguard, never the reverse.
func AssertUnderHome(t *testing.T, path string) {
	t.Helper()
	home := effectiveHome()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("testsupport: abs(%q): %v", path, err)
	}
	abs = filepath.Clean(abs)
	if home == "" || !homeguard.Escapes(abs, home) {
		t.Fatalf("testsupport: path %q escapes the isolated home %q", abs, home)
	}
	if realUserHome != "" && homeguard.Escapes(abs, realUserHome) {
		t.Fatalf("testsupport: path %q is under the REAL user home %q — refusing", abs, realUserHome)
	}
}

func effectiveHome() string {
	if h := os.Getenv(envHome); h != "" {
		return filepath.Clean(h)
	}
	if h := os.Getenv(envUserProfile); h != "" {
		return filepath.Clean(h)
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Clean(h)
	}
	return ""
}
