package sched

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// #6067 made the arm id order a group delete against a timer that had ALREADY
// FIRED. It structurally cannot order a delete against a re-arm taken later:
// both fireGroupAlgo's rerun completion and runLinks' chained arm decide to
// re-arm while holding s.mu (or while holding the pass), then release and call
// scheduleGroupAlgo. A CancelGroup landing in that gap has already run — it
// dropped the arm id and cancelled what existed — so the arm that follows
// allocates a FRESH, legitimately-live id for a group that is gone. ~180s later
// runGroupAlgo (Louvain + PageRank + betweenness over the group union, in a
// subprocess, under a ctx rooted only at shutdownCtx) runs for a deleted group,
// with nothing left that can cancel it.
//
// The gap is microseconds wide, so these tests do not race for it: they install
// rearmGapHook, the test-only seam invoked inside the gap, and cancel from
// there. That places the delete exactly where the hazard lives. Both bodies are
// invoked directly from the test goroutine (the #6067 convention — the body is
// the residual work the timer leaves behind), so when the call returns the
// re-arm decision has already been taken and the state can be read without a
// single sleep.

// installRearmGapCancel makes the FIRST entry into the re-arm gap at `site`
// delete `group`, and reports whether the gap was ever entered. A test whose
// entered flag stays false never reached the window and is asserting nothing.
func installRearmGapCancel(s *Scheduler, site, group string) (entered *atomic.Bool) {
	entered = &atomic.Bool{}
	var fn func(string, string)
	fn = func(gotSite, gotGroup string) {
		if gotSite != site || gotGroup != group {
			return
		}
		if entered.Swap(true) {
			return // only the first pass through the gap gets deleted
		}
		s.CancelGroup(group)
	}
	s.rearmGapHook.Store(&fn)
	return entered
}

// TestFireGroupAlgo_RerunRearmAcrossCancelGroup_DoesNotArm drives the rerun
// completion path: a pass finishes, a recompute was requested while it ran, and
// the group is deleted in the gap between releasing s.mu and re-arming.
func TestFireGroupAlgo_RerunRearmAcrossCancelGroup_DoesNotArm(t *testing.T) {
	var ran atomic.Int32
	var s *Scheduler
	// requestRerun makes the RUNNING pass ask for a follow-up, which is the only
	// way to set groupAlgoRerun — this is what a link completion landing
	// mid-pass does. Guarded so only the first pass requests one, or the control
	// phase would chain forever.
	var requested atomic.Bool
	s = New(Config{
		Workers: 1,
		// An hour out: no timer fires on its own. The bodies below ARE the
		// timer bodies, invoked directly.
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: time.Hour,
		GroupAlgoMaxWait:  time.Hour,
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, group string) error {
			ran.Add(1)
			if !requested.Swap(true) {
				s.scheduleGroupAlgo(group) // recompute requested mid-pass
			}
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	entered := installRearmGapCancel(s, "group-algo-rerun", "gB")

	s.fireGroupAlgo("gB", armedGroupAlgo(t, s, "gB"))

	if !entered.Load() {
		t.Fatal("the completion path never entered the re-arm gap — the fixture cannot exhibit the window it claims to guard")
	}
	if n := ran.Load(); n != 1 {
		t.Fatalf("the first (pre-delete) pass must run exactly once, ran %d time(s) — fixture is not driving the rerun path", n)
	}

	s.mu.Lock()
	_, reArmed := s.groupAlgoTimers["gB"]
	_, reArmID := s.groupAlgoArm["gB"]
	pending := s.groupAlgoPending["gB"]
	s.mu.Unlock()
	if reArmed || reArmID || pending {
		t.Fatalf("the rerun re-armed a group deleted in the gap (timer=%v arm=%v pending=%v) — the pass will run for a group that is gone, and nothing can cancel it", reArmed, reArmID, pending)
	}

	// CONTROL. Every assertion above is a negative, and all of them would also
	// hold if the rerun path were simply unreachable in this fixture — a
	// dropped rerun flag, a nil GroupAlgo, a stopped scheduler. Run the SAME
	// path with no delete in the gap: it must re-arm.
	var fn func(string, string)
	fn = func(_, _ string) {}
	s.rearmGapHook.Store(&fn)
	requested.Store(false)

	s.fireGroupAlgo("gB", armedGroupAlgo(t, s, "gB"))
	if n := ran.Load(); n != 2 {
		t.Fatalf("control: the second pass must run, ran total %d time(s)", n)
	}
	s.mu.Lock()
	_, ctlArmed := s.groupAlgoTimers["gB"]
	s.mu.Unlock()
	if !ctlArmed {
		t.Fatal("control: an UNcancelled rerun must re-arm the group-algo timer — without this the test passes even when the rerun path is dead")
	}
}

// TestRunLinks_ChainedGroupAlgoAcrossCancelGroup_DoesNotArm drives the second
// call site. Here the gap is not microseconds: cfg.Links runs for minutes
// between the hold that authorised the pass and the chained arm, so a delete
// anywhere in that span must suppress it.
func TestRunLinks_ChainedGroupAlgoAcrossCancelGroup_DoesNotArm(t *testing.T) {
	var algoRan atomic.Int32
	var linksRan atomic.Int32
	s := New(Config{
		Workers:           1,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: time.Hour,
		GroupAlgoMaxWait:  time.Hour,
		Links: func(_ context.Context, _ string) error {
			linksRan.Add(1)
			return nil
		},
		GroupAlgo: func(_ context.Context, _ string) error {
			algoRan.Add(1)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	entered := installRearmGapCancel(s, "links-done", "gB")

	s.scheduleLinks("gB")
	s.mu.Lock()
	arm, armed := s.linkArm["gB"]
	s.mu.Unlock()
	if !armed {
		t.Fatal("scheduleLinks recorded no arm id — fixture cannot stand in for a fired timer body")
	}
	s.fireLinks("gB", arm)

	if !entered.Load() {
		t.Fatal("the link pass never reached the chained group-algo arm — the fixture cannot exhibit the window it claims to guard")
	}
	if n := linksRan.Load(); n != 1 {
		t.Fatalf("the link pass must have run exactly once, ran %d time(s)", n)
	}

	s.mu.Lock()
	_, reArmed := s.groupAlgoTimers["gB"]
	_, reArmID := s.groupAlgoArm["gB"]
	pending := s.groupAlgoPending["gB"]
	s.mu.Unlock()
	if reArmed || reArmID || pending {
		t.Fatalf("a link pass whose group was deleted mid-pass still armed the group-algo pass (timer=%v arm=%v pending=%v)", reArmed, reArmID, pending)
	}
	if n := algoRan.Load(); n != 0 {
		t.Fatalf("a group-algo pass ran %d time(s) for a deleted group", n)
	}

	// CONTROL: the same path with no delete in the gap must arm.
	var fn func(string, string)
	fn = func(_, _ string) {}
	s.rearmGapHook.Store(&fn)

	s.scheduleLinks("gB")
	s.mu.Lock()
	arm2 := s.linkArm["gB"]
	s.mu.Unlock()
	if arm2 == arm {
		t.Fatal("re-arm reused the cancelled arm id — the control cannot distinguish cancelled from live")
	}
	s.fireLinks("gB", arm2)
	s.mu.Lock()
	_, ctlArmed := s.groupAlgoTimers["gB"]
	s.mu.Unlock()
	if !ctlArmed {
		t.Fatal("control: a successful link pass must chain the group-algo arm — without this the test passes even when Links never reaches the chain")
	}
}

// TestScheduleGroupAlgo_FreshRequestAfterDeleteStillArms is the counterweight.
// The generation check must reject only CONTINUATIONS of work authorised before
// the delete; a group that is deleted and then re-registered (or any fresh
// trigger arriving afterwards) must still schedule normally. Without this, a
// too-eager guard would silently disable the group-algo pass for every group
// name that had ever been deleted, and every other test here would still pass.
func TestScheduleGroupAlgo_FreshRequestAfterDeleteStillArms(t *testing.T) {
	s := New(Config{
		Workers:            1,
		LinkDebounce:       time.Hour,
		GroupAlgoDebounce:  time.Hour,
		GroupAlgoMaxWait:   time.Hour,
		Links:              func(_ context.Context, _ string) error { return nil },
		GroupAlgo:          func(_ context.Context, _ string) error { return nil },
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("gB")
	s.CancelGroup("gB")
	s.mu.Lock()
	_, armedAfterCancel := s.groupAlgoTimers["gB"]
	s.mu.Unlock()
	if armedAfterCancel {
		t.Fatal("CancelGroup left a group-algo timer armed — fixture cannot tell a suppressed arm from a surviving one")
	}

	s.scheduleGroupAlgo("gB")
	s.mu.Lock()
	_, rearmed := s.groupAlgoTimers["gB"]
	s.mu.Unlock()
	if !rearmed {
		t.Fatal("a FRESH scheduleGroupAlgo after a delete must arm — the cancel generation must gate continuations only, not the group name forever")
	}
}
