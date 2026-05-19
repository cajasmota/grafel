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
		mu          sync.Mutex
		concurrent  int
		maxConcurr  int
		totalCalls  int32
	)
	gate := make(chan struct{})

	s := New(Config{
		Workers:  3,
		BudgetMB: 100,
		Predict:  func(_ string) int64 { return 60 },
		Index: func(_ context.Context, _ string) error {
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
		mu          sync.Mutex
		concurrent  int
		maxConcurr  int
	)
	gate := make(chan struct{})

	s := New(Config{
		Workers:  3,
		BudgetMB: 200,
		Predict:  func(_ string) int64 { return 50 },
		Index: func(_ context.Context, _ string) error {
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
		Index: func(_ context.Context, _ string) error {
			calls.Add(1)
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/giant")
	time.Sleep(200 * time.Millisecond)
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
		Index: func(_ context.Context, _ string) error {
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
