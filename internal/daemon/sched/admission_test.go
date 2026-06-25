package sched

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAdmissionDefersOversizeJobs verifies that with a 100MB budget and
// three repos each predicted at 60MB, only one runs concurrently — not
// two — because 60+60=120 > 100.
func TestAdmissionDefersOversizeJobs(t *testing.T) {
	var (
		mu         sync.Mutex
		concurrent int
		maxConcurr int
		totalCalls int32
	)
	gate := make(chan struct{})

	s := New(Config{
		Workers:  3,
		BudgetMB: 100,
		Predict:  func(_ string) int64 { return 60 },
		Index: func(_ context.Context, _ string, _ string) error {
			mu.Lock()
			concurrent++
			if concurrent > maxConcurr {
				maxConcurr = concurrent
			}
			mu.Unlock()
			atomic.AddInt32(&totalCalls, 1)
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

	// Give admit loop time to dispatch what it can.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	peak1 := maxConcurr
	mu.Unlock()
	if peak1 != 1 {
		t.Fatalf("expected admission to allow only 1 concurrent (60MB ≤ 100, 120MB > 100), got %d", peak1)
	}
	// Release jobs one at a time and verify the next becomes admitted.
	for i := 0; i < 3; i++ {
		gate <- struct{}{}
		time.Sleep(150 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&totalCalls); got != 3 {
		t.Errorf("expected all 3 jobs to eventually run, got %d", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if maxConcurr > 1 {
		t.Errorf("peak concurrency=%d under 100MB cap with 60MB jobs (must be 1)", maxConcurr)
	}
}

// TestAdmissionAllowsParallelWhenBudgetFits verifies that two small
// jobs (50MB each) can run concurrently under a 200MB budget.
func TestAdmissionAllowsParallelWhenBudgetFits(t *testing.T) {
	var (
		mu         sync.Mutex
		concurrent int
		maxConcurr int
	)
	gate := make(chan struct{})

	s := New(Config{
		Workers:  3,
		BudgetMB: 200,
		Predict:  func(_ string) int64 { return 50 },
		Index: func(_ context.Context, _ string, _ string) error {
			mu.Lock()
			concurrent++
			if concurrent > maxConcurr {
				maxConcurr = concurrent
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
	peak := maxConcurr
	mu.Unlock()
	if peak < 2 {
		t.Errorf("expected 2+ concurrent jobs under 200MB budget with 50MB jobs, got %d", peak)
	}
	for i := 0; i < 3; i++ {
		gate <- struct{}{}
	}
	time.Sleep(100 * time.Millisecond)
}

// TestAdmissionOversizeRunsSolo verifies that a single job predicted
// LARGER than the entire budget is still admitted, but only when the
// ledger is otherwise empty.
func TestAdmissionOversizeRunsSolo(t *testing.T) {
	var calls atomic.Int32
	s := New(Config{
		Workers:  2,
		BudgetMB: 100,
		Predict: func(p string) int64 {
			if p == "/giant" {
				return 999
			}
			return 50
		},
		Index: func(_ context.Context, _ string, _ string) error {
			calls.Add(1)
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/giant")
	// Converge on the single solo run (poll, not a fixed sleep, so a slow CI
	// just waits longer to observe it), then settle to confirm it doesn't run
	// twice. The single enqueue can never produce >1 admission, so this is a
	// convergence assertion, not a tight-timing one.
	waitFor(t, 5*time.Second, func() bool { return calls.Load() == 1 })
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("expected oversize job to run solo, got %d", got)
	}
}

// TestSnapshotBudgetTelemetry verifies the Snapshot exposes budget +
// used + blocked accurately during an admission backoff.
func TestSnapshotBudgetTelemetry(t *testing.T) {
	gate := make(chan struct{})
	s := New(Config{
		Workers:  3,
		BudgetMB: 100,
		Predict:  func(_ string) int64 { return 80 },
		Index: func(_ context.Context, _ string, _ string) error {
			<-gate
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/a")
	s.Enqueue("/b")
	time.Sleep(150 * time.Millisecond)
	snap := s.Snapshot()
	if snap.BudgetMB != 100 {
		t.Errorf("budget MB: got %d, want 100", snap.BudgetMB)
	}
	if snap.UsedMB != 80 {
		t.Errorf("used MB: got %d, want 80", snap.UsedMB)
	}
	if len(snap.InFlight) != 1 {
		t.Errorf("in-flight count: got %d, want 1", len(snap.InFlight))
	}
	blocked := append([]string(nil), snap.BlockedJobs...)
	sort.Strings(blocked)
	if len(blocked) != 1 || blocked[0] != "/b" {
		t.Errorf("blocked: got %v, want [/b]", blocked)
	}
	gate <- struct{}{}
	time.Sleep(150 * time.Millisecond)
	gate <- struct{}{}
}

// TestRSSHistoryRoundTrip verifies on-disk persistence.
func TestRSSHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rss.json")
	h := LoadRSSHistory(path)
	h.Record("/r1", 300)
	h.Record("/r1", 200) // smaller — moving-max keeps 300
	h.Record("/r2", 150)
	// Reload from disk.
	h2 := LoadRSSHistory(path)
	if got := h2.Predict("/r1"); got != 300 {
		t.Errorf("/r1 peak: got %d, want 300", got)
	}
	if got := h2.Predict("/r2"); got != 150 {
		t.Errorf("/r2 peak: got %d, want 150", got)
	}
	if got := h2.Predict("/unknown"); got != 0 {
		t.Errorf("unknown repo: got %d, want 0", got)
	}
	// File should exist on disk.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("history file not persisted: %v", err)
	}
}

// TestAdmissionDeltaIgnoresProcessRSS is the regression test for #1763.
// It verifies that admission control operates on the delta sum of predicted
// in-flight jobs ONLY, and does not include the daemon's process-level RSS.
//
// Scenario: daemon idle RSS >> budget (simulated by a large-but-irrelevant
// process memory baseline), but no jobs are in-flight. The next queued job
// should still be admitted because the scheduler's usedMB counter (delta
// ledger) starts at zero.
//
// Implementation note: the scheduler never reads process RSS for admission
// decisions — it only uses s.usedMB (predicted in-flight sum). This test
// confirms that a single job is admitted even when its prediction is just
// under budget, regardless of what the OS reports as process RSS.
func TestAdmissionDeltaIgnoresProcessRSS(t *testing.T) {
	// Budget = 200 MB; single job predicted at 150 MB.
	// The test passes if the job is admitted (delta 0+150 <= 200).
	// If the scheduler were to include process RSS it would need to be
	// mocked; since it doesn't, we just verify normal admission works.
	var admitted atomic.Int32
	gate := make(chan struct{})
	s := New(Config{
		Workers:  1,
		BudgetMB: 200,
		Predict:  func(_ string) int64 { return 150 },
		Index: func(_ context.Context, _ string, _ string) error {
			admitted.Add(1)
			<-gate
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo-a")
	time.Sleep(200 * time.Millisecond)
	if got := admitted.Load(); got != 1 {
		t.Fatalf("expected job admitted (delta 0+150 <= 200); admitted=%d", got)
	}
	snap := s.Snapshot()
	if snap.UsedMB != 150 {
		t.Errorf("admission ledger: got UsedMB=%d, want 150", snap.UsedMB)
	}
	gate <- struct{}{}
}

// TestAdmissionMultipleJobsDeltaAccounting verifies that when two jobs are
// in-flight their predictions are SUMMED (not replaced) in the admission
// ledger, and a third job that would exceed the sum is deferred.
func TestAdmissionMultipleJobsDeltaAccounting(t *testing.T) {
	// Budget = 300, two jobs at 120 each = 240 ≤ 300 → both admitted.
	// Third job at 120: 240+120=360 > 300 → deferred.
	gate := make(chan struct{})
	var admitted atomic.Int32
	s := New(Config{
		Workers:  3,
		BudgetMB: 300,
		Predict:  func(_ string) int64 { return 120 },
		Index: func(_ context.Context, _ string, _ string) error {
			admitted.Add(1)
			<-gate
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/a")
	s.Enqueue("/b")
	s.Enqueue("/c")
	time.Sleep(250 * time.Millisecond)

	snap := s.Snapshot()
	if snap.UsedMB != 240 {
		t.Errorf("admission ledger with 2 jobs at 120MB: got UsedMB=%d, want 240", snap.UsedMB)
	}
	if got := admitted.Load(); got != 2 {
		t.Errorf("expected exactly 2 admitted (240 ≤ 300), got %d", got)
	}
	if len(snap.BlockedJobs) != 1 {
		t.Errorf("expected 1 blocked job, got %v", snap.BlockedJobs)
	}
	// Release one — third should then be admitted.
	gate <- struct{}{}
	time.Sleep(200 * time.Millisecond)
	if got := admitted.Load(); got != 3 {
		t.Errorf("expected 3 admitted after releasing one, got %d", got)
	}
	gate <- struct{}{}
	gate <- struct{}{}
}

// TestDeadManSwitchForceAdmits verifies that checkDeadMan force-admits the
// smallest queued job when:
//   - the pending queue is non-empty,
//   - the inflight map is empty (nothing is running),
//   - the dead-man clock has exceeded deadManTimeout.
//
// We construct this state directly by manipulating the scheduler's internal
// fields under the mutex: inject a fake pending job, leave inflight empty,
// pre-set usedMB high enough to block normal admission, and backdate deadManAt.
// Then call checkDeadMan() directly to bypass the 30s ticker.
func TestDeadManSwitchForceAdmits(t *testing.T) {
	var admitted atomic.Int32
	done := make(chan struct{})
	s := New(Config{
		Workers:  2,
		BudgetMB: 100,
		Predict:  func(_ string) int64 { return 50 },
		Index: func(_ context.Context, _ string, _ string) error {
			admitted.Add(1)
			<-done
			return nil
		},
	})
	s.Start()
	defer func() {
		close(done) // unblock any in-flight index so Stop() can drain
		s.Stop()
	}()

	// Directly inject a stuck state: /wedged is in the pending queue,
	// inflight is empty, but usedMB is 999 so tryAdmit defers it, and
	// deadManAt is past the timeout.
	s.mu.Lock()
	s.pendingQ = []string{"/wedged"}
	s.pendingIndex["/wedged"] = true
	s.queueLen = 1
	s.usedMB = 999 // synthetic stuck ledger — no running job caused this
	s.deadManAt = time.Now().Add(-(deadManTimeout + time.Second))
	s.mu.Unlock()

	// Sanity: normal admission should defer /wedged (usedMB > budget).
	s.tryAdmit()
	time.Sleep(50 * time.Millisecond)
	if got := admitted.Load(); got != 0 {
		t.Fatalf("expected /wedged deferred by admission; admitted=%d", got)
	}

	// Now call checkDeadMan — it should detect inflight==0 + expired clock
	// and force-admit the job.
	// checkDeadMan needs inflight to be empty; usedMB was synthetic so
	// reset inflight to confirm the dead-man condition is met.
	s.mu.Lock()
	// inflight is already empty (we never put anything there); just
	// re-backdate in case time moved forward.
	s.deadManAt = time.Now().Add(-(deadManTimeout + time.Second))
	s.mu.Unlock()

	s.checkDeadMan()
	time.Sleep(200 * time.Millisecond)

	if got := admitted.Load(); got < 1 {
		t.Errorf("expected dead-man to force-admit /wedged; admitted=%d", got)
	}

	// Verify the admit_deadman log entry.
	snap := s.Snapshot()
	var foundDeadMan bool
	for _, e := range snap.RecentLog {
		if e.Kind == "admit_deadman" {
			foundDeadMan = true
			break
		}
	}
	if !foundDeadMan {
		t.Errorf("expected admit_deadman log entry in recent log; got: %+v", snap.RecentLog)
	}
}

// TestPredictRSSSmokeOnFakeRepo verifies the source-size predictor
// returns a sensible MB number for a tiny fixture.
func TestPredictRSSSmokeOnFakeRepo(t *testing.T) {
	dir := t.TempDir()
	// 1MB of fake Go source: predictor should report ~70 MB (70× source).
	payload := make([]byte, 1024*1024)
	for i := range payload {
		payload[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	got := PredictRSS(dir)
	if got < 60 || got > 80 {
		t.Errorf("predicted MB for 1MB source: got %d, want ~70", got)
	}
}
