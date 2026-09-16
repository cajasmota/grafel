package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// supervise_stdio_test.go pins issue #7083: the engine child's standard
// handles must be something the DAEMON owns, never something it inherits from
// whatever launched it.
//
// Before the fix, defaultEngineChildCommand did:
//
//	cmd.Stdout = os.Stdout
//	cmd.Stderr = os.Stderr
//
// so the daemon's ability to spawn its own engine depended on a property of
// its launcher that it neither controls nor checks. os/exec hands those
// *os.Files to StartProcess, which duplicates the underlying descriptor/handle
// into the child — and duplicating an INVALID one fails, so cmd.Start() fails
// deterministically. supervise.go's run loop treats a failed spawn as a crash
// (back off, retry), and a deterministic failure never recovers: it walks the
// backoff to the ceiling and gives up. A Windows process launched by
// `Start-Process -WindowStyle Hidden` with no -Redirect* flag runs under
// UseShellExecute=true and has no valid standard handles at all.
//
// The two graded directions:
//
//   - TestEngineChildStdio_HealthyParentStillLogsToDaemonLog — the healthy
//     `grafel start` shape (internal/cli/watcher_ctl.go's manual fork points
//     the daemon's own stdout/stderr at daemon.log). Child output must still
//     land in daemon.log. Green before AND after the fix: it is the
//     no-regression direction.
//   - TestEngineChildStdio_InvalidParentHandlesStillSpawns — the direction
//     nothing graded: a parent whose standard handles are CLOSED must still
//     spawn the child successfully, and the child's output must still reach
//     the daemon's log. Red before the fix.
//   - TestEngineChildStdio_UnopenableLogFallsBackAwayFromInherited — the
//     fallback: when the owned sink cannot be opened, the spawn must still
//     succeed (os.DevNull) and must NOT fall back to the inherited handle.

const (
	engineStdioHelperEnv    = "GRAFEL_TEST_ENGINE_STDIO_HELPER"
	engineStdioStdoutMarker = "engine-stdio-helper-stdout-marker"
	engineStdioStderrMarker = "engine-stdio-helper-stderr-marker"
)

// TestEngineChildStdioHelperProcess is the child process for the tests in this
// file (the standard os/exec subprocess-testing pattern). It writes a marker to
// each of its standard streams and exits; the parent then asserts where those
// markers landed. It is inert unless the env marker is set.
func TestEngineChildStdioHelperProcess(t *testing.T) {
	if os.Getenv(engineStdioHelperEnv) != "1" {
		t.Skip("helper process; only runs when re-invoked by this file's tests")
	}
	fmt.Fprintln(os.Stdout, engineStdioStdoutMarker)
	fmt.Fprintln(os.Stderr, engineStdioStderrMarker)
}

// testBinary is the path these tests pass to defaultEngineChildCommand as
// selfExe, so the constructed command already points at a runnable program.
func testBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	return exe
}

// asEngineStdioHelper rewrites an already-constructed engine-child command's
// ARGUMENTS so the test binary runs the helper process below, while KEEPING
// the stdout/stderr and the program the production constructor chose. The
// stdio wiring is the whole subject of these tests; the arguments are not, and
// `grafel engine --foreground` is not something a unit test may run.
func asEngineStdioHelper(cmd *exec.Cmd) *exec.Cmd {
	cmd.Args = []string{cmd.Path, "-test.run=^TestEngineChildStdioHelperProcess$"}
	cmd.Env = append(cmd.Env, engineStdioHelperEnv+"=1")
	return cmd
}

// withStdHandles swaps the process-wide os.Stdout/os.Stderr for the duration of
// fn, so fn observes the standard handles a particular launcher would have
// given this process. Restores on return. Not parallel-safe — no test in this
// file calls t.Parallel.
func withStdHandles(t *testing.T, out, errF *os.File, fn func()) {
	t.Helper()
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errF
	defer func() { os.Stdout, os.Stderr = prevOut, prevErr }()
	fn()
}

// daemonRootWithLogDir builds a throwaway daemon root with its logs/ directory
// present, mirroring what EnsureLayout leaves behind before serve starts.
func daemonRootWithLogDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// The production constructor caches an open sink per root. Registered
	// AFTER t.TempDir so it runs BEFORE TempDir's removal (cleanups are LIFO):
	// Windows cannot remove a file that is still open.
	t.Cleanup(closeEngineChildSinksForTest)
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatalf("create logs dir: %v", err)
	}
	return root
}

func readDaemonLog(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "logs", "daemon.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read daemon.log: %v", err)
	}
	return string(b)
}

// closedFile returns an *os.File whose descriptor/handle has been closed — the
// in-process stand-in for a parent that was handed no usable standard handles.
func closedFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "closed"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open scratch file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close scratch file: %v", err)
	}
	return f
}

// runEngineChildHelper starts the (already repointed) command and waits for it.
func runEngineChildHelper(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}

// TestEngineChildStdio_HealthyParentStillLogsToDaemonLog is the no-regression
// direction: a daemon started the supported way (`grafel start` →
// defaultManualForkStart, which points the daemon's own stdout/stderr at
// layout.LogPath) must still see its engine child's output in daemon.log.
func TestEngineChildStdio_HealthyParentStillLogsToDaemonLog(t *testing.T) {
	exe := testBinary(t)
	root := daemonRootWithLogDir(t)
	logPath := filepath.Join(root, "logs", "daemon.log")

	// Exactly what defaultManualForkStart does for the daemon process itself.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open daemon.log: %v", err)
	}
	defer logFile.Close()

	var cmd *exec.Cmd
	withStdHandles(t, logFile, logFile, func() {
		cmd = defaultEngineChildCommand(exe, root)
	})
	if err := runEngineChildHelper(t, asEngineStdioHelper(cmd)); err != nil {
		t.Fatalf("spawn with a healthy parent failed: %v", err)
	}

	got := readDaemonLog(t, root)
	if !strings.Contains(got, engineStdioStdoutMarker) {
		t.Errorf("engine child stdout did not reach daemon.log; log=%q", got)
	}
	if !strings.Contains(got, engineStdioStderrMarker) {
		t.Errorf("engine child stderr did not reach daemon.log; log=%q", got)
	}
}

// TestEngineChildStdio_InvalidParentHandlesStillSpawns is the ungraded
// direction from #7083: the daemon was handed no usable standard handles (a
// Windows scheduled task, a service wrapper, or `Start-Process -WindowStyle
// Hidden` with no -Redirect* flag). The engine child must still START, and its
// output must still reach the sink the daemon owns.
func TestEngineChildStdio_InvalidParentHandlesStillSpawns(t *testing.T) {
	exe := testBinary(t)
	root := daemonRootWithLogDir(t)

	dead := closedFile(t)
	var cmd *exec.Cmd
	withStdHandles(t, dead, dead, func() {
		cmd = defaultEngineChildCommand(exe, root)
	})
	if err := runEngineChildHelper(t, asEngineStdioHelper(cmd)); err != nil {
		t.Fatalf("engine child failed to spawn from a parent with closed standard handles: %v", err)
	}

	got := readDaemonLog(t, root)
	if !strings.Contains(got, engineStdioStdoutMarker) {
		t.Errorf("engine child stdout did not reach daemon.log; log=%q", got)
	}
	if !strings.Contains(got, engineStdioStderrMarker) {
		t.Errorf("engine child stderr did not reach daemon.log; log=%q", got)
	}
}

// TestEngineChildStdio_UnopenableLogFallsBackAwayFromInherited pins the
// fallback: when the daemon's own log sink cannot be opened, the child still
// starts AND its output does not go to the inherited handle. The parent here
// holds a perfectly VALID handle (a file its launcher gave it) — falling back
// to it would reintroduce #7083 on the launcher shape that has no valid one.
func TestEngineChildStdio_UnopenableLogFallsBackAwayFromInherited(t *testing.T) {
	exe := testBinary(t)
	// A root whose logs/ directory does not exist, so opening
	// <root>/logs/daemon.log fails.
	root := filepath.Join(t.TempDir(), "no-such-root")
	t.Cleanup(closeEngineChildSinksForTest)

	inheritedPath := filepath.Join(t.TempDir(), "launcher-gave-us-this.log")
	inherited, err := os.OpenFile(inheritedPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open inherited sink: %v", err)
	}
	defer inherited.Close()

	var cmd *exec.Cmd
	withStdHandles(t, inherited, inherited, func() {
		cmd = defaultEngineChildCommand(exe, root)
	})
	if err := runEngineChildHelper(t, asEngineStdioHelper(cmd)); err != nil {
		t.Fatalf("engine child failed to spawn without a usable log sink: %v", err)
	}

	b, err := os.ReadFile(inheritedPath)
	if err != nil {
		t.Fatalf("read inherited sink: %v", err)
	}
	if got := string(b); strings.Contains(got, engineStdioStdoutMarker) || strings.Contains(got, engineStdioStderrMarker) {
		t.Errorf("engine child wrote to the INHERITED handle; sink=%q", got)
	}
}

// TestEngineChildStdio_SinkIsSharedAcrossSpawns pins the resource claim in
// engineChildLogSink's doc: the daemon opens ONE sink per root and reuses it.
// The constructor has no completion hook (the seam returns only an *exec.Cmd),
// so a fresh open per spawn would be a descriptor the parent never closes —
// one leaked per relaunch, and a crash loop relaunches without bound.
func TestEngineChildStdio_SinkIsSharedAcrossSpawns(t *testing.T) {
	exe := testBinary(t)
	root := daemonRootWithLogDir(t)

	first, ok := defaultEngineChildCommand(exe, root).Stdout.(*os.File)
	if !ok {
		t.Fatalf("engine child stdout is not an *os.File: %T", defaultEngineChildCommand(exe, root).Stdout)
	}
	second, ok := defaultEngineChildCommand(exe, root).Stdout.(*os.File)
	if !ok {
		t.Fatal("engine child stdout is not an *os.File on the second spawn")
	}
	if first != second {
		t.Errorf("each spawn opened its own sink (fds %d and %d): the parent never closes them, so a crash loop leaks one per relaunch",
			first.Fd(), second.Fd())
	}
}
