package daemon_test

// shutdown_accept_bound_test.go pins the SECOND of the two Windows shutdown
// hazards in Run()'s graceful-shutdown tail.
//
// Bounding listener.Close() alone (transport.closeBounded) is not enough. When
// that close is NOT confirmed the listener is by definition still live, so
// go-winio's listenerRoutine can still take the accept loop's next handoff and
// park waiting for a client that never arrives. Accept never returns,
// acceptLoop never closes acceptDone, and Run's `<-acceptDone` — which used to
// be a bare channel receive sitting BEFORE the watchdog select — wedges
// forever. The hang simply moved down one line.
//
// A real net.UnixListener cannot exhibit this: its Close always unblocks
// Accept. So the shape is injected through daemon.SetListenFuncForTest, using
// a listener that delegates Accept to a real one but whose Close deliberately
// does not release it — precisely what an unconfirmed close looks like.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/transport"
)

// unconfirmedCloseListener wraps a real listener. Accept and Addr delegate, so
// the daemon starts and serves normally. Close reports failure WITHOUT closing
// the underlying listener, leaving Accept blocked exactly as a wedged
// go-winio listenerRoutine would.
type unconfirmedCloseListener struct {
	net.Listener
	closes atomic.Int32
	// real is kept separately so the test can genuinely release it at cleanup.
	real net.Listener
}

var errCloseNotConfirmedStub = errors.New("listener close not confirmed within budget")

func (l *unconfirmedCloseListener) Close() error {
	l.closes.Add(1)
	return errCloseNotConfirmedStub
}

// acceptWatchdogSignal is the message Run logs from the ONLY code path that
// makes the `<-acceptDone` step finite: the watchdog case of the select that
// used to be a bare channel receive. It is the structural signal that hazard 2
// is bounded — see signalHandler below for why the test asserts on it rather
// than on elapsed wall-clock time or on which terminal path Run happened to
// take afterwards.
const acceptWatchdogSignal = "graceful shutdown: accept loop did not stop before the watchdog expired"

// signalHandler is a slog.Handler that records whether a given message was
// logged. It replaces the previous `exitCalled` assertion, which was a
// scheduler race rather than a property of the code under test (#6373):
//
// Once the accept watchdog fires, watchdogCtx is already expired, so BOTH cases
// of Run's final select — <-connDone and <-watchdogCtx.Done() — are ready as
// soon as connWG.Wait() returns (this test holds no connections open, so that
// is immediate). Go picks a ready case uniformly at random, so whether Run
// force-exits or returns a clean nil is a coin flip decided by how fast the
// runner schedules the connDone goroutine. Both outcomes are correct: shutdown
// was BOUNDED either way, which is the property this test exists to pin. CI run
// 32388710411 lost that flip and failed a working daemon.
//
// The accept watchdog itself is not a race: the fixture's listener can never
// release Accept, so acceptDone can never close and the watchdog case is the
// only reachable one. Asserting on its signal is deterministic under any load.
type signalHandler struct {
	slog.Handler
	want string
	seen *atomic.Bool
}

func (h *signalHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Message == h.want {
		h.seen.Store(true)
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *signalHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &signalHandler{Handler: h.Handler.WithAttrs(attrs), want: h.want, seen: h.seen}
}

func (h *signalHandler) WithGroup(name string) slog.Handler {
	return &signalHandler{Handler: h.Handler.WithGroup(name), want: h.want, seen: h.seen}
}

// newSignalLogger returns a logger that discards its output and an *atomic.Bool
// set the moment want is logged.
func newSignalLogger(want string) (*slog.Logger, *atomic.Bool) {
	seen := &atomic.Bool{}
	h := &signalHandler{
		Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}),
		want:    want,
		seen:    seen,
	}
	return slog.New(h), seen
}

// TestDaemon_ShutdownBoundedWhenListenerCloseIsNotConfirmed is the RED/GREEN
// test for hazard 2. With the bare `<-acceptDone` receive, Run never returns
// and this test fails on its 8s ceiling. With the watchdog case, Run reaches
// the force-exit path and returns.
func TestDaemon_ShutdownBoundedWhenListenerCloseIsNotConfirmed(t *testing.T) {
	isolateDaemonEnv(t)

	const testWatchdog = 300 * time.Millisecond
	t.Setenv("GRAFEL_SHUTDOWN_WATCHDOG", testWatchdog.String())

	// osExit must still be stubbed: Run force-exits on one of its two legal
	// terminal paths, and an unstubbed os.Exit would kill the test binary.
	restoreExit := daemon.SetShutdownExitFuncForTest(func(int) {})
	t.Cleanup(restoreExit)

	logger, acceptWatchdogFired := newSignalLogger(acceptWatchdogSignal)

	var stub *unconfirmedCloseListener
	restoreListen := daemon.SetListenFuncForTest(func(addr string) (net.Listener, error) {
		real, err := transport.Listen(addr)
		if err != nil {
			return nil, err
		}
		stub = &unconfirmedCloseListener{Listener: real, real: real}
		// The daemon can never release this listener, so the test must.
		t.Cleanup(func() { _ = real.Close() })
		return stub, nil
	})
	t.Cleanup(restoreListen)

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

	waitDaemonReady(t, layout.SocketPath, 10*time.Second)

	start := time.Now()
	cancel()

	select {
	case <-runDone:
		// Elapsed time is logged for diagnostics only. It is NOT the
		// assertion: the 8s ceiling below is the generous non-hang guard
		// (package norm), and boundedness proper is asserted structurally.
		t.Logf("Run returned after %s (watchdog=%s), closes=%d", time.Since(start), testWatchdog, stub.closes.Load())
		if stub.closes.Load() == 0 {
			t.Fatal("Run never called listener.Close — the fixture never exercised the unconfirmed-close path")
		}
		if !acceptWatchdogFired.Load() {
			t.Fatal("Run returned without the accept-loop watchdog firing: the `<-acceptDone` step was " +
				"not bounded by the shutdown watchdog, so it is not what made this shutdown finite")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not return within 8s: the `<-acceptDone` receive is not bounded by the " +
			"shutdown watchdog, so an unconfirmed listener close still wedges shutdown forever")
	}
}

// TestDaemon_ShutdownStillCleanWhenListenerCloseSucceeds is the fixture-validity
// anchor: the SAME harness, with a listener whose Close behaves normally, must
// shut down WITHOUT the force-exit watchdog firing. Without this, the test
// above could pass on a daemon that force-exits on every shutdown, which would
// be a regression rather than a fix.
func TestDaemon_ShutdownStillCleanWhenListenerCloseSucceeds(t *testing.T) {
	isolateDaemonEnv(t)

	const testWatchdog = 300 * time.Millisecond
	t.Setenv("GRAFEL_SHUTDOWN_WATCHDOG", testWatchdog.String())

	var exitCalled atomic.Bool
	restoreExit := daemon.SetShutdownExitFuncForTest(func(int) {
		exitCalled.Store(true)
	})
	t.Cleanup(restoreExit)

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
		runDone <- daemon.Run(ctx, daemon.Config{Layout: layout, Rebuild: rb})
	}()

	waitDaemonReady(t, layout.SocketPath, 10*time.Second)
	cancel()

	select {
	case runErr := <-runDone:
		if runErr != nil {
			t.Fatalf("expected a clean graceful shutdown, got %v", runErr)
		}
		if exitCalled.Load() {
			t.Fatal("the force-exit watchdog fired on a healthy shutdown — the new watchdog " +
				"case on <-acceptDone must not short-circuit the normal path")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not return within 8s on a healthy shutdown")
	}
}
