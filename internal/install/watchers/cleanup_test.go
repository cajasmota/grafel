package watchers

import (
	"os"
	"path/filepath"
	"testing"
)

// seamLaunchctl installs a no-op fake service-manager runner for the duration
// of the test.
//
// Cleanup -> Loader.Unload issues real OS service-manager calls. `gui/<uid>/
// <label>` is a system database that no environment variable sandboxes, so
// redirecting $HOME (as these tests do) does NOT isolate them: without this
// seam the suite writes launchd override entries for the `demo` fixtures onto
// the developer's machine, permanently and with no cleanup. The fail-closed
// guard in test_isolation_guard.go now panics rather than letting that happen;
// this seam is how the test does what it actually means to do.
func seamLaunchctl(t *testing.T) {
	t.Helper()
	t.Cleanup(StubServiceCallsForTest())
}

// TestCleanup_RemovesUnitFile verifies the #5338 group-delete fix: Cleanup
// removes the on-disk watcher unit/plist for a repo so a later recreate does
// not fight stale state.
func TestCleanup_RemovesUnitFile(t *testing.T) {
	seamLaunchctl(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), ".config"))

	u := Unit{Group: "demo", Repo: filepath.Join(t.TempDir(), "core"), BinPath: "/bin/grafel"}
	path, err := Write(u)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unit file should exist after Write: %v", err)
	}

	// Cleanup must remove the unit file. (The OS Unload step is best-effort and
	// idempotent; on a clean test box the unit was never loaded, which Unload
	// treats as success.)
	Cleanup(u.Group, u.Repo, u.BinPath)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unit file should be removed after Cleanup, stat err=%v", err)
	}
}

// TestCleanup_Idempotent verifies Cleanup tolerates a never-installed unit
// (no file on disk, not loaded) without panicking or erroring.
func TestCleanup_Idempotent(t *testing.T) {
	seamLaunchctl(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), ".config"))

	u := Unit{Group: "demo", Repo: filepath.Join(t.TempDir(), "never"), BinPath: "/bin/grafel"}
	// No Write — nothing exists. Cleanup must be a no-op.
	Cleanup(u.Group, u.Repo, u.BinPath)
	Cleanup(u.Group, u.Repo, u.BinPath) // twice, still fine
}
