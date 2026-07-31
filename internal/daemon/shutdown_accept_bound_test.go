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

// TestDaemon_ShutdownBoundedWhenListenerCloseIsNotConfirmed is the RED/GREEN
// test for hazard 2. With the bare `<-acceptDone` receive, Run never returns
// and this test fails on its 8s ceiling. With the watchdog case, Run reaches
// the force-exit path and returns.
func TestDaemon_ShutdownBoundedWhenListenerCloseIsNotConfirmed(t *testing.T) {
	isolateDaemonEnv(t)

	const testWatchdog = 300 * time.Millisecond
	t.Setenv("GRAFEL_SHUTDOWN_WATCHDOG", testWatchdog.String())

	var exitCalled atomic.Bool
	restoreExit := daemon.SetShutdownExitFuncForTest(func(int) {
		exitCalled.Store(true)
	})
	t.Cleanup(restoreExit)

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
		runDone <- daemon.Run(ctx, daemon.Config{Layout: layout, Rebuild: rb})
	}()

	waitDaemonReady(t, layout.SocketPath, 10*time.Second)

	start := time.Now()
	cancel()

	select {
	case <-runDone:
		elapsed := time.Since(start)
		t.Logf("Run returned after %s (watchdog=%s), closes=%d", elapsed, testWatchdog, stub.closes.Load())
		if elapsed > 5*time.Second {
			t.Fatalf("Run took %s to return; want well under 5s (watchdog=%s)", elapsed, testWatchdog)
		}
		if stub.closes.Load() == 0 {
			t.Fatal("Run never called listener.Close — the fixture never exercised the unconfirmed-close path")
		}
		if !exitCalled.Load() {
			t.Fatal("expected the watchdog force-exit path to fire; Run returned some other way")
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
