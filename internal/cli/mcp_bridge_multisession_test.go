package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/process"
)

// mcp_bridge_multisession_test.go — the #6999 artefact test.
//
// #6999 is a cross-process defect: a bridge SIGTERMed OTHER processes. No
// in-process assertion can observe it, so these tests run real bridge processes
// and assert on what survives. The children are copies of this test binary
// named "grafel", because process.PidIsGrafel gates the (now deleted) reap on
// the executable's name: with a child called "cli.test" the old code would have
// skipped the reap and the test would have passed against the bug.

const envBridgeChildMode = "GRAFEL_TEST_BRIDGE_CHILD"

// TestHelperBridgeChild is the child entrypoint. It is inert unless the parent
// sets envBridgeChildMode, so a normal `go test` run never enters it.
func TestHelperBridgeChild(t *testing.T) {
	switch os.Getenv(envBridgeChildMode) {
	case "":
		t.Skip("child entrypoint; not run directly")
	case "bridge":
		// A real bridge over the parent's pipes, using the DEFAULT socket path
		// (GRAFEL_DAEMON_ROOT points it at the parent's temp root) so the
		// production singleton path is the one under test.
		b := &bridge{}
		_ = b.run(os.Stdin, os.Stdout)
		os.Exit(0)
	case "idle":
		// A live, grafel-named process that is NOT a bridge: the reap target
		// stand-in. Exits when its stdin closes or after a bounded wait.
		done := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(done) }()
		select {
		case <-done:
		case <-time.After(60 * time.Second):
		}
		os.Exit(0)
	}
}

// ── harness ──────────────────────────────────────────────────────────────────

type childProc struct {
	t    *testing.T
	cmd  *exec.Cmd
	pid  int
	in   io.WriteCloser
	out  *bufio.Reader
	wait chan error
}

// isolateGrafelRoot points the daemon layout at a per-test temp root. Every
// test in this file MUST call it: without it the bridge resolves the real
// ~/.grafel and would write (pre-#6999, signal) into the developer's live
// daemon directory.
func isolateGrafelRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sockets"), 0o755); err != nil {
		t.Fatalf("mkdir sockets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	t.Setenv("GRAFEL_DAEMON_ROOT", root)
	t.Setenv("GRAFEL_HOME", root)
	return root
}

// grafelNamedTestBinary copies this test binary to a file named "grafel" so
// children are classified by process.PidIsGrafel exactly as a real bridge is.
func grafelNamedTestBinary(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate test binary: %v", err)
	}
	src, err := os.ReadFile(self)
	if err != nil {
		t.Skipf("cannot read test binary: %v", err)
	}
	name := "grafel"
	if runtime.GOOS == "windows" {
		name = "grafel.exe"
	}
	// Not t.TempDir(): the binary must outlive per-subtest cleanup ordering.
	dir, err := os.MkdirTemp("", "grafel-bin")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, src, 0o755); err != nil {
		t.Skipf("cannot copy test binary: %v", err)
	}
	return dst
}

// startChild launches a grafel-named child in the given mode and cwd.
func startChild(t *testing.T, exe, mode, cwd string, extraEnv ...string) *childProc {
	t.Helper()
	cmd := exec.Command(exe, "-test.run=^TestHelperBridgeChild$", "-test.timeout=120s")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		envBridgeChildMode+"="+mode,
		"GRAFEL_DAEMON_ROOT="+os.Getenv("GRAFEL_DAEMON_ROOT"),
		"GRAFEL_HOME="+os.Getenv("GRAFEL_HOME"),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start child: %v", err)
	}
	c := &childProc{t: t, cmd: cmd, pid: cmd.Process.Pid, in: in, out: bufio.NewReader(out), wait: make(chan error, 1)}
	go func() { c.wait <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Process.Kill()
		<-c.wait
	})
	return c
}

// serves sends one MCP `initialize` and returns true if a well-formed response
// comes back — i.e. this bridge is alive AND still doing its job.
func (c *childProc) serves(id int) bool {
	c.t.Helper()
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"initialize","params":{}}`+"\n", id)
	if _, err := io.WriteString(c.in, req); err != nil {
		c.t.Logf("pid %d: write: %v", c.pid, err)
		return false
	}
	type resp struct {
		ID     float64         `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	line := make(chan string, 1)
	go func() {
		s, err := c.out.ReadString('\n')
		if err != nil && s == "" {
			line <- ""
			return
		}
		line <- s
	}()
	select {
	case s := <-line:
		var r resp
		if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &r); err != nil {
			c.t.Logf("pid %d: bad response %q: %v", c.pid, s, err)
			return false
		}
		return int(r.ID) == id && len(r.Result) > 0
	case <-time.After(10 * time.Second):
		c.t.Logf("pid %d: no response within 10s", c.pid)
		return false
	}
}

func (c *childProc) exited() bool {
	select {
	case err := <-c.wait:
		c.wait <- err
		return true
	default:
		return false
	}
}

// TestBridge_ConcurrentSessionsAllSurviveAndServe is the headline assertion of
// #6999: N bridges started against ONE daemon socket from different working
// directories all stay alive and all keep serving. Before the fix, each new
// bridge SIGTERMed the incumbent and only the newest survived.
//
// Each member of the set is asserted individually — a second project and a
// git worktree of the first are distinct members, not one representative,
// because those are the two shapes #6999 was reported in.
func TestBridge_ConcurrentSessionsAllSurviveAndServe(t *testing.T) {
	root := isolateGrafelRoot(t)
	exe := grafelNamedTestBinary(t)

	work := t.TempDir()
	projectA := filepath.Join(work, "project-a")
	projectB := filepath.Join(work, "project-b")
	worktreeA := filepath.Join(projectA, ".worktrees", "tsk_1fce9975")
	for _, d := range []string{projectA, projectB, worktreeA} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	sessions := []struct {
		name string
		cwd  string
	}{
		{"project-a", projectA},
		{"project-b (different project, same socket)", projectB},
		{"worktree of project-a", worktreeA},
	}

	var started []*childProc
	for _, s := range sessions {
		c := startChild(t, exe, "bridge", s.cwd)
		// Each bridge must be serving before the next one starts, so that the
		// next one's startup is what we are testing.
		if !c.serves(1) {
			t.Fatalf("session %q did not come up", s.name)
		}
		// Every session started BEFORE this one must still be serving.
		for i, prev := range started {
			if prev.exited() {
				t.Fatalf("session %q (pid %d) DIED when session %q started — a bridge terminated another session's bridge (#6999)",
					sessions[i].name, prev.pid, s.name)
			}
			if !prev.serves(100 + i) {
				t.Fatalf("session %q (pid %d) stopped serving when session %q started (#6999)",
					sessions[i].name, prev.pid, s.name)
			}
		}
		started = append(started, c)
	}

	// Every session must own a DISTINCT ownership record naming its own pid.
	// Under the pre-#6999 per-socket key there is exactly one such file for the
	// whole machine.
	// Enumerate the PRODUCTION record dir, not a fabricated one: deriving it
	// here the same way the bridge does is what exposed the Windows gap, where
	// filepath.Dir(SocketPath) is the named pipe `\\.\pipe` and no record was
	// ever written at all.
	recordDir, err := bridgeRecordDir()
	if err != nil {
		t.Fatalf("bridge record dir: %v", err)
	}
	if want := filepath.Join(root, "sockets"); recordDir != want {
		t.Fatalf("record dir = %q, want %q", recordDir, want)
	}
	entries, err := os.ReadDir(recordDir)
	if err != nil {
		t.Fatalf("read bridge record dir %s: %v", recordDir, err)
	}
	recorded := map[int]string{}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "mcp-bridge-") || !strings.HasSuffix(e.Name(), ".pid") {
			continue
		}
		p := filepath.Join(recordDir, e.Name())
		pid, ok := readBridgePID(p)
		if !ok {
			t.Fatalf("unreadable bridge record %s", p)
		}
		if other, dup := recorded[pid]; dup {
			t.Fatalf("pid %d recorded twice (%s and %s)", pid, other, p)
		}
		recorded[pid] = p
	}
	for i, c := range started {
		if _, ok := recorded[c.pid]; !ok {
			t.Fatalf("session %q (pid %d) has no ownership record of its own; records=%v — the key is not per-session (#6999)",
				sessions[i].name, c.pid, recorded)
		}
	}
	if len(recorded) != len(started) {
		t.Fatalf("want %d distinct bridge records, got %d: %v", len(started), len(recorded), recorded)
	}
}

// TestBridge_DoesNotSignalAnotherLiveGrafelProcess is the negative: starting a
// bridge must leave every other grafel process alone, including one whose pid a
// stale record names. Pre-#6999 that record was machine-global AND
// PidIsGrafel matched any grafel process, so this pid was SIGTERMed.
func TestBridge_DoesNotSignalAnotherLiveGrafelProcess(t *testing.T) {
	root := isolateGrafelRoot(t)
	exe := grafelNamedTestBinary(t)
	cwd := t.TempDir()

	victim := startChild(t, exe, "idle", cwd)
	if !process.IsAlive(victim.pid) {
		t.Fatalf("victim pid %d not alive", victim.pid)
	}
	if ok, err := process.PidIsGrafel(victim.pid); err != nil || !ok {
		t.Skipf("cannot classify victim as a grafel process (ok=%v err=%v); "+
			"without that this test cannot observe a reap", ok, err)
	}

	// Seed the victim's pid into EVERY plausible record path: the per-socket
	// path the pre-#6999 code used, and this process's own session path.
	socket := filepath.Join(root, "sockets", "daemon.sock")
	seed := []string{
		legacyPerSocketPidfilePath(socket),
		bridgeSingletonPath(filepath.Join(root, "sockets"), socket, bridgeSessionID()),
	}
	for _, p := range seed {
		if err := os.WriteFile(p, []byte(strconv.Itoa(victim.pid)+"\n"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}

	b := startChild(t, exe, "bridge", cwd)
	if !b.serves(1) {
		t.Fatal("bridge did not come up")
	}
	time.Sleep(300 * time.Millisecond) // the pre-#6999 reap grace was 500ms; SIGTERM itself is instant
	if victim.exited() {
		t.Fatalf("a starting bridge terminated another live grafel process (pid %d) — #6999", victim.pid)
	}
}

// legacyPerSocketPidfilePath reproduces the pre-#6999 per-socket key so a test
// can seed the exact file the old code read. It is test-only: production must
// never derive a path from the socket alone again.
func legacyPerSocketPidfilePath(socketPath string) string {
	sum := sha256.Sum256([]byte(socketPath))
	return filepath.Join(filepath.Dir(socketPath), "mcp-bridge-"+hex.EncodeToString(sum[:6])+".pid")
}

// TestBridge_SignalledBridgeNamesItselfInTheDaemonLog asserts the second half
// of the fix end-to-end, through the production wiring: a REAL bridge process
// (bridge.run, which is what arms the handler) is SIGTERMed the way a
// pre-#6999 bridge was, and must leave one line in ~/.grafel/logs/daemon.log
// naming its pidfile, its socket and #6999 — while still exiting 143.
//
// This is the artefact that turns the next occurrence into a five-minute
// triage instead of an excavation: before the fix the reaped bridge died on the
// default SIGTERM disposition and wrote nothing anywhere.
func TestBridge_SignalledBridgeNamesItselfInTheDaemonLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM delivery is unix-only")
	}
	root := isolateGrafelRoot(t)
	exe := grafelNamedTestBinary(t)

	c := startChild(t, exe, "bridge", t.TempDir())
	if !c.serves(1) {
		t.Fatal("bridge did not come up")
	}

	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}

	var werr error
	select {
	case werr = <-c.wait:
		c.wait <- werr
	case <-time.After(20 * time.Second):
		t.Fatal("signalled bridge did not exit")
	}
	code := 0
	if ee, ok := werr.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if werr != nil {
		t.Fatalf("child: %v", werr)
	}
	if code != 143 {
		t.Fatalf("signalled bridge exit code = %d, want 143 (128+SIGTERM)", code)
	}

	body, rerr := os.ReadFile(filepath.Join(root, "logs", "daemon.log"))
	if rerr != nil {
		t.Fatalf("no daemon log line for a signalled bridge: %v", rerr)
	}
	socket := filepath.Join(root, "sockets", "daemon.sock")
	for _, want := range []string{"terminating on signal", "#6999", "socket=" + socket, "pidfile=" + filepath.Join(root, "sockets", "mcp-bridge-")} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("daemon log does not name %q:\n%s", want, body)
		}
	}
	// One death, one line.
	if n := strings.Count(string(body), "terminating on signal"); n != 1 {
		t.Fatalf("daemon log has %d termination lines, want exactly 1:\n%s", n, body)
	}
}

// ── shared helpers for the singleton unit tests ──────────────────────────────

// startFakeGrafelChild starts a live process that process.PidIsGrafel
// classifies as grafel — the stand-in for a prior bridge.
func startFakeGrafelChild(t *testing.T) *childProc {
	t.Helper()
	isolateGrafelRoot(t)
	return startChild(t, grafelNamedTestBinary(t), "idle", t.TempDir())
}

// startNonGrafelChild starts a live process that is NOT grafel (this test
// binary under its own name) — the recycled-pid stand-in.
func startNonGrafelChild(t *testing.T) *childProc {
	t.Helper()
	isolateGrafelRoot(t)
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate test binary: %v", err)
	}
	if strings.Contains(strings.ToLower(filepath.Base(self)), "grafel") {
		t.Skipf("test binary %q is itself named grafel; cannot model a non-grafel pid", self)
	}
	return startChild(t, self, "idle", t.TempDir())
}

// aliveAfter reports whether the child is still running after d.
func (c *childProc) aliveAfter(d time.Duration) bool {
	time.Sleep(d)
	return !c.exited() && process.IsAlive(c.pid)
}

func anyContains(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
