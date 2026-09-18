package daemon_test

// startup_signal_test.go — the startup and RPC-arrival anchors this package's
// shutdown tests wait on (#7228).
//
// waitDaemonReady (daemon_test.go) returns as soon as the daemon's socket
// accepts a dial. On Unix the socket is bound and listening at "startup:
// socket-listen done" (server.go), which is reached BEFORE the engine plane is
// assembled, before Run logs msg=ready, and before `go acceptLoop(...)` runs.
// A dial therefore succeeds against a daemon that has not finished starting:
// the connection sits in the kernel backlog, unaccepted and unserved.
//
// Dialability is a proxy, not the condition these tests depend on. Each test
// must wait for the thing it actually needs to have happened:
//
//	"the daemon finished starting" -> Run's msg=ready record
//	"my RPC reached its handler"   -> a signal raised by the handler itself
//
// PR #7226 established this happens-after pattern for a subprocess daemon
// (poll the captured log file for msg=ready). The daemons here run in-process,
// so the same anchor is taken from an injected slog.Handler instead.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// daemonReadySignal is Run's msg=ready record (server.go). It is the first
// point at which the daemon is fully assembled, and is logged immediately
// before the accept loop is launched.
const daemonReadySignal = "ready"

// logSignal records whether one slog message has been emitted, and lets a
// test block until it is.
type logSignal struct {
	msg  string
	seen atomic.Bool
	once sync.Once
	ch   chan struct{}
}

// Fired reports whether the message has been logged. It is the form used for
// an "X must already have happened by now" assertion.
func (s *logSignal) Fired() bool { return s.seen.Load() }

func (s *logSignal) fire() {
	s.seen.Store(true)
	s.once.Do(func() { close(s.ch) })
}

// Wait blocks until the message is logged, failing the test on timeout.
func (s *logSignal) Wait(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.ch:
	case <-time.After(timeout):
		t.Fatalf("daemon never logged %q within %s", s.msg, timeout)
	}
}

// signalHandler forwards every record to a discarding handler and raises any
// logSignal whose message matches. WithAttrs/WithGroup must carry the signals
// through, or a record emitted by a derived logger would be missed.
type signalHandler struct {
	slog.Handler
	sigs []*logSignal
}

func (h *signalHandler) Handle(ctx context.Context, rec slog.Record) error {
	for _, s := range h.sigs {
		if rec.Message == s.msg {
			s.fire()
		}
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *signalHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &signalHandler{Handler: h.Handler.WithAttrs(attrs), sigs: h.sigs}
}

func (h *signalHandler) WithGroup(name string) slog.Handler {
	return &signalHandler{Handler: h.Handler.WithGroup(name), sigs: h.sigs}
}

// newSignalLogger returns a logger that discards its output plus one
// *logSignal per message, in the order given.
func newSignalLogger(msgs ...string) (*slog.Logger, []*logSignal) {
	sigs := make([]*logSignal, len(msgs))
	for i, m := range msgs {
		sigs[i] = &logSignal{msg: m, ch: make(chan struct{})}
	}
	h := &signalHandler{
		Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}),
		sigs:    sigs,
	}
	return slog.New(h), sigs
}

// readyGapForTest is how long installReadyGap holds Run between binding the
// socket and handing the listener back. It must be far longer than the time a
// dial-based wait takes to return (~20ms, waitDaemonReady's poll interval) so
// that the difference between a correct and an incorrect wait is a decision,
// not a coin flip.
const readyGapForTest = 1 * time.Second

// installReadyGap makes the production socket-bound-but-not-ready window
// deterministic for the duration of one test: listenFn binds the real socket,
// optionally wraps it, and only then stalls for readyGapForTest before
// returning to Run. Throughout that window the socket is live and dialable
// while Run is still in its startup sequence — exactly the shape that, on a
// real daemon, is the engine-plane assembly between "startup: socket-listen
// done" and msg=ready.
//
// This is what GRADES the anchors in this package. With the gap installed, a
// test that waits on dialability alone provably acts on a daemon that has not
// started, and the post-cancel assertions in the shutdown tests turn red.
// Without it, the correct wait and the pre-#7228 proxy are indistinguishable
// on a fast machine, which is how this class of defect stayed invisible.
func installReadyGap(t *testing.T, wrap func(net.Listener) net.Listener) {
	t.Helper()
	restore := daemon.SetListenFuncForTest(func(addr string) (net.Listener, error) {
		l, err := transport.Listen(addr)
		if err != nil {
			return nil, err
		}
		if wrap != nil {
			l = wrap(l)
		}
		time.Sleep(readyGapForTest)
		return l, nil
	})
	t.Cleanup(restore)
}

// TestWaitDaemonReady_ReturnsBeforeTheDaemonIsReady is the #7228 finding
// itself, and the grader of installReadyGap: it pins that a successful dial
// does NOT imply the daemon finished starting, so the helper above cannot be
// weakened into a no-op (which would silently un-grade every anchor that
// depends on it) without this going red.
func TestWaitDaemonReady_ReturnsBeforeTheDaemonIsReady(t *testing.T) {
	isolateDaemonEnv(t)

	logger, sigs := newSignalLogger(daemonReadySignal)
	ready := sigs[0]

	installReadyGap(t, nil)

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	rb := func(proto.RebuildArgs) ([]string, string, error) { return nil, "", nil }

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- daemon.Run(ctx, daemon.Config{Layout: layout, Rebuild: rb, Logger: logger})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return within 10s of cleanup cancel")
		}
	})

	start := time.Now()
	waitDaemonReady(t, layout.SocketPath, 30*time.Second)
	dialTook := time.Since(start)
	readyAtDial := ready.Fired()

	if runtime.GOOS == "windows" {
		// The window does not exist on Windows, and that is asserted here
		// rather than skipped. The transport there is a named pipe, and
		// go-winio only creates a CONNECTABLE pipe instance inside Accept():
		// makeConnectedServerPipe -> connectPipe (go-winio pipe.go, v0.6.2).
		// The instance ListenPipe reserves (firstHandle) is never put into
		// ConnectNamedPipe. Run launches acceptLoop immediately AFTER logging
		// msg=ready, so on Windows a successful dial implies the accept loop is
		// already running, which implies readiness — a STRICTLY STRONGER
		// anchor than msg=ready, not a weaker one.
		//
		// Measured: CI run 35341675670 (job 105588757562) on windows-latest,
		// where the dial blocked for the whole of readyGapForTest and returned
		// only once the daemon was up. The elapsed check below is what keeps
		// that a measurement rather than an assumption.
		//
		// Consequence, stated so it is not mistaken for coverage: the two
		// msg=ready anchors are mutation-EQUIVALENT to the dial-based proxy on
		// Windows, so they are neither graded nor capable of being wrong there.
		// If that ever changes — a pre-accept connect path appears — the first
		// branch below fires and says exactly that.
		if !readyAtDial {
			t.Fatalf("waitDaemonReady returned after %s, before the daemon logged %q: a dial no "+
				"longer implies the accept loop is running on this platform, so installReadyGap "+
				"DOES open a socket-bound-but-not-ready window here — and the shutdown tests' "+
				"startup anchors are ungraded on Windows with nothing else covering them (#7228)",
				dialTook, daemonReadySignal)
		}
		if dialTook < readyGapForTest {
			t.Fatalf("the dial succeeded after %s, less than the %s installReadyGap holds Run "+
				"inside startup: the gap was not actually installed, so this test proves nothing "+
				"about the ordering it claims to pin", dialTook, readyGapForTest)
		}
		return
	}

	if readyAtDial {
		t.Fatalf("the daemon reached msg=%q before waitDaemonReady returned (dial took %s): the "+
			"socket-bound window installReadyGap exists to create did not happen, so nothing in "+
			"this package's shutdown tests is grading its startup anchor", daemonReadySignal, dialTook)
	}

	// The gap is a delay, not a deadlock: readiness must still arrive.
	ready.Wait(t, 30*time.Second)
}
