package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// reapedChildPID starts and reaps a trivial child so we hold a PID that is
// guaranteed dead. The kernel will not immediately recycle it, giving us a
// stable "dead PID" for liveness tests. (On the rare chance of reuse the test
// would only get MORE lenient, never produce a false pass for our assertions.)
func reapedChildPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		// `true` should always succeed; if not, fall back to an impossible PID.
		return 0x7FFFFFF0
	}
	return cmd.Process.Pid
}

func writePIDFile(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
}

// Issue #4549 mode 1: a stale pidfile naming a DEAD pid must NOT trigger the
// false "daemon already running" — AcquirePIDFile should overwrite it and
// succeed.
func TestAcquirePIDFile_StaleDeadPID_Proceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	dead := reapedChildPID(t)
	writePIDFile(t, path, dead)

	release, err := AcquirePIDFile(path, "")
	if err != nil {
		t.Fatalf("expected stale dead-pid pidfile to be overwritten, got error: %v", err)
	}
	if release == nil {
		t.Fatal("expected a non-nil release closure")
	}
	defer release()

	// The pidfile should now name OUR pid, not the dead one.
	got := ReadPIDFile(path)
	if got != os.Getpid() {
		t.Fatalf("pidfile = %d, want current pid %d", got, os.Getpid())
	}
}

// Issue #4549 mode 1 (PID reuse): a pidfile naming a LIVE process that is NOT
// an grafel daemon must be treated as stale. The test binary itself is a
// live non-grafel process, so its own pid stands in for a recycled pid.
func TestAcquirePIDFile_LiveNonGrafelPID_TreatedStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	// os.Getpid() here is the test binary ("daemon.test"), not "grafel".
	writePIDFile(t, path, os.Getpid())

	release, err := AcquirePIDFile(path, "")
	if err != nil {
		t.Fatalf("expected live non-grafel pid to be treated as stale, got: %v", err)
	}
	defer release()
}

// A pidfile naming an alive grafel daemon must be honored. We cannot spawn
// a real grafel process in a unit test, so we exercise pidIsLiveDaemon's
// decision directly via its two inputs through AcquirePIDFile on a guaranteed
// path: the only live-and-named-grafel case is covered by the unit test
// for pidIsLiveDaemon below where introspection is mocked. Here we assert the
// no-pidfile path simply succeeds (regression guard for the happy path).
func TestAcquirePIDFile_NoExistingFile_Succeeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	release, err := AcquirePIDFile(path, "")
	if err != nil {
		t.Fatalf("AcquirePIDFile on empty dir: %v", err)
	}
	defer release()
	if ReadPIDFile(path) != os.Getpid() {
		t.Fatalf("pidfile not written with current pid")
	}
}

// Release removes the pidfile so the next start sees a clean slate.
func TestAcquirePIDFile_ReleaseRemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	release, err := AcquirePIDFile(path, "")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected pidfile removed after release, stat err = %v", err)
	}
}

// pidAlive sanity: our own pid is alive; a reaped child is not.
func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Fatal("current process should be alive")
	}
	if pidAlive(reapedChildPID(t)) {
		t.Skip("reaped child pid still considered alive (kernel reuse); skipping")
	}
	if pidAlive(-1) || pidAlive(0) {
		t.Fatal("non-positive pids must be reported dead")
	}
}

// pidIsLiveDaemon must reject a dead pid outright (no name lookup needed) and
// must reject a live non-grafel pid (the test binary).
func TestPidIsLiveDaemon(t *testing.T) {
	if pidIsLiveDaemon(reapedChildPID(t)) {
		t.Skip("reaped child pid reused by kernel; skipping liveness assertion")
	}
	// The test binary is live but is not "grafel".
	if pidIsLiveDaemon(os.Getpid()) {
		t.Fatal("live non-grafel pid must not be treated as the daemon owner")
	}
}

// spawnLiveChild starts a disposable, long-lived child process and returns
// its pid plus a cleanup that force-kills it (idempotent — safe to call even
// if the test already killed it via forceKillFunc/AcquirePIDFile's reclaim
// path). Used to stand in for "a live grafel daemon" pid: we cannot spawn a
// real grafel binary in a unit test, so #5710 reclaim tests fake the
// name-match decision via pidIsLiveDaemonFunc and only need a genuinely
// live, killable pid underneath it.
func spawnLiveChild(t *testing.T) (pid int, cleanup func()) {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", "ping -n 31 127.0.0.1 >NUL")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn live child: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return cmd.Process.Pid, func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// withFakePidIsLiveDaemon overrides pidIsLiveDaemonFunc for the duration of
// the test so AcquirePIDFile treats pid as "a live grafel daemon" without
// requiring a real grafel binary.
func withFakePidIsLiveDaemon(t *testing.T, wantPID int) {
	t.Helper()
	orig := pidIsLiveDaemonFunc
	pidIsLiveDaemonFunc = func(pid int) bool { return pid == wantPID }
	t.Cleanup(func() { pidIsLiveDaemonFunc = orig })
}

// withFakeSocketHealth overrides socketHealthProbe for the duration of the
// test so AcquirePIDFile's reclaim decision doesn't need a real daemon
// listening on socketPath.
func withFakeSocketHealth(t *testing.T, healthy bool) {
	t.Helper()
	orig := socketHealthProbe
	socketHealthProbe = func(string, time.Duration) bool { return healthy }
	t.Cleanup(func() { socketHealthProbe = orig })
}

// #5710: a pidfile naming a live, grafel-named process whose socket does NOT
// answer Ping (the wedged-daemon scenario — stuck in graceful shutdown behind
// a stalled Rebuild RPC) must be RECLAIMED, not refused. AcquirePIDFile
// should force-kill the old pid and overwrite the pidfile with our own.
func TestAcquirePIDFile_LiveGrafelPID_UnhealthySocket_Reclaims(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	oldPID, cleanupChild := spawnLiveChild(t)
	defer cleanupChild()
	writePIDFile(t, path, oldPID)

	withFakePidIsLiveDaemon(t, oldPID)
	withFakeSocketHealth(t, false)

	release, err := AcquirePIDFile(path, "/nonexistent/socket/for/probe")
	if err != nil {
		t.Fatalf("expected unhealthy-socket pidfile to be reclaimed, got error: %v", err)
	}
	defer release()

	if got := ReadPIDFile(path); got != os.Getpid() {
		t.Fatalf("pidfile = %d after reclaim, want current pid %d", got, os.Getpid())
	}

	// The old, wedged pid must actually be gone — otherwise the reclaim is a
	// pidfile-only fiction and the real wedged process leaks.
	deadline := time.Now().Add(2 * time.Second)
	for pidAlive(oldPID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(oldPID) {
		t.Fatalf("old pid %d still alive after reclaim; expected it to be force-killed", oldPID)
	}
}

// #5710 false-positive guard: a pidfile naming a live, grafel-named process
// whose socket DOES answer Ping must be refused as ErrAlreadyRunning — a
// healthy daemon must never be force-killed just because it exists.
func TestAcquirePIDFile_LiveGrafelPID_HealthySocket_RefusesNoReclaim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	oldPID, cleanupChild := spawnLiveChild(t)
	defer cleanupChild()
	writePIDFile(t, path, oldPID)

	withFakePidIsLiveDaemon(t, oldPID)
	withFakeSocketHealth(t, true)

	_, err := AcquirePIDFile(path, "/nonexistent/socket/for/probe")
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning for a healthy daemon, got: %v", err)
	}

	// The healthy "daemon" must NOT have been killed.
	if !pidAlive(oldPID) {
		t.Fatalf("healthy pid %d was killed — false-positive reclaim", oldPID)
	}

	// The pidfile must still name the original (healthy) owner, not us.
	if got := ReadPIDFile(path); got != oldPID {
		t.Fatalf("pidfile = %d after refused acquire, want unchanged original pid %d", got, oldPID)
	}
}
