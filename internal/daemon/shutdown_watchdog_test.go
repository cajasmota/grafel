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
//
// #7233 — WHAT MAKES THE FORCE-EXIT ATTRIBUTABLE TO THE STALL.
//
// connWG counts accepted connections, not in-flight RPCs (acceptLoop in
// server.go does the wg.Add per conn). So an open-but-IDLE client connection
// blocks connWG.Wait() exactly as effectively as a stalled handler does, and
// the watchdog fires either way. `exitCalled` on its own therefore says
// "something was still attached", never "a Rebuild was stuck" — measured on
// 5cf7e19c2: with the stall removed (`<-blockForever` deleted) but the RPC
// still issued and the connection still open, this test stayed GREEN.
//
// Note which mutant that is. #7233 was filed against the shape BEFORE
// a0e0a9246, where simply deleting `go c.Rebuild(...)` left the test green;
// on 5cf7e19c2 that deletion is already caught, but by the handler-arrival
// select below rather than by anything the daemon does. Deleting the RPC and
// deleting the STALL are different mutants, and only the second one reached
// the shutdown assertions.
//
// The fix is to remove the connection itself as a competing explanation:
// the client closes its end BEFORE shutdown is triggered. net/rpc's
// ServeCodec loop then reads EOF, stops reading, and blocks in its internal
// wg.Wait() for the still-running handler — so after the close the ONLY
// thing holding the conn goroutine (and hence connWG) is the stalled
// Rebuild. Removing the stall now drains connWG and shutdown completes
// gracefully, which fails the assertions below.
//
// The two shapes that argument leans on are committed as tests rather than
// asserted in prose, since either could stop being true without a word here
// changing:
//
//   - TestDaemon_ShutdownWatchdogAlsoFiresForAnOpenIdleConnection pins the
//     confound (an idle conn fires the watchdog with no RPC in flight at
//     all), so nobody re-derives `exitCalled` as evidence of a stall;
//   - TestDaemon_ShutdownWatchdogDoesNotFireOnceTheConnectionIsClosed pins
//     the other half (a closed conn does NOT fire it), which is what makes
//     the close below load-bearing rather than cosmetic.
//
// #7233 — WHERE THESE ARE GRADED. Every mutation score recorded for this
// file (#7228/#7232's handler-arrival anchor and #7233's attribution rows
// alike) was taken on Unix. Nothing here is Windows-specific, but nothing
// here has been measured on Windows either: a green Windows leg is evidence
// the tests compile and pass there, NOT evidence that the anchor or the
// close still carry their weight there. Do not read it as such.

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

// testWatchdog is the (env-shortened) shutdown watchdog bound used by the two
// tests that assert the force-exit path FIRES. server.go's
// shutdownWatchdogTimeout() honors GRAFEL_SHUTDOWN_WATCHDOG
// (shutdownWatchdogEnv) so they don't wait out the real 5s default.
const testWatchdog = 300 * time.Millisecond

// gracefulWatchdog is the bound used by the one test that asserts the
// force-exit path does NOT fire. It is deliberately an order of magnitude
// larger than testWatchdog: an absence assertion under a 300ms bound would go
// red on a slow leg (the measured graceful tail is ~25ms on Unix, but
// listener.Close() on a Windows named pipe is the #6044 hazard and has no such
// budget), and a flaky control is worse than none. Nothing the test claims
// depends on the number — "a closed connection does not hold connWG" is a
// statement about whether the tail terminates, not about how fast — and the
// positive control for it is only harder to satisfy at a longer bound: a
// genuinely stalled handler never finishes, so it force-exits at any bound.
const gracefulWatchdog = 3 * time.Second

// watchdogFixture is one in-process daemon started under the shortened
// watchdog with the force-exit call stubbed out. It carries only what the
// tests assert on; each test keeps its own connection handling explicit,
// because the ORDER of dial / RPC / close / cancel is the thing under test.
type watchdogFixture struct {
	layout     daemon.Layout
	cancel     context.CancelFunc
	runDone    chan error
	exitCalled *atomic.Bool
	exitCode   *atomic.Int64
}

// startWatchdogDaemon boots daemon.Run in-process with rb as its RebuildFunc
// and waits until the socket is dialable. It installs installReadyGap so a
// wait that only proves dialability provably acts too early (#7228) — that is
// what keeps the handler-arrival anchor in the stalled test gradeable rather
// than indistinguishable from the sleep it replaced.
func startWatchdogDaemon(t *testing.T, watchdog time.Duration, rb daemon.RebuildFunc) *watchdogFixture {
	t.Helper()
	isolateDaemonEnv(t)
	t.Setenv("GRAFEL_SHUTDOWN_WATCHDOG", watchdog.String())

	f := &watchdogFixture{
		exitCalled: new(atomic.Bool),
		exitCode:   new(atomic.Int64),
	}
	restore := daemon.SetShutdownExitFuncForTest(func(code int) {
		f.exitCalled.Store(true)
		f.exitCode.Store(int64(code))
		// Deliberately do not exit — see server.go's watchdog branch: the
		// `return` immediately after its osExit(1) call is reachable only
		// when osExit does not actually terminate the process, which is
		// exactly what lets these tests observe Run() returning.
	})
	t.Cleanup(restore)

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	f.layout = layout

	installReadyGap(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	t.Cleanup(cancel)
	f.runDone = make(chan error, 1)
	go func() {
		f.runDone <- daemon.Run(ctx, daemon.Config{Layout: layout, Rebuild: rb})
	}()

	waitDaemonReady(t, layout.SocketPath, 10*time.Second)
	return f
}

// TestDaemon_ShutdownWatchdogForceExitsOnStalledRebuild is the #5710
// RED/GREEN test. Before the watchdog existed, this test would hang until
// its own outer timeout fired (a false "pass" only in the sense that `go
// test` eventually kills it — in practice it demonstrated the same
// unbounded hang the daemon suffers in production). With the watchdog, Run()
// returns quickly once osExit is stubbed to survive the force-exit call.
func TestDaemon_ShutdownWatchdogForceExitsOnStalledRebuild(t *testing.T) {
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

	f := startWatchdogDaemon(t, testWatchdog, rb)

	c, err := client.DialPath(f.layout.SocketPath)
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

	// #7233: hand back the client end BEFORE triggering shutdown. The handler
	// is provably inside <-blockForever by now (rebuildEntered is closed), so
	// closing here cannot race it away: net/rpc's ServeCodec reads EOF, leaves
	// its read loop, and parks in wg.Wait() on this one still-running call.
	// From this line on, the stalled Rebuild is the ONLY thing keeping the
	// connection goroutine — and therefore connWG — alive. That is what turns
	// the force-exit asserted below from "a connection was open" into "the
	// stalled handler is why shutdown had to be bounded": delete the stall and
	// connWG drains, shutdown completes gracefully, and these assertions fail.
	// See the header, and the two control tests it names, for why each half of
	// that claim is pinned by a test rather than by this comment.
	if err := c.Close(); err != nil {
		t.Fatalf("close client before shutdown: %v", err)
	}

	start := time.Now()
	f.cancel() // trigger shutdown while the Rebuild call is stuck
	// The check that keeps the #7228 wait above honest: reverting it to the
	// sleep (or to any dialability-based proxy) makes this fail under
	// installReadyGap. Note what it does NOT do — it observes the test's own
	// wait, not the daemon: it sits after a select that already t.Fatals on
	// timeout, so it can only fire under a mutation of that select. It is a
	// tripwire on this file, not an assertion about shutdown behaviour.
	if !rebuildRunning.Load() {
		t.Fatal("shutdown was triggered before Service.Rebuild was in flight (#7228)")
	}

	select {
	case runErr := <-f.runDone:
		elapsed := time.Since(start)
		t.Logf("Run returned after %s (watchdog=%s), err=%v, osExit called=%v code=%v",
			elapsed, testWatchdog, runErr, f.exitCalled.Load(), f.exitCode.Load())
		// Generous ceiling: an order of magnitude above the 300ms watchdog,
		// but two+ orders of magnitude below the old unbounded (2h
		// rebuildRPCTimeout-scale) hang this guards against.
		if elapsed > 5*time.Second {
			t.Fatalf("Run took %s to return; want well under 5s (watchdog=%s)", elapsed, testWatchdog)
		}
		if !f.exitCalled.Load() {
			t.Fatal("expected the #5710 watchdog to invoke the exit func; it did not fire — with the " +
				"client connection already closed, a graceful shutdown here means nothing was actually " +
				"stalled inside Service.Rebuild (#7233)")
		}
		if f.exitCode.Load() != 1 {
			t.Fatalf("exit func called with code %d, want 1", f.exitCode.Load())
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
	if _, statErr := os.Stat(f.layout.PIDPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected pidfile %s removed by force-exit cleanup, stat err = %v", f.layout.PIDPath, statErr)
	}
}

// TestDaemon_ShutdownWatchdogAlsoFiresForAnOpenIdleConnection commits the
// probe that exposed #7233. It is a NEGATIVE control for the test above, and
// the polarity is the opposite of what is intuitive: an open connection with
// no RPC in flight at all ALSO trips the force-exit path, because connWG
// counts connections rather than calls. So `exitCalled == true` is not, on its
// own, evidence that anything was stalled.
//
// Keeping this in the tree is the point. The observation was made ad hoc while
// fixing #7228 and committed nowhere, which is precisely why the stalled test
// could go on claiming causation it did not establish. If a future change ever
// makes an idle connection drain on its own, this goes red and the stalled
// test's close-before-cancel step stops being load-bearing — the two must be
// re-read together.
func TestDaemon_ShutdownWatchdogAlsoFiresForAnOpenIdleConnection(t *testing.T) {
	// Never invoked: this test issues no Rebuild. A t.Error here rather than a
	// silent no-op, so "no RPC in flight" is asserted instead of assumed.
	rb := func(args proto.RebuildArgs) ([]string, string, error) {
		t.Errorf("Service.Rebuild ran in the idle-connection control (group=%q); this test must keep "+
			"its connection idle or it is not a control at all (#7233)", args.Group)
		return nil, "", nil
	}

	f := startWatchdogDaemon(t, testWatchdog, rb)

	c, err := client.DialPath(f.layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	// A completed round-trip is the anchor: dialability alone does not prove
	// acceptLoop took the connection and did its wg.Add, and without that the
	// conn would not be in connWG when shutdown starts — this test would then
	// pass or fail on scheduling. A returned Status reply proves the server
	// accepted, read and answered on this exact connection.
	if _, err := c.Status(); err != nil {
		t.Fatalf("status round-trip (the proof this conn is accepted and in connWG): %v", err)
	}

	start := time.Now()
	f.cancel()

	select {
	case runErr := <-f.runDone:
		t.Logf("Run returned after %s (watchdog=%s), err=%v, osExit called=%v code=%v",
			time.Since(start), testWatchdog, runErr, f.exitCalled.Load(), f.exitCode.Load())
		if !f.exitCalled.Load() {
			t.Fatal("an open idle connection did NOT trip the force-exit path — if that is now the " +
				"daemon's behaviour it is a change for the better, but the stalled-Rebuild test above " +
				"documents the opposite; re-read both (#7233)")
		}
		if runErr == nil {
			t.Fatal("expected Run to return a non-nil error on the force-exit path")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not return within 8s of shutdown trigger")
	}
}

// TestDaemon_ShutdownWatchdogDoesNotFireOnceTheConnectionIsClosed is the other
// half of #7233's attribution argument: a connection the client has closed does
// NOT hold connWG, so with nothing stalled the graceful tail completes and the
// watchdog never fires.
//
// This is what makes the close-before-cancel step in the stalled test mean
// something. Without it, "we closed the client" would be an untested claim
// about net/rpc, and the stalled test would be resting on the same
// any-connection-will-do coincidence #7233 was filed about.
//
// It is an ABSENCE assertion (exitCalled must stay false), so it needs a
// positive control: planting a stalled Rebuild before the close makes it fire.
// That control is a mutation row, not a second test — see the PR for #7233.
func TestDaemon_ShutdownWatchdogDoesNotFireOnceTheConnectionIsClosed(t *testing.T) {
	rb := func(args proto.RebuildArgs) ([]string, string, error) {
		return nil, "", nil
	}

	f := startWatchdogDaemon(t, gracefulWatchdog, rb)

	c, err := client.DialPath(f.layout.SocketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Same anchor as the idle control: prove the connection really was
	// accepted and served, so "closing it released connWG" is a statement
	// about a connection that was in connWG in the first place.
	if _, err := c.Status(); err != nil {
		t.Fatalf("status round-trip (the proof this conn is accepted and in connWG): %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	start := time.Now()
	f.cancel()

	select {
	case runErr := <-f.runDone:
		elapsed := time.Since(start)
		t.Logf("Run returned after %s (watchdog=%s), err=%v, osExit called=%v",
			elapsed, gracefulWatchdog, runErr, f.exitCalled.Load())
		if f.exitCalled.Load() {
			t.Fatalf("the watchdog force-exited a shutdown with nothing in flight and the only client "+
				"connection already closed (after %s, watchdog=%s). Either connWG no longer drains on "+
				"client close — which would silently un-ground the stalled-Rebuild test's attribution "+
				"— or the graceful tail has acquired a new way to block (#7233)", elapsed, gracefulWatchdog)
		}
		if runErr != nil {
			t.Fatalf("expected a clean graceful shutdown, got err=%v", runErr)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not return within 8s of shutdown trigger")
	}

	// The graceful path removes the pidfile through its deferred
	// releasePID(), not through the force-exit branch's explicit cleanup —
	// asserting it here keeps the two exits comparable on the same axis.
	if _, statErr := os.Stat(f.layout.PIDPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected pidfile %s removed by graceful shutdown, stat err = %v", f.layout.PIDPath, statErr)
	}
}
