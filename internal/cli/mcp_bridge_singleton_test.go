package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
)

// TestBridgeSingleton_ClaimsAndReleases asserts the happy path: a clean
// acquire writes our pid to the per-session pidfile and release removes it.
func TestBridgeSingleton_ClaimsAndReleases(t *testing.T) {
	t.Setenv(EnvBridgeSession, "sess-claims")
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")

	release, path, err := acquireBridgeSingleton(dir, socket, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if want := bridgeSingletonPath(dir, socket, bridgeSessionID()); path != want {
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

// TestBridgeSingleton_ReleaseLeavesANewerOwnersRecord grades the `cur == self`
// guard in the release closure. Two bridges can share a session id (a shared
// GRAFEL_MCP_SESSION, or two stdins on /dev/null), and then the second one's
// acquire overwrites the first one's record. When the FIRST bridge later exits,
// an unconditional os.Remove would delete the record of a bridge that is still
// serving — so the record is asserted, not a counter: the file must still exist
// and must still name the newer owner.
func TestBridgeSingleton_ReleaseLeavesANewerOwnersRecord(t *testing.T) {
	t.Setenv(EnvBridgeSession, "sess-newer-owner")
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")

	release, path, err := acquireBridgeSingleton(dir, socket, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if pid, ok := readBridgePID(path); !ok || pid != os.Getpid() {
		t.Fatalf("precondition: record pid = %d (ok=%v), want %d", pid, ok, os.Getpid())
	}

	// A newer bridge for the same session claims the record.
	const newer = 999123
	if newer == os.Getpid() {
		t.Fatalf("pick a pid that is not ours: %d", newer)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(newer)+"\n"), 0o600); err != nil {
		t.Fatalf("seed newer owner: %v", err)
	}

	release()

	pid, ok := readBridgePID(path)
	if !ok {
		t.Fatalf("release deleted a record it no longer owns: %s is gone, but pid %d owns it", path, newer)
	}
	if pid != newer {
		t.Fatalf("record pid = %d, want %d — release must not disturb a newer owner's record", pid, newer)
	}
}

// TestBridgeSingleton_StalePidOverwritten asserts that a pidfile naming a DEAD
// process is silently overwritten — a crashed bridge must not wedge the next
// session.
func TestBridgeSingleton_StalePidOverwritten(t *testing.T) {
	t.Setenv(EnvBridgeSession, "sess-stale")
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	path := bridgeSingletonPath(dir, socket, bridgeSessionID())

	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	release, _, err := acquireBridgeSingleton(dir, socket, nil)
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
	path := bridgeSingletonPath(dir, socket, bridgeSessionID())

	child := startFakeGrafelChild(t)
	if !isLiveBridge(child.pid) {
		t.Fatalf("precondition: child pid %d must classify as a live grafel process; "+
			"without it this test cannot observe a reap", child.pid)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(child.pid)+"\n"), 0o600); err != nil {
		t.Fatalf("seed prior pid: %v", err)
	}

	var logged []string
	release, _, err := acquireBridgeSingleton(dir, socket, func(f string, a ...any) {
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
	dir := filepath.Join("x", "y", "sockets")
	socket := filepath.Join("x", "y", "daemon.sock")
	a := bridgeSingletonPath(dir, socket, "session-A")
	b := bridgeSingletonPath(dir, socket, "session-B")
	if a == b {
		t.Fatalf("two sessions on one socket share a pidfile %q — this is #6999", a)
	}
	if a != bridgeSingletonPath(dir, socket, "session-A") {
		t.Fatal("path not stable for a fixed (dir, socket, session)")
	}
	other := bridgeSingletonPath(dir, filepath.Join("x", "y", "other.sock"), "session-A")
	if other == a {
		t.Fatalf("distinct sockets share a pidfile: %q", a)
	}
	if got, want := filepath.Clean(filepath.Dir(a)), filepath.Clean(dir); got != want {
		t.Fatalf("pidfile not in the record dir: dir=%q want %q (path %q)", got, want, a)
	}
}

// TestBridgeRecordDir_IsAWritableDirectoryOnEveryPlatform is the pin the
// Windows CI leg needed and nobody had. It runs everywhere, unskipped, and it
// asserts the property the record depends on: the directory the record goes in
// is a REAL, writable filesystem directory derived from the layout ROOT.
//
// Before this, the record dir was filepath.Dir(Layout.SocketPath). On Windows
// SocketPath is a named pipe (`\\.\pipe\grafel-<hash>`) and SocketDir is
// deliberately "", so that expression yielded `\\.\pipe` and every record write
// failed — silently, because acquire's error is non-fatal by design. The
// consequence ran both ways: no diagnostic record existed on Windows, and the
// pre-#6999 reap could never fire there either, since readBridgePID never
// returned a prior pid.
//
// It writes through acquireBridgeSingleton rather than asserting a string, so
// a path that merely LOOKS filesystem-shaped on a platform still fails here.
//
// Scope, stated honestly: the DERIVATION (root vs socket dir) is only
// distinguishable where the two differ, i.e. on Windows — under GRAFEL_DAEMON_ROOT
// the unix socket already lives at <root>/sockets, so on unix the old and new
// expressions coincide and the equality assertion below cannot separate them.
// What this test does grade on every platform is that the dir is created and
// written through, which is the half that never happened on Windows.
func TestBridgeRecordDir_IsAWritableDirectoryOnEveryPlatform(t *testing.T) {
	root := isolateGrafelRoot(t)
	t.Setenv(EnvBridgeSession, "sess-record-dir")

	dir, err := bridgeRecordDir()
	if err != nil {
		t.Fatalf("bridgeRecordDir: %v", err)
	}
	if want := filepath.Join(root, "sockets"); dir != want {
		t.Fatalf("record dir = %q, want %q (it must come from Layout.Root)", dir, want)
	}

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	// The regression itself, stated directly: the socket's own directory is not
	// usable as the record dir on every platform, so it must not be the source.
	if runtime.GOOS == "windows" && filepath.Dir(layout.SocketPath) == dir {
		t.Fatalf("record dir %q was derived from the named pipe %q", dir, layout.SocketPath)
	}

	// Remove the dir first: nothing else creates it on Windows, so acquire has
	// to. This is the step the old code had no way to perform.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear record dir: %v", err)
	}
	release, path, err := acquireBridgeSingleton(dir, layout.SocketPath, nil)
	if err != nil {
		t.Fatalf("acquire against the real layout socket %q failed: %v — "+
			"the ownership record does not exist on this platform", layout.SocketPath, err)
	}
	defer release()
	if pid, ok := readBridgePID(path); !ok || pid != os.Getpid() {
		t.Fatalf("record %q pid = %d (ok=%v), want %d", path, pid, ok, os.Getpid())
	}
	if got := filepath.Dir(path); got != dir {
		t.Fatalf("record written to %q, want it inside %q", got, dir)
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
// on: a bridge that is IDLE when its client goes collects ITSELF. No external
// reaper is needed, so no bridge needs the right to signal another.
//
// Scope, deliberately narrow: this runs an idle in-process bridge over an
// io.Pipe, so it pins EOF-while-idle only. It does NOT observe a bridge blocked
// in a daemon call, which never returns to its read loop and so never sees the
// EOF — that residual is #7003, and the header of mcp_bridge_singleton.go says
// so rather than claiming this test covers every client-less bridge.
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

	// daemon.log is the DAEMON's log, not a bridge-private file: the daemon and
	// other bridges append to it concurrently. Seed prior content so the write
	// offset is observable — without this the test writes the file from empty
	// and a clobber cannot be seen by construction.
	logPath := filepath.Join(root, "logs", "daemon.log")
	prior := "2026/09/02 10:00:00 daemon: graceful shutdown complete\n"
	if err := os.WriteFile(logPath, []byte(prior), 0o600); err != nil {
		t.Fatalf("seed daemon log: %v", err)
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

	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("daemon log not written: %v", err)
	}
	if !strings.Contains(string(body), "mcp-bridge-abc.pid") ||
		!strings.Contains(string(body), "terminating on signal") {
		t.Fatalf("daemon log does not record the termination: %s", body)
	}
	// HasPrefix, not Contains: the failure mode is a write at the wrong OFFSET
	// (an open without O_APPEND lands the notice at byte 0, destroying the head
	// of the daemon's log and leaving a corrupt fragment). Contains(prior) still
	// passes when the notice is written in front of the prior line, so it cannot
	// grade this; only "the prior bytes are still first" can.
	if !strings.HasPrefix(string(body), prior) {
		t.Fatalf("the bridge overwrote pre-existing daemon log content — daemon.log is shared "+
			"with the daemon and with concurrent bridges, so the notice must be APPENDED:\n%s", body)
	}
}
