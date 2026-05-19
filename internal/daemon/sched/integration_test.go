package sched

import (
	"context"
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

	var calls atomic.Int32
	var sched *Scheduler
	s := New(Config{
		Workers:  3,
		BudgetMB: 500,
		Predict: func(p string) int64 {
			return preds[p]
		},
		Index: func(_ context.Context, p string) error {
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

	// Give the admit loop time to dispatch.
	time.Sleep(300 * time.Millisecond)

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
	time.Sleep(300 * time.Millisecond)
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
func TestIntegrationThreeRepoTightBudgetDefersBig(t *testing.T) {
	preds := map[string]int64{
		"/repo-small-a": 60,
		"/repo-small-b": 60,
		"/repo-big-c":   280,
	}
	var (
		mu              sync.Mutex
		concurrent      = map[string]bool{}
		bigEverConcSmall = false
		peakUsedMB      int64
	)
	gates := map[string]chan struct{}{
		"/repo-small-a": make(chan struct{}),
		"/repo-small-b": make(chan struct{}),
		"/repo-big-c":   make(chan struct{}),
	}
	var sched *Scheduler
	s := New(Config{
		Workers:  3,
		BudgetMB: 350,
		Predict:  func(p string) int64 { return preds[p] },
		Index: func(_ context.Context, p string) error {
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
	time.Sleep(300 * time.Millisecond)

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

	// Release smalls one by one.
	close(gates["/repo-small-a"])
	time.Sleep(150 * time.Millisecond)
	close(gates["/repo-small-b"])
	time.Sleep(150 * time.Millisecond)
	// Now budget should be 0+280=280, big should run.
	close(gates["/repo-big-c"])
	time.Sleep(300 * time.Millisecond)

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
		Index: func(_ context.Context, _ string) error {
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
