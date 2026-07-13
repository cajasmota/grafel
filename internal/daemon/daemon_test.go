package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestMain fail-closes the daemon package: when
// GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1 it refuses to run if HOME is the real
// user home and no GRAFEL_DAEMON_ROOT isolation is in effect — these tests
// start an in-process daemon and dial its socket, and must never displace or
// dial the developer's live daemon.
//
// It also pins this test binary's default to MONOLITH mode (ADR-0024 PR6,
// epic #5729): daemon.SplitModeEnabled() is ON by default in production, but
// the broad daemon test suite starts in-process daemons via daemon.Run /
// daemon.RunServe without a real `grafel` binary on disk — os.Executable()
// in a test binary resolves to the test binary itself, not `grafel`, so a
// real split-mode engine-child spawn would fail or hang. Setting
// GRAFEL_SPLIT_MODE=0 here (only if the environment doesn't already specify
// it) keeps the suite running monolith by default. Tests that specifically
// exercise split mode override this per-test via t.Setenv(GRAFEL_SPLIT_MODE,
// "1") (which restores to this TestMain default afterward) and/or
// daemon.SetEngineChildCommandForTest to substitute a safe in-binary helper
// process for the engine child — see serve_split_e2e_test.go and
// serve_engine_split_test.go.
func TestMain(m *testing.M) {
	if _, set := os.LookupEnv(daemon.SplitModeEnvVar); !set {
		os.Setenv(daemon.SplitModeEnvVar, "0")
	}
	testsupport.GuardRealHomeMain()
	os.Exit(m.Run())
}

// waitDaemonReady polls until the daemon at socketPath accepts a dial.
// On Unix this is equivalent to os.Stat + Dial; on Windows named pipes are
// not filesystem objects so only the dial attempt is meaningful.
func waitDaemonReady(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := client.DialPath(socketPath)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon never became ready at %s within %s", socketPath, timeout)
}

// shortTempRoot returns a directory short enough for macOS's AF_UNIX
// sun_path limit (~103 bytes). On macOS, t.TempDir() routes through
// TMPDIR (/var/folders/...) which can exceed the limit when combined
// with the daemon's "sockets/daemon.sock" suffix. On Windows the
// socket is a named pipe so the path length constraint does not apply;
// os.TempDir() (typically C:\Users\...\AppData\Local\Temp) is used
// directly. On Linux, /tmp is always short enough.
func shortTempRoot(t *testing.T) string {
	t.Helper()
	tmpBase := "/tmp"
	if runtime.GOOS == "windows" {
		// Windows: named pipes don't have path-length limits; use the
		// system temp dir which is always writable.
		tmpBase = os.TempDir()
	}
	d, err := os.MkdirTemp(tmpBase, "archi-dt-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// isolateDaemonEnv points this test's in-process daemon at a per-test temp
// root (its own socket, pid file and log dir) and disables the Layer-1
// self-defense check (#4022).
//
// Without the self-defense override these tests are NOT hermetic: `go test`
// compiles and runs the test binary under /tmp, so SelfDefenseCheck treats the
// in-test daemon as an ephemeral /tmp daemon and refuses to start it whenever a
// real canonical daemon is running on the machine — surfacing as the
// "daemon never became ready" timeouts this change fixes. The in-test daemon
// uses an isolated root + socket and never touches the canonical socket, so
// skipping the anti-displacement check is correct here.
//
// It returns the temp root so callers can build a Layout from it.
func isolateDaemonEnv(t *testing.T) string {
	t.Helper()
	root := shortTempRoot(t)
	// Safety: the isolated daemon root must never resolve under the real user
	// home — otherwise EnsureLayout/Run would write the socket/pid/log into the
	// developer's live ~/.grafel and a Dial() could hit the live daemon.
	if rh := testsupport.RealUserHome(); rh != "" {
		if rel, err := filepath.Rel(rh, root); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			t.Fatalf("isolateDaemonEnv: temp daemon root %q is under the real user home %q — refusing", root, rh)
		}
	}
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv(daemon.EnvDisableSelfDefense, "1")
	return root
}

// runDaemonForTest starts daemon.Run in a goroutine and returns the
// layout (for dialing) plus a stop function. The returned context is
// the one the daemon listens on — cancel it to force shutdown if Stop
// over RPC is what's being exercised.
func runDaemonForTest(t *testing.T, idx daemon.IndexFunc, rb daemon.RebuildFunc) daemon.Layout {
	t.Helper()
	isolateDaemonEnv(t)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx, daemon.Config{
			Layout:  layout,
			Index:   idx,
			Rebuild: rb,
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Logf("daemon did not exit within 3s")
		}
	})
	// Wait for the daemon to become ready.
	// On Unix we could stat the socket file, but named pipes on Windows are
	// not filesystem objects — use a dial-based readiness probe that works on
	// all platforms.
	//
	// 10s timeout: a loaded ubuntu CI runner under -race occasionally exceeded
	// the prior 3s window, causing TestDaemon_PingStatus and friends to flake
	// even though the daemon log showed it was ready. 10s is generous enough
	// for any reasonable CI runner while still failing fast on real bugs.
	waitDaemonReady(t, layout.SocketPath, 10*time.Second)
	return layout
}

// TestDaemon_PingStatus verifies the most basic RPC round-trip and that
// Status reports a sensible RSS/pid/uptime. Anything reporting "RSS=0"
// would be a clear sign the daemon never warmed up memstats.
func TestDaemon_PingStatus(t *testing.T) {
	layout := runDaemonForTest(t, nil, nil)
	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if r, err := c.Ping(); err != nil || r.Version == "" {
		t.Fatalf("ping: %v %+v", err, r)
	}
	st, err := c.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.PID == 0 || st.RSSBytes == 0 || st.SocketPath != layout.SocketPath {
		t.Fatalf("status looks wrong: %+v", st)
	}
}

// TestDaemon_IndexRPC stubs an IndexFunc and asserts the wire surface
// (arg shape, stats handoff) before any real extractor runs.
func TestDaemon_IndexRPC(t *testing.T) {
	idx := func(args proto.IndexArgs) (string, string, error) {
		if args.RepoPath == "" {
			return "", "", errors.New("empty repo")
		}
		return filepath.Join(args.RepoPath, ".grafel", "graph.json"),
			`{"repo":"` + args.RepoPath + `","files":42}`, nil
	}
	layout := runDaemonForTest(t, idx, nil)
	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	repoPath := filepath.Join(string(filepath.Separator)+"tmp", "fake-repo")
	reply, err := c.Index(proto.IndexArgs{RepoPath: repoPath})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	wantGraphPath := filepath.Join(repoPath, ".grafel", "graph.json")
	if reply.GraphPath != wantGraphPath {
		t.Fatalf("graph path: got %q want %q", reply.GraphPath, wantGraphPath)
	}
	if reply.StatsJSON == "" {
		t.Fatalf("stats empty")
	}
}

// TestDaemon_StopRPC closes the daemon over RPC and asserts that a
// subsequent dial reports ErrDaemonNotRunning, exactly as the CLI's
// thin clients depend on.
func TestDaemon_StopRPC(t *testing.T) {
	layout := runDaemonForTest(t, nil, nil)
	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	_ = c.Close()
	// Wait for the daemon to stop accepting connections.
	// On Unix we could check os.Stat(socketPath) for absence, but named pipes
	// on Windows are not filesystem objects. Instead we poll until DialPath
	// returns ErrDaemonNotRunning, which works on all platforms.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.GOOS != "windows" {
			// Fast path on Unix: socket file disappears on clean shutdown.
			if _, err := os.Stat(layout.SocketPath); os.IsNotExist(err) {
				return
			}
		} else {
			// Windows: poll until the pipe rejects new connections.
			if _, err := client.DialPath(layout.SocketPath); errors.Is(err, client.ErrDaemonNotRunning) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("daemon at %s did not shut down after stop", layout.SocketPath)
}

// TestDaemon_ClientReportsNotRunning probes the canonical "no daemon"
// case: dialing a socket path that doesn't exist returns
// ErrDaemonNotRunning, not some opaque syscall error.
func TestDaemon_ClientReportsNotRunning(t *testing.T) {
	var addr string
	if runtime.GOOS == "windows" {
		// Named pipes that are not listening return ErrDaemonNotRunning.
		// Use a test-specific pipe name that will never be listening.
		addr = `\\.\pipe\grafel-test-notrunning-` + t.Name()
	} else {
		addr = filepath.Join(shortTempRoot(t), "nope.sock")
	}
	_, err := client.DialPath(addr)
	if !errors.Is(err, client.ErrDaemonNotRunning) {
		t.Fatalf("want ErrDaemonNotRunning, got %v", err)
	}
}

// TestDaemon_RebuildGroupSerialisedUnderLoad verifies #2097's per-group
// mutex: two concurrent Rebuild RPCs for the SAME group must not overlap
// execution of the RebuildFunc. The test detects overlapping execution by
// counting peak concurrency inside the stub RebuildFunc and asserting it
// never exceeds 1.
func TestDaemon_RebuildGroupSerialisedUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("group-serialisation test skipped in short mode")
	}

	var current, peakConc int64
	rb := func(args proto.RebuildArgs) ([]string, string, error) {
		cur := atomic.AddInt64(&current, 1)
		defer atomic.AddInt64(&current, -1)
		// Record peak.
		for {
			pk := atomic.LoadInt64(&peakConc)
			if cur <= pk || atomic.CompareAndSwapInt64(&peakConc, pk, cur) {
				break
			}
		}
		// Simulate non-trivial work so the two goroutines can overlap if
		// the mutex is absent.
		time.Sleep(40 * time.Millisecond)
		return []string{"/tmp/fake"}, "", nil
	}

	layout := runDaemonForTest(t, nil, rb)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := client.DialPath(layout.SocketPath)
			if err != nil {
				t.Logf("dial: %v", err)
				return
			}
			defer c.Close()
			// All four RPCs target the same group — serialisation is required.
			_, _ = c.Rebuild(proto.RebuildArgs{Group: "test-group"})
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("concurrent same-group Rebuild RPCs hung after 15s")
	}

	peak := atomic.LoadInt64(&peakConc)
	if peak > 1 {
		t.Errorf("peak concurrent RebuildFunc executions = %d for same group, want ≤1 (#2097 per-group mutex)", peak)
	}
}

// TestDaemon_RebuildStatusObservability verifies the new #2097 fields in
// StatusReply: RebuildInFlight and RebuildGroupsActive are non-negative and
// consistent while a rebuild is in flight.
func TestDaemon_RebuildStatusObservability(t *testing.T) {
	if testing.Short() {
		t.Skip("rebuild observability test skipped in short mode")
	}

	// RebuildFunc that blocks until signalled, so we can observe Status mid-run.
	unblock := make(chan struct{})
	rb := func(args proto.RebuildArgs) ([]string, string, error) {
		<-unblock
		return []string{"/tmp/fake"}, "", nil
	}

	layout := runDaemonForTest(t, nil, rb)

	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Start a rebuild in the background.
	rebuildDone := make(chan error, 1)
	go func() {
		_, err := c.Rebuild(proto.RebuildArgs{Group: "obs-group"})
		rebuildDone <- err
	}()

	// Poll until a rebuild is actually in flight.
	deadline := time.Now().Add(3 * time.Second)
	var rebuilding bool
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		// Need a second connection for the status poll (the first is
		// blocked on the Rebuild RPC).
		sc, serr := client.DialPath(layout.SocketPath)
		if serr != nil {
			continue
		}
		st, sterr := sc.Status()
		sc.Close()
		if sterr != nil {
			continue
		}
		if st.RebuildGroupsActive > 0 || st.RebuildInFlight > 0 {
			rebuilding = true
			break
		}
	}

	// Unblock the rebuild.
	close(unblock)
	select {
	case <-rebuildDone:
	case <-time.After(5 * time.Second):
		t.Fatal("rebuild never completed after unblock")
	}

	if !rebuilding {
		t.Error("RebuildGroupsActive and RebuildInFlight were both 0 while a rebuild was running")
	}
}
