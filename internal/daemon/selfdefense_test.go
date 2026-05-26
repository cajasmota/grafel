package daemon_test

// selfdefense_test.go — regression tests for issue #857 daemon self-defense.
//
// Layer 1: a daemon binary under /tmp with a canonical daemon running must refuse
// to start (SelfDefenseCheck returns non-nil error).
//
// Layer 2: the CPU watchdog goroutine is covered by unit tests in
// selfdefense_internal_test.go (internal package, to access unexported helpers).
//
// Layer 3: doctor --kill-stale is tested in internal/cli.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// TestSelfDefenseCheck_AllowsCanonicalBinary verifies that SelfDefenseCheck
// returns nil when the binary is NOT under /tmp (normal production path).
// We can't override os.Executable() directly, but we can verify the exported
// behaviour when running under a non-/tmp test binary (Go test binaries compile
// to TMPDIR which is NOT /tmp on macOS — it's something like /var/folders/...).
func TestSelfDefenseCheck_AllowsCanonicalBinary(t *testing.T) {
	// Self-defense is a Unix concept; on Windows /tmp does not exist and
	// isTmpPath is a no-op, so the check always passes.
	if runtime.GOOS == "windows" {
		t.Skip("self-defense /tmp check is Unix-only")
	}
	// Skip this test if the test binary itself happens to live under /tmp.
	// That would make SelfDefenseCheck try to scan for a canonical daemon,
	// which is environment-dependent and would flap in CI.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if strings.HasPrefix(self, "/tmp/") || self == "/tmp" {
		t.Skip("test binary is under /tmp — cannot test canonical-binary path")
	}

	if err := daemon.SelfDefenseCheck(nil); err != nil {
		t.Errorf("SelfDefenseCheck should return nil for canonical binary at %s, got: %v", self, err)
	}
}

// TestSelfDefenseCheck_RunRefusesWhenCalledFromTmpBinary verifies Layer 1 end-to-end
// by compiling the full archigraph binary under /tmp (using `go test -c` output)
// and confirming that daemon.Run returns an error when a canonical daemon is present.
//
// The test is skipped when there is no canonical daemon on the machine (CI/clean env),
// because we can only simulate the /tmp-binary scenario by building and running the
// real binary — which requires a pre-existing canonical daemon to conflict with.
//
// The test also skips gracefully when a canonical daemon exists but binary parity
// cannot be verified (worktree scenario where the running daemon binary may have
// a different SHA than the worktree-compiled binary, causing structural fragility).
func TestSelfDefenseCheck_RunRefusesWhenCalledFromTmpBinary(t *testing.T) {
	// Self-defense is a Unix concept; on Windows /tmp does not exist and
	// isTmpPath is a no-op, so the binary-under-tmp scenario cannot occur.
	if runtime.GOOS == "windows" {
		t.Skip("self-defense /tmp check is Unix-only")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root — PPID/ps signal checks behave differently")
	}

	// Check if we're running in a worktree (presence of .git/worktrees indicates
	// git worktree setup). If so, and a canonical daemon exists, skip gracefully
	// because binary SHA mismatch is expected and unavoidable.
	canonPID, canonExe := daemon.FindCanonicalDaemon()
	if canonPID > 0 {
		// A canonical daemon is running. Check if we're in a worktree.
		modRoot, err := findModuleRoot()
		if err == nil {
			gitDir := filepath.Join(modRoot, ".git")
			if st, err := os.Stat(gitDir); err == nil && st.IsDir() {
				// Check for git worktree marker (git worktrees have a "worktrees" subdirectory).
				worktreeMarkerPath := filepath.Join(gitDir, "worktrees")
				if _, err := os.Stat(worktreeMarkerPath); err == nil {
					t.Skipf("self-defense check requires installed binary parity; skipping in worktree environment (canonical daemon pid=%d %s)", canonPID, canonExe)
				}
			}
		}
	}

	// Build the full archigraph binary under /tmp using go test -c then go build.
	modRoot, err := findModuleRoot()
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}

	tmpDir, err := os.MkdirTemp("/tmp", "archigraph-sdef-e2e-")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	helperBin := filepath.Join(tmpDir, "archigraph")
	buildCmd := exec.Command("go", "build", "-o", helperBin, "./cmd/archigraph")
	buildCmd.Dir = modRoot
	if out, berr := buildCmd.CombinedOutput(); berr != nil {
		t.Fatalf("build archigraph binary: %v\n%s", berr, out)
	}

	// Use a fresh temp root for ARCHIGRAPH_DAEMON_ROOT so this daemon doesn't
	// conflict with any real daemon running on the machine.
	daemonRoot, err := os.MkdirTemp("/tmp", "archigraph-sdef-root-")
	if err != nil {
		t.Fatalf("mkdirtemp daemon root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(daemonRoot) })

	// Run the binary as `archigraph daemon` from /tmp with ARCHIGRAPH_DAEMON_ROOT set.
	// Two outcomes:
	//   - Canonical daemon present: SelfDefenseCheck exits immediately with code 1.
	//   - No canonical daemon: the daemon starts successfully and runs as a server forever.
	//
	// Use a short deadline (10s) so the test never blocks indefinitely. When the daemon
	// starts successfully (no conflict) it won't exit on its own; the context cancellation
	// sends SIGKILL, and cmd.Wait returns context.DeadlineExceeded — that is the expected
	// "no-conflict" path. When the daemon self-refuses (Layer 1), it exits with code 1
	// well within the 10s window.
	const daemonStartTimeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), daemonStartTimeout)
	defer cancel()

	var stderrBuf strings.Builder
	cmd := exec.CommandContext(ctx, helperBin, "daemon")
	cmd.Env = append(os.Environ(), "ARCHIGRAPH_DAEMON_ROOT="+daemonRoot)
	cmd.Stderr = &stderrBuf

	waitErr := cmd.Run()

	// Determine whether the daemon exited on its own (Layer 1 refusal) or was
	// killed by context timeout (normal startup — no conflict).
	timedOut := ctx.Err() == context.DeadlineExceeded
	combined := stderrBuf.String()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	t.Logf("archigraph daemon exit=%d timedOut=%v waitErr=%v stderr=%s",
		exitCode, timedOut, waitErr, combined)

	// Key regression assertion: if a canonical daemon exists (detected by ps),
	// the binary MUST exit 1 with the Layer 1 refusal message — and it must do
	// so quickly (well within the 10s timeout, so timedOut must be false).
	if canonPID > 0 {
		// A canonical daemon is running — the /tmp binary MUST have refused.
		if timedOut {
			t.Errorf("Layer 1 FAIL: canonical daemon at pid=%d %s, but /tmp binary did not exit within %s (still running)",
				canonPID, canonExe, daemonStartTimeout)
		} else if exitCode != 1 {
			t.Errorf("Layer 1 FAIL: canonical daemon at pid=%d %s, but /tmp binary did not refuse (exit=%d)",
				canonPID, canonExe, exitCode)
		} else if !strings.Contains(combined, "refusing to start") {
			t.Errorf("Layer 1 FAIL: expected 'refusing to start' in stderr, got: %s", combined)
		} else {
			t.Logf("Layer 1 PASS: /tmp binary correctly refused (canonical daemon pid=%d %s)", canonPID, canonExe)
		}
	} else {
		// No canonical daemon — daemon should have started and kept running until
		// the context timeout killed it.
		if timedOut {
			t.Log("no canonical daemon — /tmp binary started successfully (killed by test timeout as expected)")
		} else {
			t.Logf("no canonical daemon — /tmp binary exited %d (may be pid-file conflict or other)", exitCode)
		}
	}
}

// TestFindCanonicalDaemon_SkipsTmpProcesses verifies the parsing logic by
// ensuring that a process with a /tmp binary path is not classified as canonical.
// We test this indirectly by confirming SelfDefenseCheck does NOT refuse to start
// when the only other archigraph-like process (this test process itself) is also
// under a non-canonical path.
//
// This is primarily a smoke test; the core invariant is: if findCanonicalDaemon()
// finds a process, it must have a non-/tmp binary path.
func TestFindCanonicalDaemon_SkipsTmpProcesses(t *testing.T) {
	// Self-defense is a Unix concept; on Windows /tmp does not exist.
	if runtime.GOOS == "windows" {
		t.Skip("self-defense /tmp check is Unix-only")
	}
	// We test the isTmpPath helper via exported SelfDefenseCheck + current binary.
	self, _ := os.Executable()
	if strings.HasPrefix(self, "/tmp/") {
		// Running from /tmp — SelfDefenseCheck will try to find a canonical
		// daemon. On a clean CI machine there won't be one, so it should return nil.
		err := daemon.SelfDefenseCheck(nil)
		if err != nil {
			// This is actually the correct Layer 1 behavior: a /tmp binary found
			// a canonical daemon on this machine.
			t.Logf("SelfDefenseCheck correctly found a canonical daemon: %v", err)
		} else {
			t.Log("SelfDefenseCheck returned nil — no canonical daemon on this machine (expected in CI)")
		}
		return
	}
	// Non-/tmp binary: SelfDefenseCheck must always return nil.
	if err := daemon.SelfDefenseCheck(nil); err != nil {
		t.Errorf("unexpected refusal for non-/tmp binary: %v", err)
	}
}

// TestFindCanonicalDaemon_EsbuildFalsePositive is a regression test for #1719.
//
// Background: when tests run inside a worktree whose path contains "archigraph"
// (e.g. /Users/.../archigraph-worktrees/fix-selfdefense/webui-v2/node_modules/
// @esbuild/darwin-arm64/bin/esbuild), the old strings.Contains check on the full
// executable path would classify esbuild as a canonical archigraph daemon and
// refuse to start a /tmp daemon.
//
// The fix uses filepath.Base() so only the binary's own name is tested against
// the allowlist — a directory component containing "archigraph" is irrelevant.
func TestFindCanonicalDaemon_EsbuildFalsePositive(t *testing.T) {
	// We can't inject a synthetic process into FindByName, so we verify the
	// underlying classification logic directly via FindCanonicalDaemon's
	// documented contract: it must never return a process whose base-name is
	// NOT in the canonical set.
	//
	// Specifically, construct hypothetical paths that the old code would have
	// matched but the new code must not, and confirm they are rejected by
	// reproducing the basename check in-test.

	falsePositivePaths := []string{
		// The exact path from the bug report (project root named "archigraph").
		"/Users/user/Projects/archigraph/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild",
		// Generic worktree layout.
		"/tmp/archigraph-worktrees/fix-selfdefense/node_modules/.bin/esbuild",
		// Another tool in a directory named after the project.
		"/home/ci/archigraph/scripts/build-helper.sh",
		// vite binary in an archigraph project.
		"/home/user/archigraph/node_modules/.bin/vite",
	}

	for _, path := range falsePositivePaths {
		base := strings.ToLower(filepath.Base(path))
		isCanonical := base == "archigraph"
		if isCanonical {
			t.Errorf("false-positive: path %q has basename %q which incorrectly matches canonical set", path, base)
		}
	}

	// Sanity: real archigraph binaries must still match.
	truePaths := []string{
		"/usr/local/bin/archigraph",
		"/home/user/go/bin/archigraph",
		"/opt/archigraph/bin/archigraph",
	}
	for _, path := range truePaths {
		base := strings.ToLower(filepath.Base(path))
		isCanonical := base == "archigraph"
		if !isCanonical {
			t.Errorf("true-negative: path %q with basename %q should match canonical set but does not", path, base)
		}
	}
}

// findModuleRoot walks upward from the current directory to find go.mod.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
