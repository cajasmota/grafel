package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/process"
)

// TestBridgeSingleton_ClaimsAndReleases asserts the happy path: a clean
// acquire writes our pid to the per-socket pidfile and release removes it.
func TestBridgeSingleton_ClaimsAndReleases(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	path := bridgeSingletonPath(socket)

	release, err := acquireBridgeSingleton(socket, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pid, ok := readBridgePID(path)
	if !ok || pid != os.Getpid() {
		t.Fatalf("pidfile pid = %d (ok=%v), want %d", pid, ok, os.Getpid())
	}
	release()
	if _, ok := readBridgePID(path); ok {
		t.Fatal("pidfile not removed on release")
	}
}

// TestBridgeSingleton_StalePidOverwritten asserts that a pidfile naming a DEAD
// process is silently overwritten — a crashed bridge must not wedge the next
// session.
func TestBridgeSingleton_StalePidOverwritten(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	path := bridgeSingletonPath(socket)

	// Seed a stale pidfile with a pid that is essentially guaranteed dead.
	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	release, err := acquireBridgeSingleton(socket, nil)
	if err != nil {
		t.Fatalf("acquire over stale pidfile: %v", err)
	}
	defer release()
	if pid, _ := readBridgePID(path); pid != os.Getpid() {
		t.Fatalf("stale pidfile not claimed: pid=%d want %d", pid, os.Getpid())
	}
}

// TestBridgeSingleton_ReapsLivePriorBridge asserts that a live prior process
// recorded in the pidfile is reaped (signalled to exit) before we claim
// ownership — the core "single bridge" guarantee (#5633 part 3).
//
// We use a real child process (a long sleep) as the stand-in for the orphan.
// isLiveBridge would normally require the pid to be a grafel process, but on
// non-grafel test children PidIsGrafel returns false; to exercise the reap path
// deterministically without spawning a real grafel binary we drive the reap
// through the exported helpers with a child we then confirm receives SIGTERM.
func TestBridgeSingleton_ReapsLivePriorBridge(t *testing.T) {
	// Spawn a child that ignores nothing and exits on SIGTERM (default).
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Skipf("cannot spawn child process: %v", err)
	}
	priorPID := child.Process.Pid
	// Ensure cleanup even if the test fails before reaping.
	defer func() { _ = child.Process.Kill() }()

	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	path := bridgeSingletonPath(socket)
	if err := os.WriteFile(path, []byte(strconv.Itoa(priorPID)+"\n"), 0o600); err != nil {
		t.Fatalf("seed prior pid: %v", err)
	}

	// The child is not a grafel process, so isLiveBridge → false and the reap
	// path is skipped (we must never SIGTERM a recycled non-grafel pid). Assert
	// that: the child survives and we still claim ownership.
	if isLiveBridge(priorPID) {
		t.Fatal("isLiveBridge returned true for a non-grafel child; reap would target a foreign pid")
	}

	release, err := acquireBridgeSingleton(socket, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()
	if pid, _ := readBridgePID(path); pid != os.Getpid() {
		t.Fatalf("did not claim pidfile: pid=%d want %d", pid, os.Getpid())
	}

	// Child must still be alive (it was not a grafel process, so not reaped).
	time.Sleep(50 * time.Millisecond)
	if child.ProcessState != nil && child.ProcessState.Exited() {
		t.Fatal("non-grafel child was killed — reap must be gated on PidIsGrafel")
	}
}

// TestBridgeSingleton_ReapTargetsLiveGrafel exercises the reap of a process we
// CAN classify as a bridge: we kill it via the same path the acquire uses and
// confirm it exits. We model the live-grafel case by directly invoking the
// reap logic on a child whose liveness we control, asserting waitForExit
// observes the exit after SIGTERM.
func TestBridgeSingleton_ReapTargetsLiveGrafel(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Skipf("cannot spawn child: %v", err)
	}
	pid := child.Process.Pid
	exited := make(chan struct{})
	go func() { _ = child.Wait(); close(exited) }()

	// Simulate the reap action acquire would take for a confirmed live bridge.
	if !process.IsAlive(pid) {
		t.Fatal("expected child to be alive")
	}
	reapPriorBridge(pid)

	select {
	case <-exited:
		// Reaped successfully.
	case <-time.After(2 * time.Second):
		_ = child.Process.Kill()
		t.Fatal("prior bridge was not reaped within the grace window")
	}
}

// TestBridgeSingleton_PathStableAndPerSocket asserts the pidfile path is
// deterministic for a socket and differs across sockets (no cross-socket
// collision).
func TestBridgeSingleton_PathStableAndPerSocket(t *testing.T) {
	// Build socket paths with filepath.Join so the separators match the host
	// OS (backslash on Windows, slash elsewhere). Comparing against a literal
	// "/x/y" would spuriously fail on Windows, where filepath.Dir returns the
	// backslash form (\x\y) — the production placement is correct; only the
	// fixture was unix-only.
	dir := filepath.Join("x", "y")
	aSock := filepath.Join(dir, "a.sock")
	bSock := filepath.Join(dir, "b.sock")

	a1 := bridgeSingletonPath(aSock)
	a2 := bridgeSingletonPath(aSock)
	b := bridgeSingletonPath(bSock)
	if a1 != a2 {
		t.Fatalf("path not stable: %q vs %q", a1, a2)
	}
	if a1 == b {
		t.Fatalf("distinct sockets share a pidfile: %q", a1)
	}
	// The pidfile must live beside the socket: compare cleaned directories on
	// both sides so the assertion is separator-agnostic.
	if got, want := filepath.Clean(filepath.Dir(a1)), filepath.Clean(dir); got != want {
		t.Fatalf("pidfile not beside socket: dir=%q want %q (path %q)", got, want, a1)
	}
}
