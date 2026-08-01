package sched

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The #6068 hazard one hop UPSTREAM of the two sites pinned in
// cancelgroup_rearmgap_6068_test.go.
//
// runIndex's success path chains scheduleLinks for every group the repo belongs
// to. That call happens after a multi-MINUTE index, outside every lock, and —
// before this file — with no generation to re-present. So:
//
//  1. repo R is indexing for group gB (minutes).
//  2. DeleteGroup(gB) → cancelGroupWork → CancelGroup("gB"). Generation 0→1,
//     link timers dropped, group-algo torn down.
//  3. runIndex completes successfully and chains scheduleLinks("gB") — a LIVE
//     link timer for a group that no longer exists.
//  4. That timer fires; fireLinks captures gen = groupGenLocked("gB") = 1, the
//     CURRENT value, so the #6068 guard on the chained group-algo arm sees
//     1 == 1 and arms.
//  5. ~180s later Louvain + PageRank + betweenness run for a deleted group,
//     under a ctx rooted only at shutdownCtx, with nothing left to cancel it.
//
// The guard added for runLinks cannot see this: by the time fireLinks runs, the
// delete is in the PAST and the generation it reads is already the post-delete
// one. The generation has to be captured at the hold that dequeued the index —
// the hold that AUTHORISED this whole chain — and threaded through.
//
// Reachable, not theoretical, for two reasons:
//   - CancelGroup deliberately leaves running any reindex whose repo is still
//     referenced by a surviving group (repoBelongsOnlyToLocked). That reindex
//     completes normally and chains scheduleLinks for every group
//     GroupsForRepo returns.
//   - service.go's DeleteGroup calls cancelGroupWork BEFORE registry teardown,
//     so there is a real window in which GroupsForRepo(repoPath) still returns
//     the deleted group.
//
// OVER-SUPPRESSION is the dangerous failure mode for the fix, not the bug: a
// guard that silently drops link (and therefore group-algo) passes for LIVE
// groups means missing analysis, which is worse than the leak because it is
// invisible. Every test below pairs its negative with a live sibling group that
// must still arm — in the SAME GroupsForRepo loop, on the SAME index — plus a
// fresh-trigger control.

// indexRearmScheduler builds a scheduler whose repo belongs to groups and whose
// Index body runs onIndex (the seam that places a delete DURING the index).
// Debounces are an hour out so nothing fires on its own: the assertions read
// whether a timer was ARMED, never whether it ran.
func indexRearmScheduler(groups []string, onIndex func()) (*Scheduler, *atomic.Int32, *atomic.Int32) {
	var linksRan atomic.Int32
	var algoRan atomic.Int32
	s := New(Config{
		Workers:           1,
		LinkDebounce:      time.Hour,
		GroupAlgoDebounce: time.Hour,
		GroupAlgoMaxWait:  time.Hour,
		Index: func(_ context.Context, _ string, _ string) error {
			if onIndex != nil {
				onIndex()
			}
			return nil
		},
		Links: func(_ context.Context, _ string) error {
			linksRan.Add(1)
			return nil
		},
		GroupAlgo: func(_ context.Context, _ string) error {
			algoRan.Add(1)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return groups },
		MemReleaseDisabled: true,
	})
	return s, &linksRan, &algoRan
}

// linkArmed reports whether group currently holds link-timer state of any kind.
// All three are checked because the pre-fix defect resurrects all three, and a
// fix that dropped only the timer while leaving linkPending set would still
// re-arm on the next retry.
func linkArmed(s *Scheduler, group string) (timer, arm, pending bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, timer = s.linkTimers[group]
	_, arm = s.linkArm[group]
	pending = s.linkPending[group]
	return
}

// TestRunIndex_ChainedLinksAcrossCancelGroup_DoesNotArm is the primary
// interleaving: the delete lands WHILE the index is running, i.e. after the
// hold that dequeued the job and long before scheduleLinks is chained.
//
// runIndex is invoked directly from the test goroutine — the #6067 convention.
// It is the worker-loop body (`s.runIndex(tok)`), so calling it is exactly the
// work a dequeued job performs, with the scheduling non-determinism removed.
func TestRunIndex_ChainedLinksAcrossCancelGroup_DoesNotArm(t *testing.T) {
	var s *Scheduler
	// deleted fires the delete from INSIDE the index body. That is the window:
	// the authorising hold has been released, the index is in flight, and the
	// GroupsForRepo loop has not run yet.
	deleted := &atomic.Bool{}
	s, linksRan, algoRan := indexRearmScheduler([]string{"gA", "gB"}, func() {
		if deleted.Swap(true) {
			return // only the first index deletes; the control below must not
		}
		s.CancelGroup("gB")
	})
	s.Start()
	defer s.Stop()

	s.runIndex(jobToken{repoPath: "/repo-a", ref: "main", commit: "c0"})

	if !deleted.Load() {
		t.Fatal("the index body never ran — the fixture never opened the window it claims to guard, and every assertion below is vacuous")
	}

	timer, arm, pending := linkArmed(s, "gB")
	if timer || arm || pending {
		t.Fatalf("a reindex that completed AFTER its group was deleted armed a live link timer for it (timer=%v arm=%v pending=%v) — that timer fires, fireLinks reads the post-delete generation, and the chained group-algo pass runs for a group that is gone", timer, arm, pending)
	}

	// OVER-SUPPRESSION GUARD. gA was never deleted and is in the SAME
	// GroupsForRepo loop, on the SAME index, one iteration away from gB. If the
	// fix suppresses this too, the negative above still holds and the bug is
	// traded for a silent, invisible loss of every downstream link + group-algo
	// pass for live groups.
	if ctlTimer, ctlArm, ctlPending := linkArmed(s, "gA"); !ctlTimer || !ctlArm || !ctlPending {
		t.Fatalf("the LIVE sibling group gA was suppressed by a delete of gB (timer=%v arm=%v pending=%v) — over-suppression drops analysis for groups that still exist and does so silently", ctlTimer, ctlArm, ctlPending)
	}

	// Nothing downstream may have run: the debounces are an hour out, so a
	// non-zero count here means an arm fired immediately, not that it was armed.
	if n := linksRan.Load(); n != 0 {
		t.Fatalf("no link pass should have RUN yet (debounce is an hour), ran %d", n)
	}
	if n := algoRan.Load(); n != 0 {
		t.Fatalf("no group-algo pass should have RUN yet, ran %d", n)
	}

	// CONTROL: a second index of the same repo, with no delete in flight, must
	// arm gB again. The cancel generation gates CONTINUATIONS of work the delete
	// already cancelled — it must never gate the group name forever.
	s.mu.Lock()
	prevArm := s.linkArm["gA"]
	s.mu.Unlock()

	s.runIndex(jobToken{repoPath: "/repo-a", ref: "main", commit: "c1"})

	if timer, arm, pending = linkArmed(s, "gB"); !timer || !arm || !pending {
		t.Fatalf("control: a fresh index after the delete must arm gB's link timer (timer=%v arm=%v pending=%v) — without this the test passes even when the guard has disabled the group name permanently", timer, arm, pending)
	}
	s.mu.Lock()
	newArm := s.linkArm["gA"]
	s.mu.Unlock()
	if newArm == prevArm {
		t.Fatal("control: gA's link timer was not re-armed by the second index — the fixture cannot distinguish a fresh arm from the stale one left by the first")
	}
}

// TestRunIndex_ChainedLinksCancelInGap_DoesNotArm pins the NARROWEST form of the
// same window: the index body has already returned, and the delete lands in the
// handful of instructions between the success log and the scheduleLinks call.
// Driven through rearmGapHook rather than raced for, so the delete is placed
// exactly there.
func TestRunIndex_ChainedLinksCancelInGap_DoesNotArm(t *testing.T) {
	s, _, algoRan := indexRearmScheduler([]string{"gA", "gB"}, nil)
	s.Start()
	defer s.Stop()

	entered := installRearmGapCancel(s, "index-done", "gB")

	s.runIndex(jobToken{repoPath: "/repo-a", ref: "main", commit: "c0"})

	if !entered.Load() {
		t.Fatal("runIndex never entered the pre-scheduleLinks gap — the fixture cannot exhibit the window it claims to guard")
	}

	if timer, arm, pending := linkArmed(s, "gB"); timer || arm || pending {
		t.Fatalf("a delete landing in the gap between a completed index and its chained scheduleLinks still armed a live link timer (timer=%v arm=%v pending=%v)", timer, arm, pending)
	}
	// Over-suppression guard again: gA is armed by the SAME loop.
	if timer, arm, pending := linkArmed(s, "gA"); !timer || !arm || !pending {
		t.Fatalf("the live sibling gA was suppressed (timer=%v arm=%v pending=%v)", timer, arm, pending)
	}
	if n := algoRan.Load(); n != 0 {
		t.Fatalf("no group-algo pass should have run, ran %d", n)
	}
}

// TestRunIndex_GroupJoiningDuringIndexStillArms is the third over-suppression
// guard, and the one a plausible wrong implementation trips: a group that was
// NOT a member when the index started has no captured generation, so the arm
// must treat it as a fresh trigger. Defaulting the missing entry to 0 instead
// (the zero value) looks harmless and is not — for any group name that had ever
// been cancelled, groupCancelSeq is non-zero, 0 never matches, and the first
// link pass of that newly-added repo/group pair is dropped silently and forever.
func TestRunIndex_GroupJoiningDuringIndexStillArms(t *testing.T) {
	// gJoin is deleted up front, so its generation is 1, not the zero value.
	// Membership then changes mid-index: gJoin is absent at the authorising hold
	// and present at the chained arm.
	var s *Scheduler
	joined := &atomic.Bool{}
	s, _, _ = indexRearmScheduler(nil, nil)
	s.cfg.GroupsForRepo = func(_ string) []string {
		if joined.Load() {
			return []string{"gJoin"}
		}
		return nil
	}
	s.cfg.Index = func(_ context.Context, _ string, _ string) error {
		joined.Store(true) // gJoin joins WHILE the index runs
		return nil
	}
	s.Start()
	defer s.Stop()

	s.CancelGroup("gJoin") // some earlier delete moved gJoin's generation off 0

	s.runIndex(jobToken{repoPath: "/repo-a", ref: "main", commit: "c0"})

	if !joined.Load() {
		t.Fatal("the index body never ran — membership never changed, so the fixture is not exercising the join-during-index path")
	}
	if timer, arm, pending := linkArmed(s, "gJoin"); !timer || !arm || !pending {
		t.Fatalf("a group that JOINED during the index was suppressed (timer=%v arm=%v pending=%v) — it has no captured generation because it is not a continuation of anything, and gating it drops the first link pass of every newly-added repo/group pair, invisibly", timer, arm, pending)
	}
}

// TestScheduleLinks_FreshRequestAfterDeleteStillArms is the standalone
// counterweight for the link timer, mirroring the group-algo one. A group that
// is deleted and then re-registered — or any fresh trigger arriving afterwards —
// must schedule normally. Without this, a guard that gated on the group name
// rather than on the generation captured by a continuation would pass every
// other test in this file.
func TestScheduleLinks_FreshRequestAfterDeleteStillArms(t *testing.T) {
	s, _, _ := indexRearmScheduler([]string{"gB"}, nil)
	s.Start()
	defer s.Stop()

	s.scheduleLinks("gB")
	s.CancelGroup("gB")
	if timer, arm, pending := linkArmed(s, "gB"); timer || arm || pending {
		t.Fatalf("CancelGroup left link state behind (timer=%v arm=%v pending=%v) — fixture cannot tell a suppressed arm from a surviving one", timer, arm, pending)
	}

	s.scheduleLinks("gB")
	if timer, arm, pending := linkArmed(s, "gB"); !timer || !arm || !pending {
		t.Fatalf("a FRESH scheduleLinks after a delete must arm (timer=%v arm=%v pending=%v) — the cancel generation must gate continuations only, not the group name forever", timer, arm, pending)
	}
}
