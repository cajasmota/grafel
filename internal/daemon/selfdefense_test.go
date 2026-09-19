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

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/testsupport"
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
// by compiling the full grafel binary under /tmp (using `go test -c` output)
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

	// Check if we're running in a worktree. If so, and a canonical daemon
	// exists, skip gracefully because the worktree-compiled binary's SHA won't
	// match the installed daemon's ("binary updated since last install"),
	// making this real-binary e2e refusal test structurally fragile.
	//
	// Two worktree shapes must both be detected (the old guard only caught the
	// first, which is why it never fired when the tests themselves run FROM a
	// linked worktree):
	//   - a MAIN checkout that has linked worktrees → .git is a DIR containing
	//     a "worktrees/" subdirectory.
	//   - a LINKED worktree (this case) → .git is a FILE ("gitdir: …" pointer),
	//     not a directory.
	canonPID, canonExe := daemon.FindCanonicalDaemon()
	if canonPID > 0 {
		if modRoot, err := findModuleRoot(); err == nil {
			gitPath := filepath.Join(modRoot, ".git")
			if st, err := os.Stat(gitPath); err == nil {
				inWorktree := false
				if st.IsDir() {
					if _, err := os.Stat(filepath.Join(gitPath, "worktrees")); err == nil {
						inWorktree = true
					}
				} else {
					// .git is a file → this checkout is itself a linked worktree
					// (or submodule); binary SHA parity cannot be guaranteed.
					inWorktree = true
				}
				if inWorktree {
					t.Skipf("self-defense check requires installed binary parity; skipping in worktree environment (canonical daemon pid=%d %s)", canonPID, canonExe)
				}
			}
		}
	}

	// Build the full grafel binary under /tmp using go test -c then go build.
	modRoot, err := findModuleRoot()
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}

	tmpDir, err := os.MkdirTemp("/tmp", "grafel-sdef-e2e-")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	helperBin := filepath.Join(tmpDir, "grafel")
	buildCmd := exec.Command("go", "build", "-o", helperBin, "./cmd/grafel")
	buildCmd.Dir = modRoot
	if out, berr := buildCmd.CombinedOutput(); berr != nil {
		t.Fatalf("build grafel binary: %v\n%s", berr, out)
	}

	// Use a fresh temp root for GRAFEL_DAEMON_ROOT so this daemon doesn't
	// conflict with any real daemon running on the machine.
	daemonRoot, err := os.MkdirTemp("/tmp", "grafel-sdef-root-")
	if err != nil {
		t.Fatalf("mkdirtemp daemon root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(daemonRoot) })

	// Run the binary as `grafel daemon` from /tmp with GRAFEL_DAEMON_ROOT set.
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
	// #6134 — GRAFEL_HOME must be pinned alongside the daemon root, and via the
	// daemon.EnvRoot CONSTANT rather than a raw literal.
	//
	// The root only isolates DefaultLayout (socket/pid/log); daemon.StoreDir()
	// resolves $GRAFEL_HOME (else ~/.grafel) + "/store", which this child would
	// otherwise inherit from the developer's real environment via os.Environ() —
	// and its startup tail runs MigrateToRefStore and PruneStaleGenerations
	// against it, mutating and pruning the live store.
	//
	// The raw string literal is also why the original #6134 sweep missed this
	// site: it greps for daemon.EnvRoot, and a literal does not match. Using the
	// constant keeps this line discoverable by the next audit.
	cmd.Env = append(os.Environ(), daemon.EnvRoot+"="+daemonRoot, "GRAFEL_HOME="+daemonRoot,
		// Guard belt (#stop-fleet): testing.Testing() is false in this
		// go-built child, so the watchers package's service-manager guard
		// cannot see that it is under test. Export the belt so a child that
		// ever reaches `grafel stop`/`install`/`update` refuses to mutate the
		// developer's real launchd/systemd state instead of doing it silently.
		watchers.NoServiceMutationEnv+"=1",
	)
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
	t.Logf("grafel daemon exit=%d timedOut=%v waitErr=%v stderr=%s",
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
// when the only other grafel-like process (this test process itself) is also
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
// Background: when tests run inside a worktree whose path contains "grafel"
// (e.g. /Users/.../grafel-worktrees/fix-selfdefense/webui-v2/node_modules/
// @esbuild/darwin-arm64/bin/esbuild), the old strings.Contains check on the full
// executable path would classify esbuild as a canonical grafel daemon and
// refuse to start a /tmp daemon.
//
// The fix uses filepath.Base() so only the binary's own name is tested against
// the allowlist — a directory component containing "grafel" is irrelevant.
//
// WHAT CHANGED IN #7277. Until then this test computed
// `strings.ToLower(filepath.Base(path)) == "grafel"` itself and asserted on
// that, which is a hand-written copy of the rule it claims to grade: the
// assertion compared the copy against itself and could not observe the shipped
// code at all. Measured on the pre-fix revision, all three of these mutants
// left it PASSING — canonicalBasenames emptied, IsCanonicalBinaryPath forced to
// true, IsCanonicalBinaryPath forced to false. The rows below now go through
// daemon.IsCanonicalBinaryPath, the exported identity gate #7268 lifted out of
// findCanonicalDaemon, and each of those three mutants fails at least one half.
// Do not reintroduce a local derivation of the rule here; call production.
//
// PLATFORM. IsCanonicalBinaryPath opens with filepath.IsAbs, whose answer is
// GOOS-dependent: "/usr/local/bin/grafel" is absolute on unix and NOT absolute
// on windows, which has no volume in it. Every row is therefore routed through
// testsupport.AbsFixture, or on windows the accept rows would invert and the
// reject rows would pass for the unrelated reason that nothing looked absolute.
// A /tmp-PREFIX fixture cannot be routed — a volume moves it off the boundary
// its sibling guards test on, see testsupport.AbsFixture — so such a row is
// SKIPPED on windows instead, with a count floor below because a skipped row
// reports SUCCESS.
func TestFindCanonicalDaemon_EsbuildFalsePositive(t *testing.T) {
	type row struct {
		path string
		// tmpPrefix marks a fixture whose /tmp prefix must survive verbatim, so
		// it cannot be absolutised and does not grade the gate on windows.
		tmpPrefix bool
	}

	// Paths the old strings.Contains check matched and the shipped gate must
	// reject: the "grafel" is a DIRECTORY component, never the binary's name.
	rejected := []row{
		// The exact path from the bug report (project root named "grafel").
		{path: "/Users/user/Projects/grafel/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild"},
		// Generic worktree layout.
		{path: "/tmp/grafel-worktrees/fix-selfdefense/node_modules/.bin/esbuild", tmpPrefix: true},
		// Another tool in a directory named after the project.
		{path: "/home/ci/grafel/scripts/build-helper.sh"},
		// vite binary in an grafel project.
		{path: "/home/user/grafel/node_modules/.bin/vite"},
	}

	// PERMISSIVE DIRECTION. Real grafel binaries must still match, or the gate
	// stops finding the user's canonical daemon and the whole anti-displacement
	// protection silently switches off. Both halves have to be load-bearing:
	// a gate hard-wired to false satisfies every row above on its own.
	accepted := []row{
		{path: "/usr/local/bin/grafel"},
		{path: "/home/user/go/bin/grafel"},
		{path: "/opt/grafel/bin/grafel"},
	}

	// Count floors, stated separately from the tables so emptying or thinning
	// one is loud rather than a silently vacuous pass. Measured: deleting the
	// rows leaves the assertions below with nothing to check and the test green
	// without them.
	const (
		wantRejected  = 4
		wantAccepted  = 3
		tmpPrefixRows = 1 // rows ungradable on windows; see PLATFORM above
	)
	if len(rejected) != wantRejected || len(accepted) != wantAccepted {
		t.Fatalf("table has %d reject / %d accept rows, want %d / %d — change the constants deliberately",
			len(rejected), len(accepted), wantRejected, wantAccepted)
	}
	flagged := 0
	for _, r := range append(append([]row{}, rejected...), accepted...) {
		if r.tmpPrefix {
			flagged++
		}
	}
	if flagged != tmpPrefixRows {
		t.Fatalf("%d rows are marked tmpPrefix, want %d — change tmpPrefixRows deliberately, or drop the flag",
			flagged, tmpPrefixRows)
	}

	wantRun := wantRejected + wantAccepted
	if runtime.GOOS == "windows" {
		wantRun -= tmpPrefixRows
	}
	ran := 0

	check := func(r row, want bool) {
		if r.tmpPrefix && runtime.GOOS == "windows" {
			return // ungradable here; counted by wantRun
		}
		ran++
		exe := r.path
		if !r.tmpPrefix {
			exe = testsupport.AbsFixture(r.path)
		}
		if got := daemon.IsCanonicalBinaryPath(exe); got != want {
			t.Errorf("daemon.IsCanonicalBinaryPath(%q) = %v, want %v", exe, got, want)
		}
	}
	for _, r := range rejected {
		check(r, false)
	}
	for _, r := range accepted {
		check(r, true)
	}

	if ran != wantRun {
		t.Errorf("%d of %d rows ran on %s, want %d — a widened skip grades less while still reporting ok",
			ran, wantRejected+wantAccepted, runtime.GOOS, wantRun)
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
