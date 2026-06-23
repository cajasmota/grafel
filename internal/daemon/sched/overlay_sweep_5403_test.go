package sched

import (
	"context"
	"sync"
	"testing"
	"time"
)

// #5403: the periodic overlay-freshness sweep re-arms a group-algo pass for a
// SETTLED group whose overlay went stale (no reindex → no link pass → no
// scheduleGroupAlgo would otherwise fire). These tests drive the testable core
// (sweepStaleOverlays) directly so no real ticker / wall-clock is needed.

// newSweepScheduler builds a scheduler whose StaleGroups returns the provided
// set and whose GroupAlgo increments a counter. A long GroupAlgoDebounce keeps
// any armed pass pending (does NOT fire) within the test window, so we can
// assert on the pending flag without racing the debounce timer.
func newSweepScheduler(t *testing.T, stale func() []string, algoCalls *int, mu *sync.Mutex) *Scheduler {
	t.Helper()
	return New(Config{
		Workers:              1,
		GroupAlgoDebounce:    10 * time.Second, // long: armed pass stays pending
		OverlaySweepInterval: time.Hour,        // we drive sweepStaleOverlays manually
		Index:                func(_ context.Context, _ string, _ string) error { return nil },
		Links:                func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			mu.Lock()
			*algoCalls++
			mu.Unlock()
			return nil
		},
		StaleGroups:        stale,
		MemReleaseDisabled: true,
	})
}

// TestOverlaySweep_StaleGroupArmsPass: a stale group gets a group-algo pass
// armed (groupAlgoPending true) when the sweep runs.
func TestOverlaySweep_StaleGroupArmsPass(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	s := newSweepScheduler(t, func() []string { return []string{"upvate"} }, &calls, &mu)
	// Do NOT Start(): we only exercise the testable core, no goroutines needed.

	s.sweepStaleOverlays()

	s.mu.Lock()
	pending := s.groupAlgoPending["upvate"]
	s.mu.Unlock()
	if !pending {
		t.Fatalf("expected a group-algo pass to be armed for the stale group, pending=%v", pending)
	}
	// Clean up the armed timer.
	s.mu.Lock()
	s.cancelGroupAlgoLocked("upvate")
	s.mu.Unlock()
}

// TestOverlaySweep_FreshGroupIsNoOp: an empty stale set arms nothing.
func TestOverlaySweep_FreshGroupIsNoOp(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	s := newSweepScheduler(t, func() []string { return nil }, &calls, &mu)

	s.sweepStaleOverlays()

	s.mu.Lock()
	n := len(s.groupAlgoTimers)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected no group-algo passes armed for a fresh sweep, got %d armed", n)
	}
}

// TestOverlaySweep_DoesNotReArmPendingPass: a group that already has a pending
// pass is NOT re-armed by the sweep (no debounce reset / thrash). We detect
// re-arm by swapping the timer pointer.
func TestOverlaySweep_DoesNotReArmPendingPass(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	s := newSweepScheduler(t, func() []string { return []string{"upvate"} }, &calls, &mu)

	// Pre-arm a pass so the group is "busy" (pending).
	s.scheduleGroupAlgo("upvate")
	s.mu.Lock()
	first := s.groupAlgoTimers["upvate"]
	s.mu.Unlock()
	if first == nil {
		t.Fatal("setup: expected a pre-armed group-algo timer")
	}

	s.sweepStaleOverlays()

	s.mu.Lock()
	second := s.groupAlgoTimers["upvate"]
	s.mu.Unlock()
	if second != first {
		t.Fatalf("sweep re-armed an already-pending group-algo pass (timer swapped) — should have skipped it")
	}
	s.mu.Lock()
	s.cancelGroupAlgoLocked("upvate")
	s.mu.Unlock()
}

// TestOverlaySweep_DoesNotReArmInFlightPass: a group with an in-flight pass
// (groupAlgoCancel present) is skipped by the sweep.
func TestOverlaySweep_DoesNotReArmInFlightPass(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	s := newSweepScheduler(t, func() []string { return []string{"upvate"} }, &calls, &mu)

	// Simulate an in-flight pass: a cancel func registered, no pending timer.
	_, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.groupAlgoCancel["upvate"] = cancel
	s.mu.Unlock()

	s.sweepStaleOverlays()

	s.mu.Lock()
	_, armed := s.groupAlgoTimers["upvate"]
	pending := s.groupAlgoPending["upvate"]
	s.mu.Unlock()
	if armed || pending {
		t.Fatalf("sweep armed a new pass while one was in flight (armed=%v pending=%v)", armed, pending)
	}
	cancel()
}

// TestOverlaySweep_NilStaleGroupsIsNoOp: nil StaleGroups makes sweep a no-op
// and the loop is never started.
func TestOverlaySweep_NilStaleGroupsIsNoOp(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	s := newSweepScheduler(t, nil, &calls, &mu)

	s.sweepStaleOverlays() // must not panic / arm anything

	s.mu.Lock()
	n := len(s.groupAlgoTimers)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("nil StaleGroups should arm nothing, got %d", n)
	}
}

// TestOverlaySweep_DisabledIntervalDoesNotStartLoop: interval 0 means the sweep
// goroutine is never started even with a StaleGroups callback. We assert the
// callback is never invoked over a short window.
func TestOverlaySweep_DisabledIntervalDoesNotStartLoop(t *testing.T) {
	var (
		mu     sync.Mutex
		called bool
	)
	s := New(Config{
		Workers:              1,
		GroupAlgoDebounce:    10 * time.Second,
		OverlaySweepInterval: -1, // negative: explicitly disabled (bypasses env default)
		Index:                func(_ context.Context, _ string, _ string) error { return nil },
		Links:                func(_ context.Context, _ string) error { return nil },
		GroupAlgo:            func(_ context.Context, _ string) error { return nil },
		StaleGroups: func() []string {
			mu.Lock()
			called = true
			mu.Unlock()
			return []string{"upvate"}
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	got := called
	mu.Unlock()
	if got {
		t.Fatal("overlay sweep loop ran despite a disabled (<=0) interval")
	}
}
