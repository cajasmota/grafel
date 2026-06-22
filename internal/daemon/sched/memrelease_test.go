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

// TestMaybeReleaseMemory_PendingAlgoCountsAsBusy asserts a pending downstream
// algo pass keeps the scheduler "busy" so we don't FreeOSMemory in the gap
// between an index completing and its algo/link passes running.
func TestMaybeReleaseMemory_PendingAlgoCountsAsBusy(t *testing.T) {
	var freed atomic.Int32
	s := newMemTestScheduler(10*time.Second, func() { freed.Add(1) })

	s.mu.Lock()
	s.groupAlgoPending["shared"] = true
	s.mu.Unlock()

	base := time.Now()
	s.maybeReleaseMemory(base)
	s.maybeReleaseMemory(base.Add(20 * time.Second))
	if got := freed.Load(); got != 0 {
		t.Fatalf("released while an algo pass was pending; want 0 got %d", got)
	}

	// Algo pass clears → now genuinely idle.
	s.mu.Lock()
	s.groupAlgoPending["shared"] = false
	s.mu.Unlock()
	s.maybeReleaseMemory(base.Add(30 * time.Second)) // arms fresh clock
	s.maybeReleaseMemory(base.Add(41 * time.Second)) // fires
	if got := freed.Load(); got != 1 {
		t.Fatalf("did not release once truly idle; want 1 got %d", got)
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
