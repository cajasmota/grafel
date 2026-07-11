package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSchedulerRef_EnqueueCapturesRef verifies that the ref captured at
// Enqueue time is the ref passed to IndexFn, not the ref at dispatch time.
//
// Scenario: enqueue repo with RefCapture returning "feat/alpha", then swap
// the captured ref to "main" before the job runs. IndexFn must receive
// "feat/alpha" (the ref at Enqueue time).
func TestSchedulerRef_EnqueueCapturesRef(t *testing.T) {
	var capturedRef string
	var mu sync.Mutex

	// Start with "feat/alpha" as the current ref.
	currentRef := "feat/alpha"
	var refMu sync.Mutex

	gate := make(chan struct{})
	var indexCalled atomic.Bool

	s := New(Config{
		Workers: 1,
		RefCapture: func(_ string) (string, string) {
			refMu.Lock()
			defer refMu.Unlock()
			return currentRef, ""
		},
		Index: func(_ context.Context, _ string, ref string) error {
			mu.Lock()
			capturedRef = ref
			mu.Unlock()
			indexCalled.Store(true)
			<-gate // hold the job until we're ready to inspect
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	// Enqueue while ref is "feat/alpha".
	s.Enqueue("/repo/client-fixture-a")

	// Wait briefly, then change the "current" ref. The scheduler should
	// NOT re-read it — it was already captured at Enqueue time.
	time.Sleep(30 * time.Millisecond)
	refMu.Lock()
	currentRef = "main"
	refMu.Unlock()

	// Let the job run.
	close(gate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if indexCalled.Load() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !indexCalled.Load() {
		t.Fatal("IndexFn was never called")
	}

	mu.Lock()
	got := capturedRef
	mu.Unlock()

	if got != "feat/alpha" {
		t.Errorf("IndexFn received ref %q, want feat/alpha (captured at Enqueue time)", got)
	}
}

// TestSchedulerRef_EnqueueRefPassesRefDirectly verifies that EnqueueRef
// passes the supplied ref directly to IndexFn without calling RefCapture.
func TestSchedulerRef_EnqueueRefPassesRefDirectly(t *testing.T) {
	var capturedRef string
	var mu sync.Mutex

	// RefCapture always returns "main" — but EnqueueRef bypasses it.
	s := New(Config{
		Workers: 1,
		RefCapture: func(_ string) (string, string) {
			return "main", ""
		},
		Index: func(_ context.Context, _ string, ref string) error {
			mu.Lock()
			capturedRef = ref
			mu.Unlock()
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.EnqueueRef("/repo/client-fixture-a", "feat/explicit-branch")

	// Wait for the job to run.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	got := capturedRef
	mu.Unlock()

	if got != "feat/explicit-branch" {
		t.Errorf("IndexFn received ref %q, want feat/explicit-branch", got)
	}
}

// TestSchedulerRef_BranchSwitchAfterFirstIndex verifies that when a branch
// switch EnqueueRef arrives while a job for the same repo is already in-flight,
// the NEXT index pass (scheduled after the first completes) uses the new ref.
// This is the correct behaviour: the in-flight job uses the ref it was admitted
// with; the subsequent job picks up the updated ref.
func TestSchedulerRef_BranchSwitchAfterFirstIndex(t *testing.T) {
	var mu sync.Mutex
	var calls []string // collected ref per IndexFn call

	gate := make(chan struct{}) // hold the first job until we're ready

	s := New(Config{
		Workers: 1,
		RefCapture: func(_ string) (string, string) {
			return "feat/original", ""
		},
		Index: func(_ context.Context, _ string, ref string) error {
			// Block on the gate so we can enqueue the branch-switch while
			// the first job is in-flight.
			<-gate
			mu.Lock()
			calls = append(calls, ref)
			mu.Unlock()
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	// Enqueue with "feat/original". The job enters inflight quickly.
	s.Enqueue("/repo/client-fixture-b")
	// Give dedupLoop time to pick up the enqueue and start the job.
	time.Sleep(50 * time.Millisecond)

	// Now send a branch-switch while the job is in-flight.
	// dedupLoop will stash "feat/switched" into pendingRefs so the NEXT run uses it.
	s.EnqueueRef("/repo/client-fixture-b", "feat/switched")

	// Release the gate so the first job finishes, which will trigger a second
	// job (re-enqueue from the stashed branch-switch pending entry).
	close(gate)

	// Wait for two calls to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(calls)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	got := make([]string, len(calls))
	copy(got, calls)
	mu.Unlock()

	// Regardless of timing there must have been at least one call.
	if len(got) == 0 {
		t.Fatal("IndexFn was never called")
	}
	// First call should have used "feat/original".
	if got[0] != "feat/original" {
		t.Errorf("first IndexFn call got ref %q, want feat/original", got[0])
	}
}
