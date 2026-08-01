package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The windows-latest leg of the v0.2.0 release gate reported
// `TestCancelGroup_DeferredLinkPassNeverRuns: a cancelled, deferred link pass
// ran 1 time(s) after the group was deleted`. That test drives the window
// through real timers, so it only reports the defect on a host slow enough to
// open it. The two tests here drive the SAME window deterministically.
//
// The window: CancelGroup stops the group's debounce/retry timer with
// Timer.Stop(), which reports false once the timer has ALREADY fired — the
// AfterFunc body (fireLinks) is then in flight on the timer goroutine and is
// not reachable by anything CancelGroup does. Pre-fix that body went on to
// acquire the heavy write-stage token and run the deleted group's link pass;
// on the deferred path it re-armed a fresh retry timer and re-registered the
// stage deferral, resurrecting both map entries CancelGroup had just dropped.
//
// Calling s.fireLinks(group, arm) directly IS that in-flight body: the timer is
// armed as time.AfterFunc(d, func() { s.fireLinks(group, arm) }), so this is
// the residual work a failed Stop() leaves behind, with the scheduling
// non-determinism removed.

// TestFireLinks_AfterCancelGroup_DoesNotRun covers the plain (non-deferred)
// arm: a debounce timer that fired just before the group was deleted.
func TestFireLinks_AfterCancelGroup_DoesNotRun(t *testing.T) {
	var ran atomic.Int32
	s := New(Config{
		Workers: 1,
		// Long enough that the timer never fires on its own — this test fires
		// the body itself, exactly as the already-fired timer would.
		LinkDebounce: time.Hour,
		Links: func(_ context.Context, _ string) error {
			ran.Add(1)
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleLinks("gB")
	s.mu.Lock()
	arm, armed := s.linkArm["gB"]
	s.mu.Unlock()
	if !armed {
		t.Fatal("scheduleLinks recorded no arm id — fixture cannot exhibit the cancel/fire race it claims to guard")
	}

	s.CancelGroup("gB")

	// The already-fired timer body runs AFTER the delete.
	s.fireLinks("gB", arm)
	if n := ran.Load(); n != 0 {
		t.Fatalf("a link pass whose group was deleted ran %d time(s) — CancelGroup's Timer.Stop() lost the race with the firing timer", n)
	}
	// It must also not resurrect the group's scheduling state.
	s.mu.Lock()
	_, reArmed := s.linkTimers["gB"]
	_, reDeferred := s.stageDeferSince["links:gB"]
	s.mu.Unlock()
	if reArmed || reDeferred {
		t.Fatalf("a cancelled fire re-registered the deleted group (armed=%v deferred=%v)", reArmed, reDeferred)
	}

	// CONTROL — without this the test passes for any reason fireLinks might
	// decline to run (nil Links, stopped scheduler, a permanently blocked
	// stage gate), i.e. it would not be checking the cancel at all.
	s.scheduleLinks("gB")
	s.mu.Lock()
	arm2 := s.linkArm["gB"]
	s.mu.Unlock()
	if arm2 == arm {
		t.Fatal("re-arm reused the cancelled arm id — the control cannot distinguish cancelled from live")
	}
	s.fireLinks("gB", arm2)
	if n := ran.Load(); n != 1 {
		t.Fatalf("control: an UNcancelled fired timer must run the pass exactly once, ran %d time(s)", n)
	}

	// linkArm must track linkTimers exactly: an id outliving the timer it names
	// is an entry that grows per group and can never be matched again, and it
	// makes "is this group armed?" answerable two ways that disagree.
	s.mu.Lock()
	_, stillTimed := s.linkTimers["gB"]
	_, stillArm := s.linkArm["gB"]
	s.mu.Unlock()
	if stillTimed != stillArm {
		t.Fatalf("linkArm/linkTimers disagree after the pass ran (timer=%v arm=%v) — the arm id must be dropped with the timer", stillTimed, stillArm)
	}
}

// TestFireLinks_AfterCancelGroup_DeferredArmDoesNotRun covers the arm the CI
// failure actually hit: the retry timer of a pass DEFERRED by the heavy
// write-stage gate. gA holds the token, so an unguarded fire would not run the
// pass immediately — it would re-arm a retry timer and re-register the stage
// deferral for the deleted group, and run it as soon as gA released.
func TestFireLinks_AfterCancelGroup_DeferredArmDoesNotRun(t *testing.T) {
	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	var ranB atomic.Int32

	s := New(Config{
		Workers: 1,
		// The debounce is short so gB's first fire really goes through the timer
		// and really defers on the gate. The RETRY it then arms is an hour out,
		// so the only thing that fires it is this test — the fired-timer body is
		// invoked deterministically below rather than raced for.
		LinkDebounce:   time.Millisecond,
		StageGateRetry: time.Hour,
		Links: func(_ context.Context, group string) error {
			if group == "gB" {
				ranB.Add(1)
				return nil
			}
			close(startedA)
			<-releaseA
			return nil
		},
		MemReleaseDisabled: true,
	})
	var releaseOnce sync.Once
	releaseAFn := func() { releaseOnce.Do(func() { close(releaseA) }) }
	s.Start()
	defer func() { releaseAFn(); s.Stop() }()

	s.scheduleLinks("gA")
	<-startedA

	// gB fires while gA holds the token → it defers and re-arms a retry timer.
	s.scheduleLinks("gB")
	waitFor(t, 2*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, deferred := s.stageDeferSince["links:gB"]
		return deferred
	})
	s.mu.Lock()
	retryArm, armed := s.linkArm["gB"]
	s.mu.Unlock()
	if !armed {
		t.Fatal("a deferred pass recorded no retry arm id — fixture cannot exhibit the race it claims to guard")
	}
	if n := ranB.Load(); n != 0 {
		t.Fatalf("gB's link pass ran %d time(s) while gA held the stage token", n)
	}

	s.CancelGroup("gB")

	// The retry timer had already fired when the delete landed.
	s.fireLinks("gB", retryArm)

	s.mu.Lock()
	_, reArmed := s.linkTimers["gB"]
	_, reDeferred := s.stageDeferSince["links:gB"]
	s.mu.Unlock()
	if reArmed || reDeferred {
		t.Fatalf("a cancelled deferred fire re-registered the deleted group (armed=%v deferred=%v) — it will run as soon as the token frees", reArmed, reDeferred)
	}

	// Free the token: nothing must be waiting on it for gB.
	releaseAFn()
	waitFor(t, 2*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.stageHolder == ""
	})
	if n := ranB.Load(); n != 0 {
		t.Fatalf("a cancelled, deferred link pass ran %d time(s) after the group was deleted", n)
	}
}
