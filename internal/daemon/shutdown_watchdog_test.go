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
	"sync"
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
	//
	// #7228: rebuildEntered is the anchor this test actually depends on. A
	// successful dial does not mean the RPC reached the server — it does not
	// even mean the daemon finished starting — and the handler itself is the
	// only place the arrival of Service.Rebuild is observable.
	blockForever := make(chan struct{})
	rebuildEntered := make(chan struct{})
	var rebuildRunning atomic.Bool
	var enteredOnce sync.Once
	rb := func(args proto.RebuildArgs) ([]string, string, error) {
		rebuildRunning.Store(true)
		enteredOnce.Do(func() { close(rebuildEntered) })
		<-blockForever
		return nil, "", nil
	}

	// #7228: hold Run inside startup with the socket already bound, so a wait
	// that only proves dialability provably acts too early. This is what makes
	// the anchor below gradeable rather than indistinguishable from the sleep
	// it replaces.
	installReadyGap(t, nil)

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
	go func() {
		_, _ = c.Rebuild(proto.RebuildArgs{Group: "stall-group"})
	}()
	// #7228: wait for the RPC to reach its handler. This replaces a bare 150ms
	// sleep, which was not merely imprecise but ungraded: measured on the
	// pre-#7228 test, deleting the c.Rebuild call entirely left it GREEN. The
	// watchdog fired anyway because the dialled-but-idle connection is enough
	// to block connWG.Wait() — so the "stalled Rebuild" premise in this test's
	// name was never exercised at all.
	select {
	case <-rebuildEntered:
	case <-time.After(30 * time.Second):
		t.Fatal("Service.Rebuild never reached its handler: shutdown would be triggered against a " +
			"daemon with no stalled RPC in flight, which is not what this test claims to pin (#7228)")
	}

	start := time.Now()
	cancel() // trigger shutdown while the Rebuild call is stuck
	// The check that keeps the wait above honest: reverting it to the sleep (or
	// to any dialability-based proxy) makes this fail under installReadyGap.
	if !rebuildRunning.Load() {
		t.Fatal("shutdown was triggered before Service.Rebuild was in flight (#7228)")
	}

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
