package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/client"
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
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// ReposToWatch blocks for 5s, simulating a stalled filesystem walk
	// during watcher subscription. released is closed when it returns so
	// the test can confirm the goroutine actually ran (and is off the
	// critical path).
	const stall = 5 * time.Second
	released := make(chan struct{})
	cfg := daemon.Config{
		Layout: layout,
		ReposToWatch: func() []string {
			defer close(released)
			time.Sleep(stall)
			return nil
		},
		GroupsForRepo:  func(_ string) []string { return nil },
		SchedulerIndex: func(_ context.Context, _ string, _ string) error { return nil },
		SchedulerLinks: func(_ context.Context, _ string) error { return nil },
		SchedulerAlgo:  func(_ context.Context, _ string) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, cfg) }()

	// The daemon must SERVE RPC promptly — far sooner than the 5s stall.
	// We assert a real Ping round-trip completes, not merely that the socket
	// file exists: the socket is created early (transport.Listen) but the
	// accept loop, dashboard goroutine, and "ready" log all sit AFTER the
	// watcher subscription on the boot path. A synchronous stall there means
	// the daemon binds the fd but never answers — exactly the live symptom
	// (0% CPU, :47274 never serving).
	//
	// Budget bumped from 2s → 4s (#1797 / #937 investigation): under
	// `go test -race` on shared CI runners the race detector adds ~2× overhead
	// to goroutine scheduling; 2s was routinely too tight and caused
	// sync.Cond-Wait to outlast the whole test-suite timeout (10 min).
	// The boot path itself is sub-100ms on any tier of hardware so 4s is
	// still a tight regression guard while being robust to CI noise.
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

	servedDeadline := time.Now().Add(4 * time.Second)
	served := false
	for time.Now().Before(servedDeadline) {
		if pingOnce(200 * time.Millisecond) {
			served = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !served {
		t.Fatalf("daemon did not answer an RPC within 4s while the watcher "+
			"subscription was stalled for %s — watcher setup is blocking the "+
			"boot critical path (#1456 regression)", stall)
	}

	// Sanity: the stalled subscription really was running concurrently and
	// eventually returns (it is not silently skipped).
	select {
	case <-released:
	case <-time.After(stall + 2*time.Second):
		t.Fatalf("ReposToWatch never released — watcher goroutine not started")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Log("daemon did not exit within 3s of cancel")
	}
}
