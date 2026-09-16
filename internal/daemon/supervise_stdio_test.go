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
// into the child — and duplicating an unusable one fails on Windows, so
// cmd.Start() fails deterministically. supervise.go's run loop treats a failed
// spawn as a crash (back off, retry), and a deterministic failure never
// recovers: it walks the backoff to the ceiling and gives up.
//
// The graded directions:
//
//   - ..._HealthyParentKeepsTheLaunchdSplit — the no-regression direction, in
//     the shape launchd actually installs (StandardOutPath=daemon.log,
//     StandardErrorPath=daemon.err). Child stdout must still reach daemon.log
//     and child stderr must still reach daemon.err, and NEITHER file may be
//     truncated: the daemon's own logger has already written there. Green
//     before AND after the fix.
//   - ..._InvalidParentHandlesStillSpawns — the direction nothing graded: a
//     parent whose standard handles are CLOSED must still spawn the child, and
//     the child's output must still reach both owned files. Red before the fix.
//   - ..._UnopenableSinksFallBackAwayFromInherited — the fallback: when the
//     owned sinks cannot be opened, the cmd fields must be left nil (os/exec's
//     os.DevNull), NOT set to a nil *os.File and NOT to the inherited handle.
//   - ..._NonAbsoluteRootYieldsNoSink — an empty/relative root must not make
//     the daemon adopt a logs/ directory in its cwd.
//   - ..._SinksAreSharedAcrossSpawns — one sink per file, reused across
//     relaunches, so a crash loop does not leak a descriptor per spawn.

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
// ARGUMENTS so the test binary runs the helper process above, while KEEPING
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
	// The production constructor caches an open sink per path. Registered
	// AFTER t.TempDir so it runs BEFORE TempDir's removal (cleanups are LIFO):
	// Windows cannot remove a file that is still open.
	t.Cleanup(closeEngineChildSinksForTest)
	if err := os.MkdirAll(logDirForRoot(root), 0o700); err != nil {
		t.Fatalf("create logs dir: %v", err)
	}
	return root
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
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

// assertChildStreamsLanded checks the artefact: the helper's stdout marker in
// daemon.log, its stderr marker in daemon.err, and each marker ONLY in its own
// file. Splitting them is the property launchd's plist defines
// (StandardOutPath/StandardErrorPath) and the property `grafel status` and
// `grafel doctor` depend on when they tell a user to read daemon.err.
func assertChildStreamsLanded(t *testing.T, root string) {
	t.Helper()
	outLog := readFileString(t, logPathForRoot(root))
	errLog := readFileString(t, errPathForRoot(root))

	if !strings.Contains(outLog, engineStdioStdoutMarker) {
		t.Errorf("engine child stdout did not reach daemon.log; daemon.log=%q", outLog)
	}
	if !strings.Contains(errLog, engineStdioStderrMarker) {
		t.Errorf("engine child stderr did not reach daemon.err; daemon.err=%q", errLog)
	}
	if strings.Contains(outLog, engineStdioStderrMarker) {
		t.Errorf("engine child stderr was folded into daemon.log, losing the daemon.err split; daemon.log=%q", outLog)
	}
	if strings.Contains(errLog, engineStdioStdoutMarker) {
		t.Errorf("engine child stdout was folded into daemon.err; daemon.err=%q", errLog)
	}
}

// seedLogs writes a sentinel into daemon.log and daemon.err, standing in for
// what the daemon's own logger (and its service definition's redirects) have
// already written there before the first engine spawn. Returns the sentinels.
func seedLogs(t *testing.T, root string) (outSentinel, errSentinel string) {
	t.Helper()
	outSentinel = "sentinel-already-in-daemon-log\n"
	errSentinel = "sentinel-already-in-daemon-err\n"
	if err := os.WriteFile(logPathForRoot(root), []byte(outSentinel), 0o600); err != nil {
		t.Fatalf("seed daemon.log: %v", err)
	}
	if err := os.WriteFile(errPathForRoot(root), []byte(errSentinel), 0o600); err != nil {
		t.Fatalf("seed daemon.err: %v", err)
	}
	return outSentinel, errSentinel
}

// assertSentinelsSurvived is the O_APPEND grading. The engine child's sinks
// must APPEND: daemon.log's byte offsets are a documented contract (#2300, the
// bench harness reads by offset) and daemon.err is the only trace of prior
// incidents that `grafel status` and `grafel doctor` point users at. Opening
// either with O_TRUNC silently wipes what the daemon already logged.
func assertSentinelsSurvived(t *testing.T, root, outSentinel, errSentinel string) {
	t.Helper()
	if got := readFileString(t, logPathForRoot(root)); !strings.HasPrefix(got, outSentinel) {
		t.Errorf("spawning the engine child truncated daemon.log; want it to still start with %q, got %q", outSentinel, got)
	}
	if got := readFileString(t, errPathForRoot(root)); !strings.HasPrefix(got, errSentinel) {
		t.Errorf("spawning the engine child truncated daemon.err; want it to still start with %q, got %q", errSentinel, got)
	}
}

// TestEngineChildStdio_HealthyParentKeepsTheLaunchdSplit is the no-regression
// direction, in the shape the installed service actually uses: launchd's plist
// (internal/daemon/service/launchd_darwin.go) points the daemon's stdout at
// daemon.log and its stderr at daemon.err. The engine child's output must
// still reach the SAME two files, still split, and must not truncate either.
func TestEngineChildStdio_HealthyParentKeepsTheLaunchdSplit(t *testing.T) {
	exe := testBinary(t)
	root := daemonRootWithLogDir(t)
	outSentinel, errSentinel := seedLogs(t, root)

	// Exactly what launchd hands the daemon: two separate append handles.
	parentOut, err := os.OpenFile(logPathForRoot(root), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open daemon.log: %v", err)
	}
	defer parentOut.Close()
	parentErr, err := os.OpenFile(errPathForRoot(root), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open daemon.err: %v", err)
	}
	defer parentErr.Close()

	var cmd *exec.Cmd
	withStdHandles(t, parentOut, parentErr, func() {
		cmd = defaultEngineChildCommand(exe, root)
	})
	if err := runEngineChildHelper(t, asEngineStdioHelper(cmd)); err != nil {
		t.Fatalf("spawn with a healthy parent failed: %v", err)
	}

	assertChildStreamsLanded(t, root)
	assertSentinelsSurvived(t, root, outSentinel, errSentinel)
}

// TestEngineChildStdio_InvalidParentHandlesStillSpawns is the ungraded
// direction from #7083: the daemon was handed no usable standard handles (a
// Windows scheduled task, a service wrapper, or `Start-Process -WindowStyle
// Hidden` with no -Redirect* flag). The engine child must still START, and its
// output must still reach the sinks the daemon owns — without truncating them.
func TestEngineChildStdio_InvalidParentHandlesStillSpawns(t *testing.T) {
	exe := testBinary(t)
	root := daemonRootWithLogDir(t)
	outSentinel, errSentinel := seedLogs(t, root)

	dead := closedFile(t)
	var cmd *exec.Cmd
	withStdHandles(t, dead, dead, func() {
		cmd = defaultEngineChildCommand(exe, root)
	})
	if err := runEngineChildHelper(t, asEngineStdioHelper(cmd)); err != nil {
		t.Fatalf("engine child failed to spawn from a parent with closed standard handles: %v", err)
	}

	assertChildStreamsLanded(t, root)
	assertSentinelsSurvived(t, root, outSentinel, errSentinel)
}

// TestEngineChildStdio_UnopenableSinksFallBackAwayFromInherited pins the
// fallback: when the daemon's own sinks cannot be opened, the child still
// starts, the cmd fields are LEFT NIL (os/exec's documented os.DevNull), and
// nothing goes to the inherited handle. The parent here holds perfectly VALID
// handles — falling back to them would reintroduce #7083 on the launcher shape
// that has no valid ones. Leaving a nil *os.File in the field instead of nil
// would be #7083's defect itself on Windows: os/exec takes its *os.File branch,
// and a nil *os.File's Fd() is ^uintptr(0).
func TestEngineChildStdio_UnopenableSinksFallBackAwayFromInherited(t *testing.T) {
	exe := testBinary(t)
	// A root whose logs/ directory does not exist, so opening both sinks fails.
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
	// Positive control AND the nil-*os.File guard: os/exec only connects the
	// child to os.DevNull for a field that is nil interface-wide.
	if cmd.Stdout != nil {
		t.Errorf("Stdout = %#v, want an untouched nil field so os/exec uses os.DevNull", cmd.Stdout)
	}
	if cmd.Stderr != nil {
		t.Errorf("Stderr = %#v, want an untouched nil field so os/exec uses os.DevNull", cmd.Stderr)
	}
	if err := runEngineChildHelper(t, asEngineStdioHelper(cmd)); err != nil {
		t.Fatalf("engine child failed to spawn without usable sinks: %v", err)
	}

	if got := readFileString(t, inheritedPath); strings.Contains(got, engineStdioStdoutMarker) || strings.Contains(got, engineStdioStderrMarker) {
		t.Errorf("engine child wrote to the INHERITED handle; sink=%q", got)
	}
}

// TestEngineChildStdio_NonAbsoluteRootYieldsNoSink pins the guard on a root
// that is empty or relative: the derived path would then be relative to the
// daemon's CWD, so a stray logs/ directory there would receive — and the
// process would then cache — the engine child's output.
func TestEngineChildStdio_NonAbsoluteRootYieldsNoSink(t *testing.T) {
	exe := testBinary(t)
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "logs"), 0o700); err != nil {
		t.Fatalf("create cwd logs dir: %v", err)
	}
	t.Chdir(cwd)
	t.Cleanup(closeEngineChildSinksForTest)

	for _, root := range []string{"", "." + string(os.PathSeparator), "relative-root"} {
		t.Run("root="+root, func(t *testing.T) {
			cmd := defaultEngineChildCommand(exe, root)
			if cmd.Stdout != nil || cmd.Stderr != nil {
				t.Errorf("root %q produced sinks (Stdout=%#v Stderr=%#v); a relative root resolves against the daemon's cwd",
					root, cmd.Stdout, cmd.Stderr)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(cwd, "logs", "daemon.log")); err == nil {
		t.Error("a relative root created logs/daemon.log in the daemon's cwd")
	}
}

// TestEngineChildStdio_SinksAreSharedAcrossSpawns pins the resource claim in
// engineChildSink's doc: the daemon opens ONE sink per file and reuses it. The
// constructor has no completion hook (the seam returns only an *exec.Cmd), so a
// fresh open per spawn would be a descriptor the parent never closes — one
// leaked per relaunch, and a crash loop relaunches without bound.
func TestEngineChildStdio_SinksAreSharedAcrossSpawns(t *testing.T) {
	exe := testBinary(t)
	root := daemonRootWithLogDir(t)

	first := defaultEngineChildCommand(exe, root)
	second := defaultEngineChildCommand(exe, root)

	firstOut, ok := first.Stdout.(*os.File)
	if !ok {
		t.Fatalf("engine child stdout is not an *os.File: %T", first.Stdout)
	}
	secondOut, ok := second.Stdout.(*os.File)
	if !ok {
		t.Fatalf("engine child stdout is not an *os.File on the second spawn: %T", second.Stdout)
	}
	firstErr, ok := first.Stderr.(*os.File)
	if !ok {
		t.Fatalf("engine child stderr is not an *os.File: %T", first.Stderr)
	}
	secondErr, ok := second.Stderr.(*os.File)
	if !ok {
		t.Fatalf("engine child stderr is not an *os.File on the second spawn: %T", second.Stderr)
	}

	if firstOut != secondOut || firstErr != secondErr {
		t.Errorf("each spawn opened its own sinks (stdout fds %d/%d, stderr fds %d/%d): the parent never closes them, so a crash loop leaks one per relaunch",
			firstOut.Fd(), secondOut.Fd(), firstErr.Fd(), secondErr.Fd())
	}
	if firstOut == firstErr {
		t.Error("stdout and stderr share one sink: the daemon.log/daemon.err split is gone")
	}
}
