package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

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

// runDaemonForTest starts daemon.Run in a goroutine and returns the
// layout (for dialing) plus a stop function. The returned context is
// the one the daemon listens on — cancel it to force shutdown if Stop
// over RPC is what's being exercised.
func runDaemonForTest(t *testing.T, idx daemon.IndexFunc, rb daemon.RebuildFunc) daemon.Layout {
	t.Helper()
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
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
	waitDaemonReady(t, layout.SocketPath, 3*time.Second)
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
		return filepath.Join(args.RepoPath, ".archigraph", "graph.json"),
			`{"repo":"` + args.RepoPath + `","files":42}`, nil
	}
	layout := runDaemonForTest(t, idx, nil)
	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	reply, err := c.Index(proto.IndexArgs{RepoPath: "/tmp/fake-repo"})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if reply.GraphPath != "/tmp/fake-repo/.archigraph/graph.json" {
		t.Fatalf("graph path: %q", reply.GraphPath)
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
		addr = `\\.\pipe\archigraph-test-notrunning-` + t.Name()
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
