package daemon

// White-box tests for the #3366 async Index path. These construct a Service +
// real Scheduler directly (no socket, no daemon.Run) so they do NOT trip the
// canonical-socket guard and never disturb a live daemon.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// newAsyncTestService wires a Service to a started Scheduler whose Index hook
// is `idxFn`. The synchronous Index entrypoint is `syncFn`.
func newAsyncTestService(t *testing.T, syncFn IndexFunc, idxFn sched.IndexFn) (*Service, func()) {
	t.Helper()
	sc := sched.New(sched.Config{
		Workers:   1,
		Index:     idxFn,
		Links:     func(context.Context, string) error { return nil },
		GroupAlgo: func(context.Context, string) error { return nil },
	})
	sc.Start()
	svc := newService(syncFn, nil, nil, "", make(chan struct{}, 1), nil, 1)
	svc.scheduler = sc
	return svc, func() { sc.Stop() }
}

// TestIndexAsync_EnqueuesAndReturnsWithoutBlocking verifies that an Index RPC
// with Async=true routes to the debounced scheduler and returns immediately,
// WITHOUT invoking the heavy synchronous Index entrypoint (which here blocks
// for 30s — if the async path waited on it the test would time out).
func TestIndexAsync_EnqueuesAndReturnsWithoutBlocking(t *testing.T) {
	var syncCalls atomic.Int32
	schedCh := make(chan string, 8)

	svc, stop := newAsyncTestService(t,
		func(proto.IndexArgs) (string, string, error) {
			syncCalls.Add(1)
			time.Sleep(30 * time.Second)
			return "", "", nil
		},
		func(_ context.Context, repo string, _ string) error {
			schedCh <- repo
			return nil
		},
	)
	defer stop()

	done := make(chan error, 1)
	go func() {
		var reply proto.IndexReply
		done <- svc.Index(&proto.IndexArgs{RepoPath: "/some/repo", Async: true}, &reply)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("async Index returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("async Index blocked >3s — it must ACK immediately, not wait for the reindex")
	}

	if got := syncCalls.Load(); got != 0 {
		t.Errorf("async path invoked the synchronous Index entrypoint %d times; want 0", got)
	}

	select {
	case got := <-schedCh:
		if got != "/some/repo" {
			t.Errorf("scheduler enqueued repo=%q, want /some/repo", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("async Index did not enqueue a scheduler reindex")
	}
}

// TestIndexAsync_FallsBackToSyncWhenNoScheduler verifies the async flag is a
// no-op when no scheduler is attached (a watcher-less daemon): the RPC runs the
// synchronous entrypoint so manual/rebuild indexing still works.
func TestIndexAsync_FallsBackToSyncWhenNoScheduler(t *testing.T) {
	var syncCalls atomic.Int32
	svc := newService(
		func(proto.IndexArgs) (string, string, error) {
			syncCalls.Add(1)
			return "/g/graph.fb", "", nil
		}, nil, nil, "", make(chan struct{}, 1), nil, 1)
	// svc.scheduler is nil.

	var reply proto.IndexReply
	if err := svc.Index(&proto.IndexArgs{RepoPath: "/some/repo", Async: true}, &reply); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if syncCalls.Load() != 1 {
		t.Errorf("no-scheduler async path should fall back to sync index; sync calls=%d", syncCalls.Load())
	}
}

// TestIndexSync_Unchanged verifies the DEFAULT (Async=false) path is unchanged:
// it runs the synchronous entrypoint and never touches the scheduler enqueue.
func TestIndexSync_Unchanged(t *testing.T) {
	var syncCalls atomic.Int32
	var schedCalls atomic.Int32

	svc, stop := newAsyncTestService(t,
		func(proto.IndexArgs) (string, string, error) {
			syncCalls.Add(1)
			return "/g/graph.fb", "", nil
		},
		func(context.Context, string, string) error {
			schedCalls.Add(1)
			return nil
		},
	)
	defer stop()

	var reply proto.IndexReply
	if err := svc.Index(&proto.IndexArgs{RepoPath: "/some/repo"}, &reply); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if syncCalls.Load() != 1 {
		t.Errorf("synchronous index entrypoint calls=%d, want 1", syncCalls.Load())
	}
	if reply.GraphPath != "/g/graph.fb" {
		t.Errorf("sync reply.GraphPath=%q, want /g/graph.fb", reply.GraphPath)
	}
	// MarkIndexed may touch the scheduler, but the reindex hook itself
	// (schedCalls) must NOT fire on the synchronous path.
	if schedCalls.Load() != 0 {
		t.Errorf("synchronous path must not enqueue a scheduler reindex; got %d", schedCalls.Load())
	}
}

// TestIndexAsync_BurstCoalesces verifies a burst of async enqueues for the same
// repo collapses to a single reindex (#3366) — the guarantee that stops a rebase
// (N commits) and concurrent worktrees from stampeding the daemon.
func TestIndexAsync_BurstCoalesces(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	release := make(chan struct{})

	svc, stop := newAsyncTestService(t,
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		func(context.Context, string, string) error {
			mu.Lock()
			calls++
			mu.Unlock()
			<-release // hold the first job in-flight so the burst coalesces
			return nil
		},
	)
	defer stop()

	for i := 0; i < 10; i++ {
		var reply proto.IndexReply
		if err := svc.Index(&proto.IndexArgs{RepoPath: "/some/repo", Async: true}, &reply); err != nil {
			t.Fatalf("async Index %d: %v", i, err)
		}
	}

	time.Sleep(300 * time.Millisecond)
	close(release)
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	got := calls
	mu.Unlock()
	// Per-repo dedup: while one run is in-flight, the other 9 enqueues are
	// coalesced (at most one queued re-run after it completes), never 10.
	if got == 0 || got > 2 {
		t.Errorf("burst of 10 async enqueues produced %d reindexes; want coalesced (1-2)", got)
	}
}
