package sched

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// newMemTestScheduler builds a scheduler wired with a stub FreeOSMemory and a
// short debounce, WITHOUT starting the goroutine loops — the tests drive
// maybeReleaseMemory directly with a synthetic clock so they are fully
// deterministic (no sleeps, no real STW GC).
func newMemTestScheduler(debounce time.Duration, free func()) *Scheduler {
	return New(Config{
		Workers:            1,
		MemReleaseDebounce: debounce,
		FreeOSMemory:       free,
		Index:              func(context.Context, string, string) error { return nil },
	})
}

// TestMaybeReleaseMemory_FiresAfterIdleDebounce asserts FreeOSMemory is
// called exactly once after the scheduler has been idle for the full debounce
// window — and not before.
func TestMaybeReleaseMemory_FiresAfterIdleDebounce(t *testing.T) {
	var freed atomic.Int32
	s := newMemTestScheduler(30*time.Second, func() { freed.Add(1) })

	base := time.Now()
	// First idle observation: arms the clock, does not fire.
	s.maybeReleaseMemory(base)
	if got := freed.Load(); got != 0 {
		t.Fatalf("released on first idle tick; want 0 got %d", got)
	}
	// Still inside the debounce window: must not fire.
	s.maybeReleaseMemory(base.Add(20 * time.Second))
	if got := freed.Load(); got != 0 {
		t.Fatalf("released before debounce elapsed; want 0 got %d", got)
	}
	// Past the debounce window: fires exactly once.
	s.maybeReleaseMemory(base.Add(31 * time.Second))
	if got := freed.Load(); got != 1 {
		t.Fatalf("did not release after debounce; want 1 got %d", got)
	}
	// Subsequent idle ticks in the SAME idle period must not re-fire.
	s.maybeReleaseMemory(base.Add(60 * time.Second))
	s.maybeReleaseMemory(base.Add(120 * time.Second))
	if got := freed.Load(); got != 1 {
		t.Fatalf("released more than once in one idle period; want 1 got %d", got)
	}
}

// TestMaybeReleaseMemory_BusyResetsAndDebounces asserts that activity
// (in-flight work) resets the idle clock and re-arms the one-shot release, so
// a new idle period must serve out a fresh debounce before firing again.
func TestMaybeReleaseMemory_BusyResetsAndDebounces(t *testing.T) {
	var freed atomic.Int32
	s := newMemTestScheduler(30*time.Second, func() { freed.Add(1) })

	base := time.Now()
	s.maybeReleaseMemory(base)
	s.maybeReleaseMemory(base.Add(31 * time.Second)) // fires (1)
	if got := freed.Load(); got != 1 {
		t.Fatalf("want 1 release after first idle period, got %d", got)
	}

	// Simulate work arriving: a job goes in-flight.
	s.mu.Lock()
	s.inflight["/repo"] = 1
	s.mu.Unlock()

	// A tick while busy resets the idle clock and re-arms the release.
	s.maybeReleaseMemory(base.Add(40 * time.Second))
	if got := freed.Load(); got != 1 {
		t.Fatalf("released while busy; want 1 got %d", got)
	}

	// Work completes.
	s.mu.Lock()
	delete(s.inflight, "/repo")
	s.mu.Unlock()

	// New idle period: arms the clock, must NOT fire immediately even though
	// wall-clock is well past the original idleSince.
	s.maybeReleaseMemory(base.Add(50 * time.Second))
	if got := freed.Load(); got != 1 {
		t.Fatalf("released without a fresh debounce after busy→idle; want 1 got %d", got)
	}
	// Fresh debounce elapses: second release.
	s.maybeReleaseMemory(base.Add(81 * time.Second))
	if got := freed.Load(); got != 2 {
		t.Fatalf("did not release after second idle debounce; want 2 got %d", got)
	}
}

// TestMaybeReleaseMemory_PendingAlgoDoesNotBlockRelease pins the corrected
// rule. This test previously asserted the OPPOSITE — that a pending downstream
// algo pass keeps the scheduler "busy" so we don't FreeOSMemory in the gap
// between an index completing and its passes running.
//
// That rule was wrong in both directions. It never protected in-flight work (a
// PENDING pass is by definition not executing), and it made the release
// starvable: a stage the gate defers keeps groupAlgoPending set for as long as
// it is blocked, so one wedged gate meant 8+ hours without a single heap
// release. It also held the arena through the 180s group-algo debounce — a
// two-and-a-half-minute idle window, against a 30s release debounce — which is
// the best moment to scavenge, not the worst.
//
// Release is now gated on workInFlightLocked only. See workPendingLocked.
func TestMaybeReleaseMemory_PendingAlgoDoesNotBlockRelease(t *testing.T) {
	var freed atomic.Int32
	s := newMemTestScheduler(10*time.Second, func() { freed.Add(1) })

	s.mu.Lock()
	s.groupAlgoPending["shared"] = true
	s.mu.Unlock()

	base := time.Now()
	s.maybeReleaseMemory(base)
	s.maybeReleaseMemory(base.Add(11 * time.Second))
	if got := freed.Load(); got != 1 {
		t.Fatalf("a merely-pending algo pass blocked the heap release; want 1 got %d", got)
	}

	// ...but once it is actually RUNNING (it holds the stage gate), the release
	// must stop: that is the case the old rule was reaching for.
	s.mu.Lock()
	s.groupAlgoPending["shared"] = false
	s.stageHolder = "group-algo:shared"
	s.mu.Unlock()
	s.maybeReleaseMemory(base.Add(20 * time.Second))
	s.maybeReleaseMemory(base.Add(40 * time.Second))
	if got := freed.Load(); got != 1 {
		t.Fatalf("released the heap under a RUNNING algo pass; want 1 got %d", got)
	}
}

// TestMemReleaseLoop_Disabled asserts MemReleaseDisabled suppresses the
// goroutine entirely (Start/Stop clean with no release).
func TestMemReleaseLoop_Disabled(t *testing.T) {
	var freed atomic.Int32
	s := New(Config{
		Workers:            1,
		MemReleaseDisabled: true,
		MemReleaseDebounce: time.Millisecond,
		FreeOSMemory:       func() { freed.Add(1) },
		Index:              func(context.Context, string, string) error { return nil },
	})
	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.Stop()
	if got := freed.Load(); got != 0 {
		t.Fatalf("disabled release still fired; want 0 got %d", got)
	}
}

// TestMemReleaseDefaults asserts New fills in a default debounce and a
// non-nil FreeOSMemory when the caller leaves them zero.
func TestMemReleaseDefaults(t *testing.T) {
	s := New(Config{Workers: 1})
	if s.cfg.MemReleaseDebounce != memReleaseDebounceDefault {
		t.Errorf("default debounce: want %v got %v", memReleaseDebounceDefault, s.cfg.MemReleaseDebounce)
	}
	if s.cfg.FreeOSMemory == nil {
		t.Error("FreeOSMemory left nil; want debug.FreeOSMemory default")
	}
}
