package daemon

// Regression tests for the two engine-side rebuild-drain defects (epic #5729,
// split mode ON):
//
//   (d) A KindRebuild that keeps getting interrupted mid-apply (memlimit /
//       reaper / crash / RPC EOF) must NOT re-run unboundedly on every 2s
//       drain tick / every engine restart. Bounded recovery, then dead-letter.
//   (c) The per-group single-flight guard must live on the ENGINE side (where
//       rebuildFn actually runs), so two concurrent KindRebuild applications
//       for the SAME group serialise — while DIFFERENT groups still overlap.

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
)

// (d): a rebuild whose apply crashes mid-flight (simulated by a panicking
// rebuildFn — the in-process analogue of a SIGKILL that dies before the ack is
// written) must be re-applied AT MOST maxRebuildAttempts times across repeated
// drains, then dead-lettered — never once-per-drain-tick forever.
//
// Old behaviour (no persisted attempt claim / no bounded recovery): each drain
// re-applies the never-acked request, so the call count climbs with every
// drain pass (here: 8) — this test fails, proving the unbounded loop.
func TestDrain_KindRebuild_CrashLoopIsBounded(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "1")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	const group = "d-crashloop-group"
	dir := requestsDirForGroup(group)
	payload, err := json.Marshal(proto.RebuildArgs{Group: group})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := requests.Write(dir, requests.Record{Kind: requests.KindRebuild, Payload: payload}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var calls int32
	rebuildFn := func(proto.RebuildArgs) ([]string, string, error) {
		atomic.AddInt32(&calls, 1)
		panic("simulated memlimit SIGKILL mid-rebuild")
	}

	// Simulate 8 engine restarts / drain ticks. The outer recover models the
	// process dying and being restarted: with the OLD code drainRequestsOnce
	// itself panics (no per-record recovery) and the request is never acked,
	// so every iteration re-applies. With the fix, the drain recovers the
	// crash internally, persists a bounded attempt count, and dead-letters.
	const drains = 8
	for i := 0; i < drains; i++ {
		func() {
			defer func() { _ = recover() }()
			_ = drainRequestsOnce(requestsRoot(), nil, rebuildFn, nil)
		}()
	}

	got := atomic.LoadInt32(&calls)
	if got < 1 {
		t.Fatalf("expected the rebuild to be attempted at least once, got %d", got)
	}
	if got > maxRebuildAttempts {
		t.Fatalf("rebuild re-applied %d times across %d drains — unbounded loop; want at most %d (bounded recovery)", got, drains, maxRebuildAttempts)
	}

	// After the attempt budget is exhausted the request must be gone
	// (dead-lettered), so further drains never re-run it.
	recs, err := requests.ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected the request to be dead-lettered (removed) after the attempt budget, still pending: %+v", recs)
	}
}

// (c): two concurrent KindRebuild applications for the SAME group must be
// serialised — rebuildFn is never in-flight more than once at a time.
//
// Old behaviour (single-flight guard on the dead serve side, none on the
// engine drain side): the two applyRequest calls run rebuildFn concurrently,
// peak concurrency reaches 2 — this test fails.
func TestApplyRequest_KindRebuild_SameGroupSerialised(t *testing.T) {
	const group = "c-same-group"
	payload, err := json.Marshal(proto.RebuildArgs{Group: group})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := requests.Record{ID: "same-1", Kind: requests.KindRebuild, Payload: payload}

	var inFlight int32
	var peak int32
	rebuildFn := func(proto.RebuildArgs) ([]string, string, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return []string{"/repo"}, "", nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := applyRequest(nil, rebuildFn, rec); err != nil {
				t.Errorf("applyRequest: %v", err)
			}
		}()
	}
	wg.Wait()

	if p := atomic.LoadInt32(&peak); p > 1 {
		t.Fatalf("peak concurrent rebuildFn executions for the same group = %d, want 1 (engine-side single-flight)", p)
	}
}

// (c) preserved behaviour: DIFFERENT groups may rebuild concurrently. Each
// rebuildFn waits (bounded) for the other to arrive; if the guard wrongly
// serialised across groups the first would block forever waiting for a second
// that can never start, and the barrier would time out.
func TestApplyRequest_KindRebuild_DifferentGroupsConcurrent(t *testing.T) {
	mkRec := func(group, id string) requests.Record {
		payload, err := json.Marshal(proto.RebuildArgs{Group: group})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return requests.Record{ID: id, Kind: requests.KindRebuild, Payload: payload}
	}

	arrived := make(chan struct{}, 2)
	proceed := make(chan struct{})
	bothConcurrent := int32(0)
	rebuildFn := func(proto.RebuildArgs) ([]string, string, error) {
		arrived <- struct{}{}
		select {
		case <-proceed:
			atomic.AddInt32(&bothConcurrent, 1)
		case <-time.After(2 * time.Second):
		}
		return []string{"/repo"}, "", nil
	}

	var wg sync.WaitGroup
	for _, g := range []struct{ group, id string }{{"c-diff-a", "a-1"}, {"c-diff-b", "b-1"}} {
		rec := mkRec(g.group, g.id)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := applyRequest(nil, rebuildFn, rec); err != nil {
				t.Errorf("applyRequest: %v", err)
			}
		}()
	}

	// Wait for both rebuildFns to arrive concurrently, then release them.
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(2 * time.Second):
			t.Fatal("different-group rebuilds did not run concurrently (guard over-serialised across groups)")
		}
	}
	close(proceed)
	wg.Wait()

	if atomic.LoadInt32(&bothConcurrent) != 2 {
		t.Fatalf("expected both different-group rebuilds to be in-flight concurrently, got %d", atomic.LoadInt32(&bothConcurrent))
	}
}
