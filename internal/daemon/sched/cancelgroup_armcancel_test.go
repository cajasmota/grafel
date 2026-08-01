package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/indexstate"
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
// non-determinism removed. The group-algo family carries the identical hazard
// and is pinned the same way at the bottom of this file.

// armCancelHangGuard bounds the waits in this file. Every one of them is a hang
// guard on a state transition that must happen, never a latency budget: the
// assertions are all "this did / did not run", none of them "within X". Sized
// generously because a loaded windows-latest runner has to schedule a goroutine,
// fire a 1ms timer, acquire the stage token and enter a callback — a 2s wait for
// that chain is a Windows red waiting to happen, which is the whole reason this
// branch exists.
const armCancelHangGuard = 30 * time.Second

// waitForState polls cond until it holds, and fails with what the caller was
// waiting FOR when it does not. (The package's shared waitFor reports only
// "condition not met", which on a starved runner is indistinguishable from a
// dozen other things.)
func waitForState(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(armCancelHangGuard)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for: %s", armCancelHangGuard, what)
}

// waitForChan waits for ch to close, failing with what the caller expected
// rather than hanging until the package timeout dumps every goroutine.
func waitForChan(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(armCancelHangGuard):
		t.Fatalf("timed out after %s waiting for: %s", armCancelHangGuard, what)
	}
}

// armedGroupAlgo arms group's group-algo timer through the production path and
// returns the id of the timer it armed — the id that timer's AfterFunc body
// carries. A test that invokes fireGroupAlgo directly is STANDING IN for that
// body, so it must use this: calling with an id no timer ever held is not a
// faithful stand-in, and the arm check would (correctly) decline to run it.
func armedGroupAlgo(t *testing.T, s *Scheduler, group string) uint64 {
	t.Helper()
	s.scheduleGroupAlgo(group)
	s.mu.Lock()
	defer s.mu.Unlock()
	arm, ok := s.groupAlgoArm[group]
	if !ok {
		t.Fatalf("scheduleGroupAlgo(%q) armed no timer — the fixture cannot stand in for a fired timer body", group)
	}
	return arm
}

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
	waitForChan(t, "gA's link pass to start and take the heavy write-stage token", startedA)

	// gB fires while gA holds the token → it defers and re-arms a retry timer.
	s.scheduleLinks("gB")
	waitForState(t, "gB's link pass to be DEFERRED by the stage gate", func() bool {
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
	waitForState(t, "gA to release the heavy write-stage token", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.stageHolder == ""
	})
	if n := ranB.Load(); n != 0 {
		t.Fatalf("a cancelled, deferred link pass ran %d time(s) after the group was deleted", n)
	}

	// CONTROL. Everything above is a negative assertion, and with the retry armed
	// an hour out and then cancelled, ranB == 0 would also hold if gB's pass were
	// unreachable for some reason that has nothing to do with the cancel — a
	// mis-wired Links callback, a permanently held token, a stopped scheduler.
	// Re-arming gB now, with the token free, must produce exactly the run the
	// cancel suppressed.
	s.scheduleLinks("gB")
	waitForState(t, "a freshly armed gB link pass to run (control for the cancelled one)", func() bool {
		return ranB.Load() == 1
	})
}

// TestFireGroupAlgo_AfterCancelGroup_DoesNotRun is the group-algo twin of
// TestFireLinks_AfterCancelGroup_DoesNotRun, and the stakes are higher.
// runGroupAlgo is the heaviest job the daemon runs — Louvain + PageRank +
// betweenness over the group union, in a subprocess — and its ctx is rooted only
// at shutdownCtx, so a pass that escapes its group's delete has nothing left to
// cancel it: it pins cores to completion for a group that no longer exists,
// which is the exact leak CancelGroup was written to prevent.
func TestFireGroupAlgo_AfterCancelGroup_DoesNotRun(t *testing.T) {
	var ran atomic.Int32
	s := New(Config{
		Workers: 1,
		// Both an hour out, so no timer fires on its own: this test invokes the
		// body, exactly as an already-fired timer would.
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: time.Hour,
		GroupAlgoMaxWait:  time.Hour,
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			ran.Add(1)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	arm := armedGroupAlgo(t, s, "gB")
	s.CancelGroup("gB")

	// The already-fired timer body runs AFTER the delete.
	s.fireGroupAlgo("gB", arm)
	if n := ran.Load(); n != 0 {
		t.Fatalf("a group-algo pass whose group was deleted ran %d time(s) — nothing can cancel it once it starts", n)
	}
	s.mu.Lock()
	_, reArmed := s.groupAlgoTimers["gB"]
	_, reInflight := s.groupAlgoCancel["gB"]
	s.mu.Unlock()
	if reArmed || reInflight {
		t.Fatalf("a cancelled fire re-registered the deleted group (armed=%v inflight=%v)", reArmed, reInflight)
	}

	// CONTROL — without it this passes for any reason fireGroupAlgo might
	// decline to run, and would not be testing the cancel at all.
	arm2 := armedGroupAlgo(t, s, "gB")
	if arm2 == arm {
		t.Fatal("re-arm reused the cancelled arm id — the control cannot distinguish cancelled from live")
	}
	s.fireGroupAlgo("gB", arm2)
	if n := ran.Load(); n != 1 {
		t.Fatalf("control: an UNcancelled fired timer must run the pass exactly once, ran %d time(s)", n)
	}

	// groupAlgoArm must track groupAlgoTimers exactly. This is a consistency
	// assertion, not a behavioural pin: groupAlgoArm has one reader and ids never
	// repeat, so a stale entry has no observable effect beyond the disagreement
	// itself.
	s.mu.Lock()
	_, stillTimed := s.groupAlgoTimers["gB"]
	_, stillArm := s.groupAlgoArm["gB"]
	s.mu.Unlock()
	if stillTimed != stillArm {
		t.Fatalf("groupAlgoArm/groupAlgoTimers disagree after the pass ran (timer=%v arm=%v)", stillTimed, stillArm)
	}
}

// TestFireGroupAlgo_AfterCancelGroup_DeferredArmBalancesIndexstate covers the
// gate-deferred arm, where the escape did more than waste CPU. The deferred
// branch of fireGroupAlgo takes an indexstate.GroupAlgoBegin (so a DEFERRED pass
// stays visible to grafel_stats). cancelGroupAlgoLocked releases that pair on
// delete — and then the already-fired body took a FRESH one that nothing would
// ever release: the process-global counter sits at +1 forever and every
// grafel_stats consumer sees a group-algo pass permanently in flight.
func TestFireGroupAlgo_AfterCancelGroup_DeferredArmBalancesIndexstate(t *testing.T) {
	var ran atomic.Int32
	s := New(Config{
		Workers:           1,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: time.Hour,
		GroupAlgoMaxWait:  time.Hour,
		StageGateRetry:    time.Hour,
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			ran.Add(1)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	// indexstate is PROCESS-GLOBAL: a t.Fatal mid-test would otherwise leak this
	// test's hold into every later test in the binary.
	t.Cleanup(func() {
		s.mu.Lock()
		delete(s.inflight, "/busy")
		s.markGroupAlgoDeferredLocked("gB", false)
		s.mu.Unlock()
	})
	before := indexstate.Get().GroupAlgoInFlight

	// An index batch in flight makes the gate refuse, so the fire DEFERS.
	s.mu.Lock()
	s.inflight["/busy"] = 1
	s.mu.Unlock()

	s.fireGroupAlgo("gB", armedGroupAlgo(t, s, "gB"))
	if got := indexstate.Get().GroupAlgoInFlight; got != before+1 {
		t.Fatalf("a DEFERRED pass must hold exactly one indexstate count (got %d, was %d) — fixture cannot detect an unbalanced re-take", got, before)
	}
	s.mu.Lock()
	deferredArm, armed := s.groupAlgoArm["gB"]
	s.mu.Unlock()
	if !armed {
		t.Fatal("a deferred pass recorded no retry arm id — fixture cannot exhibit the race it claims to guard")
	}

	// The delete releases the deferred hold.
	s.CancelGroup("gB")
	if got := indexstate.Get().GroupAlgoInFlight; got != before {
		t.Fatalf("CancelGroup must release the deferred pass's indexstate hold (got %d, want %d)", got, before)
	}

	// The retry timer had already fired when the delete landed.
	s.fireGroupAlgo("gB", deferredArm)

	if got := indexstate.Get().GroupAlgoInFlight; got != before {
		t.Fatalf("a cancelled deferred fire re-took an indexstate hold nobody will release (got %d, want %d) — grafel_stats reports a group-algo pass in flight forever", got, before)
	}
	s.mu.Lock()
	_, reArmed := s.groupAlgoTimers["gB"]
	_, reDeferred := s.stageDeferSince["group-algo:gB"]
	stillMarked := s.groupAlgoDeferred["gB"]
	s.mu.Unlock()
	if reArmed || reDeferred || stillMarked {
		t.Fatalf("a cancelled deferred fire re-registered the deleted group (armed=%v deferred=%v marked=%v)", reArmed, reDeferred, stillMarked)
	}
	if n := ran.Load(); n != 0 {
		t.Fatalf("a cancelled, deferred group-algo pass ran %d time(s) after the group was deleted", n)
	}

	// CONTROL: with the gate free again, a freshly armed pass must run — so the
	// zeroes above are the cancel's doing, not an unreachable callback.
	s.mu.Lock()
	delete(s.inflight, "/busy")
	s.mu.Unlock()
	s.fireGroupAlgo("gB", armedGroupAlgo(t, s, "gB"))
	if n := ran.Load(); n != 1 {
		t.Fatalf("control: an UNcancelled fired timer must run the pass exactly once, ran %d time(s)", n)
	}
	if got := indexstate.Get().GroupAlgoInFlight; got != before {
		t.Fatalf("indexstate did not return to its baseline after the control pass (got %d, want %d)", got, before)
	}
}
