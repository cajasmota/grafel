package sched

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// busy_barge_5954_test.go — a live FOREGROUND BARGE must count as busy.
//
// The barge is the only signal the scheduler has for heavy work that runs
// OUTSIDE it: the rebuild path and the post-rebuild analytics batch never call
// Enqueue, so s.inflight stays empty and every other busyLocked() clause reads
// idle for the entire window. busyLocked() also gates the debounced
// FreeOSMemory release, so without this the daemon fires a stop-the-world GC
// plus a scavenge in the middle of the largest allocation it performs.

func newBargeBusyScheduler(debounce time.Duration, free func()) *Scheduler {
	return New(Config{
		Workers:            1,
		MemReleaseDebounce: debounce,
		StageGateBargeMax:  time.Hour,
		FreeOSMemory:       free,
		Index:              func(context.Context, string, string) error { return nil },
	})
}

// TestBusyLocked_LiveBargeCountsAsBusy: while foreground work is registered,
// the scheduler must not report itself idle.
func TestBusyLocked_LiveBargeCountsAsBusy(t *testing.T) {
	s := newBargeBusyScheduler(time.Minute, func() {})

	// busyLocked takes an injected clock (it reaps expired barges on the way
	// through). Hold it fixed at wall-clock now: bargeForeground stamps
	// bargeHold.since from real time, and StageGateBargeMax is an hour, so no
	// reap can fire inside this test and the barge under assertion stays live.
	now := time.Now()

	s.mu.Lock()
	idleBefore := !s.busyLocked(now)
	s.mu.Unlock()
	if !idleBefore {
		t.Fatal("precondition: a fresh scheduler with no work must read idle")
	}

	release := s.bargeForeground("analytics:acme")

	s.mu.Lock()
	busyDuring := s.busyLocked(now)
	s.mu.Unlock()
	if !busyDuring {
		t.Error("busyLocked() reported IDLE while a foreground barge was live; heavy work " +
			"that runs outside the scheduler is invisible to every other clause, so the " +
			"barge is the only thing that can make it visible")
	}

	release()

	s.mu.Lock()
	idleAfter := !s.busyLocked(now)
	s.mu.Unlock()
	if !idleAfter {
		t.Error("busyLocked() still reported BUSY after the barge was released")
	}
}

// TestMaybeReleaseMemory_DoesNotFireUnderALiveBarge: the consequence that
// actually costs something. A debounced FreeOSMemory must not fire underneath
// foreground work that is still allocating.
func TestMaybeReleaseMemory_DoesNotFireUnderALiveBarge(t *testing.T) {
	var freed atomic.Int32
	s := newBargeBusyScheduler(30*time.Second, func() { freed.Add(1) })

	release := s.bargeForeground("analytics:acme")

	base := time.Now()
	s.maybeReleaseMemory(base)
	s.maybeReleaseMemory(base.Add(31 * time.Second))
	s.maybeReleaseMemory(base.Add(120 * time.Second))
	if got := freed.Load(); got != 0 {
		t.Fatalf("FreeOSMemory fired %d time(s) while foreground work was registered; "+
			"the pages it scavenges are about to be dirtied straight back", got)
	}

	// Once the barge lifts, a fresh idle period releases as usual.
	release()
	s.maybeReleaseMemory(base.Add(200 * time.Second))
	s.maybeReleaseMemory(base.Add(240 * time.Second))
	if got := freed.Load(); got != 1 {
		t.Fatalf("FreeOSMemory did not resume after the barge lifted; want 1 got %d", got)
	}
}
