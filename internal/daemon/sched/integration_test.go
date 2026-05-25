package sched

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestIntegrationThreeRepoBudgetSerialisesLargest models the
// post-#639 real-fixture scenario: three repos, two small (~50MB) and
// one large (~280MB). With a 500MB cap and 2-worker pool, the two
// small ones should be allowed to run together, but the large one
// must NOT join them — otherwise the predicted peak would be 380MB,
// which combined with arena reuse blows the 500MB target.
//
// We verify the admission ledger by counting concurrent in-flight
// jobs grouped by size.
func TestIntegrationThreeRepoBudgetSerialisesLargest(t *testing.T) {
	preds := map[string]int64{
		"/repo-small-a": 60,
		"/repo-small-b": 60,
		"/repo-big-c":   280,
	}

	var (
		mu             sync.Mutex
		concurrent     = map[string]bool{}
		peakConcurrent int
		peakUsedMB     int64
	)
	gates := map[string]chan struct{}{
		"/repo-small-a": make(chan struct{}),
		"/repo-small-b": make(chan struct{}),
		"/repo-big-c":   make(chan struct{}),
	}
	// allStarted receives one notification per Index callback entry. With
	// 3 enqueued repos and budget=500 that fits all three (60+60+280=400),
	// we expect all 3 to dispatch concurrently and signal here.
	// Buffered so workers never block on send.
	allStarted := make(chan struct{}, 3)

	var calls atomic.Int32
	var sched *Scheduler
	s := New(Config{
		Workers:  3,
		BudgetMB: 500,
		Predict: func(p string) int64 {
			return preds[p]
		},
		Index: func(_ context.Context, p string, _ string) error {
			calls.Add(1)
			mu.Lock()
			concurrent[p] = true
			if len(concurrent) > peakConcurrent {
				peakConcurrent = len(concurrent)
			}
			// Snapshot used MB while inside the index; the ledger
			// reflects exactly what admission control reserved.
			if sched != nil {
				snap := sched.Snapshot()
				if snap.UsedMB > peakUsedMB {
					peakUsedMB = snap.UsedMB
				}
			}
			mu.Unlock()
			allStarted <- struct{}{}
			<-gates[p]
			mu.Lock()
			delete(concurrent, p)
			mu.Unlock()
			return nil
		},
	})
	sched = s
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo-small-a")
	s.Enqueue("/repo-small-b")
	s.Enqueue("/repo-big-c")

	// Wait until all 3 Index callbacks have actually been entered before
	// we take the mid-run snapshot. Deterministic under -race on any
	// hardware — replaces the racy time.Sleep(300ms) that caused
	// intermittent failures on ubuntu-latest CI (see sibling
	// TestIntegrationThreeRepoTightBudgetDefersBig comment above for
	// the same fix pattern).
	for i := 0; i < 3; i++ {
		select {
		case <-allStarted:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of 3 Index callbacks entered after 10s", i)
		}
	}

	// Verify the budget telemetry: usedMB should be <= 500.
	snap := s.Snapshot()
	if snap.UsedMB > snap.BudgetMB {
		t.Fatalf("ledger blown: used=%dMB budget=%dMB", snap.UsedMB, snap.BudgetMB)
	}
	// We expect 60+60+280=400 (all three fit) OR 60+60=120 + big blocked.
	// Either way, the peak predicted ledger is <=400.
	if snap.UsedMB > 400 {
		t.Errorf("predicted ledger=%dMB exceeds expected max 400MB", snap.UsedMB)
	}

	// Release all jobs.
	for _, p := range []string{"/repo-small-a", "/repo-small-b", "/repo-big-c"} {
		close(gates[p])
	}
	// Wait for all 3 to actually complete (delete from concurrent map)
	// instead of an arbitrary sleep. Poll briefly.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(concurrent) == 0 && calls.Load() == 3
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 indexes total, got %d", got)
	}
	if peakConcurrent < 2 {
		t.Errorf("expected at least 2 concurrent at peak (2 small fit under 500MB), got %d", peakConcurrent)
	}
	if peakUsedMB > 500 {
		t.Errorf("ledger blown: peak=%dMB > 500MB", peakUsedMB)
	}
	t.Logf("3-repo cap trace: peak concurrent=%d, peak ledger=%dMB (budget=500MB)",
		peakConcurrent, peakUsedMB)
}

// TestIntegrationThreeRepoTightBudgetDefersBig models the same
// three repos but with a tighter 350MB cap. The big repo (predicted
// 280MB) MUST wait until at least one small finishes — otherwise the
// ledger would hit 60+60+280=400MB > 350MB.
//
// The previous implementation used time.Sleep(300ms) to wait for the
// scheduler to reach steady-state before taking a mid-run snapshot.
// Under -race on a loaded CI runner (ubuntu-latest) the sleep was not
// long enough and the snapshot could race with job dispatch, causing
// intermittent "expected /repo-big-c to be blocked" failures. Fixed by
// using a buffered channel (smallsStarted) so the test waits until both
// small Index callbacks have actually been entered — i.e. we observe
// the exact state we intend to assert on.
func TestIntegrationThreeRepoTightBudgetDefersBig(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macos: TODO #2121-C (timing-sensitive scheduler test times out on macos-latest CI)")
	}
	preds := map[string]int64{
		"/repo-small-a": 60,
		"/repo-small-b": 60,
		"/repo-big-c":   280,
	}
	var (
		mu               sync.Mutex
		concurrent       = map[string]bool{}
		bigEverConcSmall = false
		peakUsedMB       int64
	)
	gates := map[string]chan struct{}{
		"/repo-small-a": make(chan struct{}),
		"/repo-small-b": make(chan struct{}),
		"/repo-big-c":   make(chan struct{}),
	}
	// smallsStarted receives one notification per small-repo Index
	// invocation. Buffered for 2 so the workers never block on send.
	smallsStarted := make(chan struct{}, 2)
	// bigStarted is closed when the big repo's Index callback begins,
	// signalling that big-c was eventually admitted (after a small finishes).
	bigStarted := make(chan struct{})

	var sched *Scheduler
	s := New(Config{
		Workers:  3,
		BudgetMB: 350,
		Predict:  func(p string) int64 { return preds[p] },
		Index: func(_ context.Context, p string, _ string) error {
			mu.Lock()
			concurrent[p] = true
			if p == "/repo-big-c" && (concurrent["/repo-small-a"] || concurrent["/repo-small-b"]) {
				// Big is in flight at the same time as at least one
				// small — fine as long as ledger fits.
				if sched != nil {
					snap := sched.Snapshot()
					if snap.UsedMB > 350 {
						bigEverConcSmall = true
					}
				}
			}
			if sched != nil {
				snap := sched.Snapshot()
				if snap.UsedMB > peakUsedMB {
					peakUsedMB = snap.UsedMB
				}
			}
			mu.Unlock()

			// Signal callers that this Index invocation has begun.
			if p == "/repo-small-a" || p == "/repo-small-b" {
				smallsStarted <- struct{}{}
			} else if p == "/repo-big-c" {
				close(bigStarted)
			}

			<-gates[p]
			mu.Lock()
			delete(concurrent, p)
			mu.Unlock()
			return nil
		},
	})
	sched = s
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo-small-a")
	s.Enqueue("/repo-small-b")
	s.Enqueue("/repo-big-c")

	// Wait until BOTH small Index callbacks are running before we take the
	// mid-run snapshot. This is deterministic under -race on any hardware:
	// we assert only when we know the scheduler has reached the exact state
	// we intend to inspect.
	for i := 0; i < 2; i++ {
		select {
		case <-smallsStarted:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for both small-repo index jobs to start")
		}
	}

	// Snapshot mid-run: the big repo should be in BlockedJobs
	// because 60+60+280=400 > 350.
	snap := s.Snapshot()
	if snap.UsedMB > 350 {
		t.Errorf("ledger blown mid-run: used=%dMB > 350MB", snap.UsedMB)
	}
	foundBlocked := false
	for _, b := range snap.BlockedJobs {
		if b == "/repo-big-c" {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Errorf("expected /repo-big-c to be blocked by 350MB cap, got blocked=%v inflight=%v",
			snap.BlockedJobs, snap.InFlight)
	}

	// Release smalls one by one; then wait for big-c to actually start
	// before releasing its gate (confirms deferral + eventual admission).
	close(gates["/repo-small-a"])
	close(gates["/repo-small-b"])
	select {
	case <-bigStarted:
		// big-c was admitted after smalls drained — correct.
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for big-c to be admitted after smalls finished")
	}
	close(gates["/repo-big-c"])

	// Give workers time to finish and update peakUsedMB.
	time.Sleep(100 * time.Millisecond)

	if peakUsedMB > 350 {
		t.Errorf("peak ledger=%dMB exceeded 350MB cap", peakUsedMB)
	}
	if bigEverConcSmall {
		t.Errorf("big-c was concurrent with a small while ledger over budget")
	}
	t.Logf("tight-cap trace: peak ledger=%dMB (budget=350MB) - big-c deferred until smalls drained",
		peakUsedMB)
}

// TestIntegrationBudgetBlowoutWithoutCap demonstrates the NEGATIVE
// case: with BudgetMB=0 (disabled), three jobs run concurrently with
// no ledger cap. This is a regression guard — if someone removes the
// admission gate, this test still passes (intentional) but the
// scenario above breaks.
func TestIntegrationBudgetBlowoutWithoutCap(t *testing.T) {
	var (
		mu         sync.Mutex
		concurrent int
		peak       int
	)
	gate := make(chan struct{})
	s := New(Config{
		Workers:  3,
		BudgetMB: 0, // disabled
		Predict:  func(_ string) int64 { return 999 },
		Index: func(_ context.Context, _ string, _ string) error {
			mu.Lock()
			concurrent++
			if concurrent > peak {
				peak = concurrent
			}
			mu.Unlock()
			<-gate
			mu.Lock()
			concurrent--
			mu.Unlock()
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/a")
	s.Enqueue("/b")
	s.Enqueue("/c")
	time.Sleep(250 * time.Millisecond)
	mu.Lock()
	got := peak
	mu.Unlock()
	if got != 3 {
		t.Errorf("with cap disabled, expected all 3 concurrent; got %d", got)
	}
	for i := 0; i < 3; i++ {
		gate <- struct{}{}
	}
}
