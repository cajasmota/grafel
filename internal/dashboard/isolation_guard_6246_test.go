// isolation_guard_6246_test.go pins the two resolved-path guards on the wizard's
// config writer.
//
// The hole these close is the one #6246 slice 1 reported and did not fix. This
// package had NO analogue of internal/install/mcpreg's isolation guard, while
// configPathFor is $HOME-derived and writeMCPConfig now FOLLOWS symlinks (slice
// 1). Pre-#6246 a link planted inside a test's sandbox was flattened where it
// stood, so a badly-isolated test stopped at the sandbox boundary; now the write
// goes THROUGH it into the real $HOME. That is the original incident — a
// dashboard test rewriting the developer's live ~/.cursor/mcp.json on every `go
// test` — re-entering through the door the symlink fix opened.
//
// There are TWO guarded operations, and the tests below drive them SEPARATELY.
// #6240 found that two overlapping checks left the suite green when either was
// deleted, so each of these constructs a case in which only one of the two can
// possibly fire:
//
//   - the BACKUP: a link inside the real home pointing at a file in the sandbox.
//     os.Stat succeeds, so copyFile runs and writes `<path>.bak` into the real
//     home — while the config write itself resolves harmlessly into the sandbox,
//     so the write guard never sees anything wrong.
//   - the WRITE: a link inside the sandbox pointing at the real home. The target
//     does not exist, so no backup is taken at all, and only the resolved write
//     target escapes.
//
// Neither test writes a regular file into the real home even when it FAILS: the
// canary directory holds at most a symlink, and is removed on cleanup.
package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/homeguard"
)

// canaryDir6246 returns a directory name under the developer's REAL home that
// is not any real dotfile, and arranges for it to be removed however the test
// ends. Skips when there is no real home to guard against.
func canaryDir6246(t *testing.T) string {
	t.Helper()
	if homeguard.RealUserHome == "" {
		t.Skip("no real home to guard against")
	}
	dir := filepath.Join(homeguard.RealUserHome, ".grafel6246-dashboard-canary")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestWriteMCPConfig_BackupIsGuarded pins the guard on the `<path>.bak` sidecar.
//
// Only this guard can fire here: the config write resolves into the sandbox and
// is entirely legitimate. Deleting the write guard leaves this test green;
// deleting the backup guard makes it fail with no panic AND a `.bak` sitting in
// the developer's home.
func TestWriteMCPConfig_BackupIsGuarded(t *testing.T) {
	requireSymlinks6246(t)
	canary := canaryDir6246(t)
	if err := os.MkdirAll(canary, 0o755); err != nil {
		t.Fatalf("mkdir canary: %v", err)
	}

	sandbox := t.TempDir()
	real := filepath.Join(sandbox, "mcp.json")
	seedMCPConfig6246(t, real, 0o600)

	escape := filepath.Join(canary, "mcp.json")
	if err := os.Symlink(real, escape); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	backup := escape + ".bak"

	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("writeMCPConfig backed up to %s — inside the REAL user home — "+
				"without tripping the isolation guard", backup)
		} else if msg, _ := r.(string); !strings.Contains(msg, "TEST SANDBOX ESCAPE") {
			t.Errorf("panic was not the isolation guard: %v", r)
		}
		if _, err := os.Lstat(backup); err == nil {
			t.Errorf("a backup file was created at %s despite the guard — the guard must "+
				"run BEFORE copyFile, not after", backup)
		}
	}()
	_ = writeMCPConfig(escape, grafelCfg6246())
}

// TestWriteMCPConfig_SymlinkTargetIsGuarded pins the guard on the resolved WRITE
// target — the one the symlink fix made necessary.
//
// Only this guard can fire here: the target does not exist, so writeMCPConfig
// takes no backup and the backup guard is never reached.
func TestWriteMCPConfig_SymlinkTargetIsGuarded(t *testing.T) {
	requireSymlinks6246(t)
	canary := canaryDir6246(t)
	escapeTarget := filepath.Join(canary, "mcp.json")

	sandbox := t.TempDir()
	link := filepath.Join(sandbox, "mcp.json")
	if err := os.Symlink(escapeTarget, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("writeMCPConfig followed a symlink out of the test sandbox into the "+
				"REAL user home (%s) without tripping the isolation guard", escapeTarget)
		} else if msg, _ := r.(string); !strings.Contains(msg, "TEST SANDBOX ESCAPE") {
			t.Errorf("panic was not the isolation guard: %v", r)
		}
		if _, err := os.Lstat(escapeTarget); err == nil {
			t.Errorf("a config file was created at %s despite the guard", escapeTarget)
		}
	}()
	_ = writeMCPConfig(link, grafelCfg6246())
}

// TestWriteMCPConfig_IsolatedSandboxIsAllowed is the other half: the guard must
// be inert for the many correctly-isolated tests in this package, backup path
// included. A guard that fired on a TempDir would be found instantly; one that
// never fires would not.
func TestWriteMCPConfig_IsolatedSandboxIsAllowed(t *testing.T) {
	dir := t.TempDir()
	if homeguard.Escapes(dir, homeguard.RealUserHome) {
		t.Skipf("t.TempDir() %q is inside the real home on this platform", dir)
	}
	path := filepath.Join(dir, "mcp.json")
	seedMCPConfig6246(t, path, 0o600)

	if err := writeMCPConfig(path, grafelCfg6246()); err != nil {
		t.Fatalf("writeMCPConfig under an isolated sandbox should succeed: %v", err)
	}
	assertGrafelWritten6246(t, path)
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup not written under an isolated sandbox: %v", err)
	}
}
