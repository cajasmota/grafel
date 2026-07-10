package daemon_test

// shutdown_watchdog_test.go — regression test for issue #5710: a stalled
// Service.Rebuild RPC (rebuildRPCTimeout = 2h, no shutdown/ctx.Done case)
// holds its connection open indefinitely, so connWG.Wait() in Run()'s
// graceful-shutdown tail can block forever, wedging the pidfile. This test
// verifies the hard-exit watchdog: once shutdown is triggered while a
// Rebuild is in flight, Run() must return within the (test-shortened)
// watchdog timeout rather than hang.
//
// This test cannot substitute a fake RebuildFunc through a real subprocess
// (cmd/grafel wires the production RebuildFunc, not an injectable stub), so
// it drives daemon.Run in-process via the same harness as daemon_test.go
// (runDaemonForTest / isolateDaemonEnv / waitDaemonReady, all defined in
// that file within this same package). The watchdog's real behavior calls
// os.Exit(1), which would kill the whole `go test` process if exercised
// as-is; daemon.SetShutdownExitFuncForTest (server.go) is the exported hook
// that lets an external test swap in a no-op and observe that Run() still
// returns via its fallback path — proving the watchdog unblocked Run()
// instead of hanging on connWG.Wait() forever.

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestDaemon_ShutdownWatchdogForceExitsOnStalledRebuild is the #5710
// RED/GREEN test. Before the watchdog existed, this test would hang until
// its own outer timeout fired (a false "pass" only in the sense that `go
// test` eventually kills it — in practice it demonstrated the same
// unbounded hang the daemon suffers in production). With the watchdog, Run()
// returns quickly once osExit is stubbed to survive the force-exit call.
func TestDaemon_ShutdownWatchdogForceExitsOnStalledRebuild(t *testing.T) {
	isolateDaemonEnv(t)

	// #5710: server.go's shutdownWatchdogTimeout() honors this env var
	// (shutdownWatchdogEnv) so the test doesn't wait out the real 5s default.
	const testWatchdog = 300 * time.Millisecond
	t.Setenv("GRAFEL_SHUTDOWN_WATCHDOG", testWatchdog.String())

	var exitCalled atomic.Bool
	var exitCode atomic.Int64
	restore := daemon.SetShutdownExitFuncForTest(func(code int) {
		exitCalled.Store(true)
		exitCode.Store(int64(code))
		// Deliberately do not exit — see server.go's watchdog branch: the
		// `return` immediately after its osExit(1) call is reachable only
		// when osExit does not actually terminate the process, which is
		// exactly what lets this test observe Run() returning.
	})
	t.Cleanup(restore)

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	// RebuildFunc that blocks forever — simulates the stalled-rebuild
	// deadlock from #5710. The channel is never closed; the handler
	// goroutine (and the client goroutine driving it) are deliberately
	// leaked for the test process's lifetime, matching the real scenario
	// where a stalled rebuild is abandoned rather than cancelled.
	blockForever := make(chan struct{})
	rb := func(args proto.RebuildArgs) ([]string, string, error) {
		<-blockForever
		return nil, "", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- daemon.Run(ctx, daemon.Config{Layout: layout, Rebuild: rb})
	}()

	waitDaemonReady(t, layout.SocketPath, 10*time.Second)

	c, err := client.DialPath(layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	rebuildStarted := make(chan struct{})
	go func() {
		close(rebuildStarted)
		_, _ = c.Rebuild(proto.RebuildArgs{Group: "stall-group"})
	}()
	<-rebuildStarted
	// Best-effort: give the RPC a moment to actually land server-side before
	// triggering shutdown, so the watchdog genuinely races a stalled call in
	// flight rather than one that hasn't arrived yet. The deadline assertion
	// below is generous enough to tolerate scheduling slack either way.
	time.Sleep(150 * time.Millisecond)

	start := time.Now()
	cancel() // trigger shutdown while the Rebuild call is stuck

	select {
	case runErr := <-runDone:
		elapsed := time.Since(start)
		t.Logf("Run returned after %s (watchdog=%s), err=%v, osExit called=%v code=%v",
			elapsed, testWatchdog, runErr, exitCalled.Load(), exitCode.Load())
		// Generous ceiling: an order of magnitude above the 300ms watchdog,
		// but two+ orders of magnitude below the old unbounded (2h
		// rebuildRPCTimeout-scale) hang this guards against.
		if elapsed > 5*time.Second {
			t.Fatalf("Run took %s to return; want well under 5s (watchdog=%s)", elapsed, testWatchdog)
		}
		if !exitCalled.Load() {
			t.Fatal("expected the #5710 watchdog to invoke the exit func; it did not fire")
		}
		if exitCode.Load() != 1 {
			t.Fatalf("exit func called with code %d, want 1", exitCode.Load())
		}
		if runErr == nil {
			t.Fatal("expected Run to return a non-nil error on the force-exit path")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not return within 8s of shutdown trigger — #5710 regression: watchdog did not unblock connWG.Wait()")
	}

	// The pidfile must not be left behind by the force-exit path: server.go
	// explicitly removes it before calling osExit, since a real os.Exit
	// would skip the deferred releasePID() entirely.
	if _, statErr := os.Stat(layout.PIDPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected pidfile %s removed by force-exit cleanup, stat err = %v", layout.PIDPath, statErr)
	}
}
