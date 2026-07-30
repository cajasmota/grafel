package cli

// watcher_ctl_stop_rpc_test.go pins requirement 3 of issue #6044: the
// RPC-only stop path (no OS service installed — a manually-started
// foreground daemon) must confirm the daemon actually exited instead of
// unconditionally printing "stop requested" and returning 0, the same
// defect class as #5991's `grafel reset`.
//
// Both directions of the fixture are pinned: a daemon that honors the Stop
// RPC and tears down its socket promptly (success), and one that accepts the
// RPC but keeps the socket alive (the exact #6044 shape — a service manager,
// or here a stand-in "stubborn" daemon, that does not actually go away) must
// be reported as a genuine failure.

import (
	"bytes"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// stopStub implements just the Stop RPC method the daemon service exposes.
// closeAfter, when >= 0, closes the listener (simulating the socket going
// away as the daemon fully exits) that many milliseconds after Stop is
// called. A negative value means "never close" — the stubborn-daemon case.
type stopStub struct {
	calls      int32
	closeAfter time.Duration
	ln         net.Listener
}

func (s *stopStub) Stop(_ proto.StopArgs, _ *proto.StopReply) error {
	atomic.AddInt32(&s.calls, 1)
	if s.closeAfter >= 0 {
		go func() {
			time.Sleep(s.closeAfter)
			_ = s.ln.Close()
		}()
	}
	return nil
}

// serveStopStub starts a fake daemon on a sandboxed GRAFEL_DAEMON_ROOT
// exposing only Stop, and returns the stub for assertions.
func serveStopStub(t *testing.T, closeAfter time.Duration) *stopStub {
	t.Helper()
	root, err := os.MkdirTemp("", "ag-stop-")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv(daemon.EnvRoot, root)

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if layout.SocketDir != "" {
		if err := os.MkdirAll(layout.SocketDir, 0o755); err != nil {
			t.Fatalf("mkdir socket dir: %v", err)
		}
	}
	ln, err := transport.Listen(layout.SocketPath)
	if err != nil {
		t.Fatalf("listen %s: %v", layout.SocketPath, err)
	}
	stub := &stopStub{closeAfter: closeAfter, ln: ln}
	t.Cleanup(func() { _ = ln.Close() })

	srv := rpc.NewServer()
	if err := srv.RegisterName(proto.ServiceName, stub); err != nil {
		t.Fatalf("register: %v", err)
	}
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()
	return stub
}

// stopConfirmTimeoutForTest overrides the package-level stopConfirmTimeout
// for the duration of a test and returns a restore closure, so tests don't
// have to wait out the real 5s production budget.
func stopConfirmTimeoutForTest(d time.Duration) (restore func()) {
	prev := stopConfirmTimeout
	stopConfirmTimeout = d
	return func() { stopConfirmTimeout = prev }
}

// TestRunDaemonStop_RPCPath_ConfirmsRealExit is the fixture-validity anchor:
// a daemon that honors Stop and promptly tears down its socket must be
// reported as a genuine, confirmed success.
func TestRunDaemonStop_RPCPath_ConfirmsRealExit(t *testing.T) {
	stubStopSeam(t, false) // force the RPC path, not the service path
	serveStopStub(t, 50*time.Millisecond)

	var out bytes.Buffer
	if err := runDaemonStop(&out); err != nil {
		t.Fatalf("runDaemonStop: %v", err)
	}
	if !strings.Contains(out.String(), "daemon stopped") {
		t.Fatalf("expected a confirmed 'daemon stopped' message, got %q", out.String())
	}
}

// TestRunDaemonStop_RPCPath_ReportsFailureWhenStillRunning is the OTHER
// direction of the same fixture — and the actual #6044 pin: a daemon that
// accepts the Stop RPC but does NOT actually go away (its socket stays live)
// must make `grafel stop` fail loudly, not print "stop requested" and exit 0.
func TestRunDaemonStop_RPCPath_ReportsFailureWhenStillRunning(t *testing.T) {
	stubStopSeam(t, false) // force the RPC path, not the service path
	stub := serveStopStub(t, -1*time.Second)

	orig := stopConfirmTimeoutForTest(300 * time.Millisecond)
	defer orig()

	var out bytes.Buffer
	err := runDaemonStop(&out)
	if err == nil {
		t.Fatalf("expected runDaemonStop to fail when the daemon is still running after Stop, got success (output: %q)", out.String())
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Fatalf("expected the error to say the daemon is still running, got %q", err.Error())
	}
	if atomic.LoadInt32(&stub.calls) != 1 {
		t.Fatalf("expected exactly one Stop RPC call, got %d", stub.calls)
	}
	if strings.Contains(out.String(), "daemon stopped") {
		t.Fatalf("must not print the success message when the daemon did not actually stop, got %q", out.String())
	}
}
