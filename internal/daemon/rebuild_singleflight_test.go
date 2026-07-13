package daemon

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestRebuild_SingleFlight_NoConcurrentOverlap is the #5681 memory-safety
// assertion: at most ONE in-process group rebuild may be alive at a time, even
// when a prior Rebuild RPC times out and its heap-heavy worker goroutine is
// left running (orphaned). A superseding same-group Rebuild must NOT start a
// second concurrent rebuild — that overlap is what piled N multi-GB documents
// into the engine and blew RSS to 3.1GB+ (measured overlap peak ≈ 2x single
// run in internal/membench).
//
// Pre-fix (per-group *sync.Mutex released in the RPC handler's defer on
// timeout): the orphaned worker keeps running while the mutex is released, so a
// second same-group Rebuild acquires it and runs concurrently -> max concurrent
// == 2 -> FAIL. Post-fix (capacity-1 semaphore released only from the worker on
// real completion): the orphan holds the guard, the second Rebuild blocks and
// times out without starting work -> max concurrent == 1 -> PASS.
func TestRebuild_SingleFlight_NoConcurrentOverlap(t *testing.T) {
	// Shrink the RPC timeout so the first rebuild "times out" quickly while its
	// worker keeps running, and the second rebuild's acquire also times out
	// quickly. Restored after the test.
	prev := rebuildRPCTimeout
	rebuildRPCTimeout = 120 * time.Millisecond
	t.Cleanup(func() { rebuildRPCTimeout = prev })

	var (
		concurrent    int32
		maxConcurrent int32
	)
	release := make(chan struct{}) // unblocks the in-flight rebuild worker
	firstEntered := make(chan struct{}, 1)

	rebuildFn := func(proto.RebuildArgs) ([]string, string, error) {
		n := atomic.AddInt32(&concurrent, 1)
		for {
			m := atomic.LoadInt32(&maxConcurrent)
			if n <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, n) {
				break
			}
		}
		select {
		case firstEntered <- struct{}{}:
		default:
		}
		<-release // hold the guard the way a heap-heavy rebuild would
		atomic.AddInt32(&concurrent, -1)
		return []string{"/repo/a"}, "", nil
	}

	svc := newService(
		func(proto.IndexArgs) (string, string, error) { return "", "", nil },
		rebuildFn,
		func(proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
			return proto.QualityAuditReply{}, nil
		},
		"/tmp/test.sock",
		make(chan struct{}),
		nil,
		1,
	)

	// RPC #1: its worker enters rebuildFn and blocks on release. The RPC times
	// out after rebuildRPCTimeout and returns, but the worker keeps holding the
	// per-group guard.
	done1 := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		done1 <- svc.Rebuild(&proto.RebuildArgs{Group: "g"}, &reply)
	}()

	// Wait until the first rebuild worker is actually running inside rebuildFn.
	select {
	case <-firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first rebuild worker never entered rebuildFn")
	}

	// RPC #1 should return a timeout error while its worker is still blocked.
	select {
	case err := <-done1:
		if err == nil {
			t.Fatal("expected RPC #1 to return a timeout error while its worker blocks")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RPC #1 did not return within timeout")
	}

	// RPC #2 for the SAME group arrives while #1's worker still holds the guard.
	// It must NOT start a concurrent rebuild; it should block on acquisition and
	// time out.
	done2 := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		done2 <- svc.Rebuild(&proto.RebuildArgs{Group: "g"}, &reply)
	}()

	select {
	case err := <-done2:
		if err == nil {
			t.Fatal("expected RPC #2 to time out waiting for the in-flight rebuild, not run concurrently")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RPC #2 did not return within timeout")
	}

	// The memory-safety invariant: at no point did two rebuilds run at once.
	if got := atomic.LoadInt32(&maxConcurrent); got != 1 {
		t.Fatalf("max concurrent in-process rebuilds = %d, want 1 (overlap = engine RSS blow-up #5681)", got)
	}

	// Release the orphaned worker; a subsequent same-group rebuild must then be
	// able to acquire the guard and run (guard was not leaked).
	close(release)

	// Give the orphan a moment to release the guard.
	rebuildRPCTimeout = 2 * time.Second
	release3 := make(chan struct{})
	close(release3) // #3 returns immediately
	// Re-point rebuildFn's release for #3 by using a fresh service-independent
	// path: simplest is to assert a fresh acquire succeeds by calling Rebuild
	// once more with an already-closed release. Reuse the same svc; its rebuildFn
	// closes over `release`, already closed, so #3 completes promptly.
	done3 := make(chan error, 1)
	go func() {
		var reply proto.RebuildReply
		done3 <- svc.Rebuild(&proto.RebuildArgs{Group: "g"}, &reply)
	}()
	select {
	case err := <-done3:
		if err != nil {
			t.Fatalf("post-release rebuild should succeed once guard is free, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("post-release rebuild blocked — guard was leaked by the orphaned worker")
	}
}
