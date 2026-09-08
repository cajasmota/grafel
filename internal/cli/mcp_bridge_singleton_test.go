package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBridgeSingleton_ClaimsAndReleases asserts the happy path: a clean
// acquire writes our pid to the per-session pidfile and release removes it.
func TestBridgeSingleton_ClaimsAndReleases(t *testing.T) {
	t.Setenv(EnvBridgeSession, "sess-claims")
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")

	release, path, err := acquireBridgeSingleton(socket, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if want := bridgeSingletonPath(socket, bridgeSessionID()); path != want {
		t.Fatalf("acquire returned pidfile %q, want %q", path, want)
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
	t.Setenv(EnvBridgeSession, "sess-stale")
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	path := bridgeSingletonPath(socket, bridgeSessionID())

	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	release, _, err := acquireBridgeSingleton(socket, nil)
	if err != nil {
		t.Fatalf("acquire over stale pidfile: %v", err)
	}
	defer release()
	if pid, _ := readBridgePID(path); pid != os.Getpid() {
		t.Fatalf("stale pidfile not claimed: pid=%d want %d", pid, os.Getpid())
	}
}

// TestBridgeSingleton_NeverSignalsPriorBridge is the direct pin on the #6999
// fix: acquiring over a pidfile that names a LIVE grafel-named process must
// leave that process running. Before #6999 this path sent SIGTERM.
//
// The child is a copy of the test binary named "grafel", so process.PidIsGrafel
// classifies it exactly as it would a real bridge — without which the old reap
// would have been skipped and this test would prove nothing.
func TestBridgeSingleton_NeverSignalsPriorBridge(t *testing.T) {
	t.Setenv(EnvBridgeSession, "sess-prior")
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	path := bridgeSingletonPath(socket, bridgeSessionID())

	child := startFakeGrafelChild(t)
	if !isLiveBridge(child.pid) {
		t.Fatalf("precondition: child pid %d must classify as a live grafel process; "+
			"without it this test cannot observe a reap", child.pid)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(child.pid)+"\n"), 0o600); err != nil {
		t.Fatalf("seed prior pid: %v", err)
	}

	var logged []string
	release, _, err := acquireBridgeSingleton(socket, func(f string, a ...any) {
		logged = append(logged, strings.TrimSpace(fmt.Sprintf(f, a...)))
	})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	// The prior bridge must still be alive: no signal was sent.
	if !child.aliveAfter(200 * time.Millisecond) {
		t.Fatalf("prior bridge (pid %d) was terminated by acquire — #6999 reap is back", child.pid)
	}
	if pid, _ := readBridgePID(path); pid != os.Getpid() {
		t.Fatalf("did not claim record: pid=%d want %d", pid, os.Getpid())
	}
	if !anyContains(logged, "leaving it alone") {
		t.Fatalf("a live prior bridge must be reported, not silently ignored; logged=%v", logged)
	}
}

// TestBridgeSingleton_PathIsPerSessionNotPerSocket asserts the key that #6999
// turned on: two sessions sharing ONE socket must not share a record. Keying on
// the socket alone (the pre-#6999 behaviour) makes these equal.
func TestBridgeSingleton_PathIsPerSessionNotPerSocket(t *testing.T) {
	socket := filepath.Join("x", "y", "daemon.sock")
	a := bridgeSingletonPath(socket, "session-A")
	b := bridgeSingletonPath(socket, "session-B")
	if a == b {
		t.Fatalf("two sessions on one socket share a pidfile %q — this is #6999", a)
	}
	if a != bridgeSingletonPath(socket, "session-A") {
		t.Fatal("path not stable for a fixed (socket, session)")
	}
	other := bridgeSingletonPath(filepath.Join("x", "y", "other.sock"), "session-A")
	if other == a {
		t.Fatalf("distinct sockets share a pidfile: %q", a)
	}
	if got, want := filepath.Clean(filepath.Dir(a)), filepath.Clean(filepath.Join("x", "y")); got != want {
		t.Fatalf("pidfile not beside socket: dir=%q want %q (path %q)", got, want, a)
	}
}

// TestBridgeSessionID_DistinctPerSession covers the three identity sources by
// name: the explicit env override, the stdin identity fallback, and the pid
// fallback of last resort. Two sessions must never share an id by accident.
func TestBridgeSessionID_DistinctPerSession(t *testing.T) {
	t.Setenv(EnvBridgeSession, "alpha")
	alpha := bridgeSessionID()
	t.Setenv(EnvBridgeSession, "beta")
	beta := bridgeSessionID()
	if alpha == beta {
		t.Fatalf("explicit session ids collided: %q", alpha)
	}
	if !strings.HasPrefix(alpha, "env:") {
		t.Fatalf("explicit session id not honoured: %q", alpha)
	}

	t.Setenv(EnvBridgeSession, "   ")
	fallback := bridgeSessionID()
	if strings.HasPrefix(fallback, "env:") {
		t.Fatalf("blank env must not be honoured as a session id: %q", fallback)
	}
	if id, ok := stdinIdentity(); ok {
		if fallback != "stdin:"+id {
			t.Fatalf("fallback = %q, want stdin identity %q", fallback, "stdin:"+id)
		}
	} else if fallback != "pid:"+strconv.Itoa(os.Getpid()) {
		t.Fatalf("last-resort fallback = %q, want pid identity", fallback)
	}
}

// TestIsLiveBridge_RejectsNonGrafelPid asserts a recycled pid belonging to some
// other program is not honoured as a bridge.
func TestIsLiveBridge_RejectsNonGrafelPid(t *testing.T) {
	child := startNonGrafelChild(t)
	if isLiveBridge(child.pid) {
		t.Fatalf("pid %d is not a grafel process but was classified as a live bridge", child.pid)
	}
	if isLiveBridge(0) || isLiveBridge(-1) || isLiveBridge(999999) {
		t.Fatal("dead/invalid pids must not classify as live bridges")
	}
}

// TestIsLiveBridge_UnverifiableExecutableIsNotABridge pins the conservative
// default: when the executable behind a live pid cannot be read, the answer is
// NO. The pre-#6999 code answered YES and fed that answer to a SIGTERM, so a
// recycled pid belonging to the daemon or the engine was a valid kill target.
func TestIsLiveBridge_UnverifiableExecutableIsNotABridge(t *testing.T) {
	orig := pidIsGrafel
	t.Cleanup(func() { pidIsGrafel = orig })
	pidIsGrafel = func(int) (bool, error) { return false, errors.New("cannot enumerate processes") }

	if isLiveBridge(os.Getpid()) {
		t.Fatal("a live pid whose executable cannot be verified must not be treated as a bridge")
	}
}

// TestBridge_ExitsWhenStdinCloses establishes the premise the whole fix rests
// on: a bridge whose client is gone collects ITSELF. No external reaper is
// needed, so no bridge needs the right to signal another.
func TestBridge_ExitsWhenStdinCloses(t *testing.T) {
	isolateGrafelRoot(t)
	pr, pw := io.Pipe()
	b := &bridge{socketPath: filepath.Join(t.TempDir(), "daemon.sock")}

	done := make(chan error, 1)
	go func() { done <- b.run(pr, io.Discard) }()

	// Client goes away: its end of the pipe closes.
	_ = pw.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge.run after stdin close = %v, want nil (clean exit)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not exit when stdin closed — the self-collection premise of #6999 is false")
	}
}

// ── signal notice ────────────────────────────────────────────────────────────

// TestBridgeSignal_NoticeNamesItsCause asserts the ARTEFACT a future triage
// will read: the line must name the signal, the pidfile, the socket and #6999,
// and it must reach the daemon log, not just the client's stderr.
func TestBridgeSignal_NoticeNamesItsCause(t *testing.T) {
	root := isolateGrafelRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	var logged []string
	var exited []int
	handleBridgeSignal(syscall.SIGTERM, "/tmp/x/mcp-bridge-abc.pid", "/tmp/x/daemon.sock",
		func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) },
		func(code int) { exited = append(exited, code) })

	if len(logged) != 1 {
		t.Fatalf("want exactly one stderr line, got %v", logged)
	}
	for _, want := range []string{"terminating on signal", "mcp-bridge-abc.pid", "daemon.sock", "#6999"} {
		if !strings.Contains(logged[0], want) {
			t.Fatalf("notice does not name %q: %s", want, logged[0])
		}
	}
	if len(exited) != 1 || exited[0] != 143 {
		t.Fatalf("exit codes = %v, want [143] (128+SIGTERM)", exited)
	}
	if got := bridgeSignalExitCode(syscall.SIGINT); got != 130 {
		t.Fatalf("SIGINT exit code = %d, want 130", got)
	}

	body, err := os.ReadFile(filepath.Join(root, "logs", "daemon.log"))
	if err != nil {
		t.Fatalf("daemon log not written: %v", err)
	}
	if !strings.Contains(string(body), "mcp-bridge-abc.pid") ||
		!strings.Contains(string(body), "terminating on signal") {
		t.Fatalf("daemon log does not record the termination: %s", body)
	}
}
