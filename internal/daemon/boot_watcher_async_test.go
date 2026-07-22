package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
)

// TestBoot_WatcherSubscriptionDoesNotBlockBind is the regression guard
// for #1456: a stalled watcher subscription (ReposToWatch returning slowly,
// or a per-repo AddRepo walk hanging on a slow mount / FD pressure / kqueue
// contention) must NOT prevent the daemon from binding its RPC socket and
// becoming ready.
//
// Before the fix the watcher subscription ran synchronously on Run()'s
// critical path, between the scheduler start and the RPC accept loop, so any
// stall there left the daemon at 0% CPU never serving — exactly what the
// live :47274 daemon exhibited on shipfast group-load (10/328 boots stalled
// in the watcher phase per the daemon.log forensics).
//
// We simulate the stall by making ReposToWatch block for far longer than the
// readiness deadline. With the async fix the socket binds well within the
// deadline; with the old synchronous code Dial would never succeed until the
// stall cleared.
func TestBoot_WatcherSubscriptionDoesNotBlockBind(t *testing.T) {
	isolateDaemonEnv(t)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// ReposToWatch blocks INDEFINITELY (until the test closes release),
	// simulating a stalled filesystem walk during watcher subscription. Using an
	// unblock-on-demand channel instead of a fixed time.Sleep removes the old
	// 4s-serve-vs-5s-stall wall-clock race that flaked under `-race`: because the
	// stall never clears on its own, the ONLY way the daemon can answer an RPC
	// within the (now generous) deadline below is if watcher subscription is off
	// the boot critical path. A synchronous regression would block here forever
	// and never serve — caught deterministically, with no timing tuning. released
	// is closed when the goroutine returns so the test can confirm it ran.
	release := make(chan struct{})
	released := make(chan struct{})
	cfg := daemon.Config{
		Layout: layout,
		ReposToWatch: func() []string {
			defer close(released)
			<-release // block until the test releases us (simulated stall)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		SchedulerIndex:     func(_ context.Context, _ string, _ string) error { return nil },
		SchedulerLinks:     func(_ context.Context, _ string) error { return nil },
		SchedulerGroupAlgo: func(_ context.Context, _ string) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, cfg) }()

	// The daemon must SERVE RPC while the watcher subscription is still blocked.
	// We assert a real Ping round-trip completes, not merely that the socket
	// file exists: the socket is created early (transport.Listen) but the
	// accept loop, dashboard goroutine, and "ready" log all sit AFTER the
	// watcher subscription on the boot path. A synchronous stall there means
	// the daemon binds the fd but never answers — exactly the live symptom
	// (0% CPU, :47274 never serving).
	//
	// Because the stall is now an indefinite block (not a 5s sleep), the serve
	// deadline no longer races a fixed stall duration and can be generous: it
	// only needs to exceed worst-case boot time on a contended `-race` runner.
	// The boot path itself is sub-100ms on any tier of hardware, so 10s is a
	// robust regression guard — a synchronous stall would miss it by an infinity,
	// not by a slim margin.
	//
	// pingOnce dials and Pings with a hard per-attempt timeout. net/rpc's
	// Call blocks until the server's accept loop reads the request, so a
	// blocked daemon would otherwise hang this attempt indefinitely and
	// silently outlast the deadline. The timeout converts that into a
	// failed attempt we can retry / time out cleanly.
	pingOnce := func(timeout time.Duration) bool {
		c, derr := client.Dial()
		if derr != nil {
			return false
		}
		defer c.Close()
		res := make(chan error, 1)
		go func() { _, perr := c.Ping(); res <- perr }()
		select {
		case err := <-res:
			return err == nil
		case <-time.After(timeout):
			return false
		}
	}

	servedDeadline := time.Now().Add(10 * time.Second)
	served := false
	for time.Now().Before(servedDeadline) {
		if pingOnce(200 * time.Millisecond) {
			served = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !served {
		t.Fatal("daemon did not answer an RPC within 10s while the watcher " +
			"subscription was blocked — watcher setup is blocking the boot " +
			"critical path (#1456 regression)")
	}

	// The daemon served while ReposToWatch was still blocked, proving the
	// subscription is off the critical path. Now release the simulated stall and
	// confirm the goroutine really ran (it is not silently skipped) and unwinds.
	close(release)
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("ReposToWatch never released — watcher goroutine not started")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Log("daemon did not exit within 3s of cancel")
	}
}
