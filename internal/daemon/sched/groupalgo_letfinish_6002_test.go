package sched

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The group-algo (overlay annotation) pass is a PURE ANNOTATION over an
// already-written graph.fb: it loads finished per-repo graphs, computes
// communities/centrality over the union, and writes a separate <group>-algo.json
// overlay. Nothing the MCP structural tools answer depends on it, so it must
// never sit on the critical path of a user-awaited rebuild — and, crucially, it
// must actually COMPLETE once it starts.
//
// Historical failure mode (91 `group-algo: starting` events, 2 completions on
// the reference corpus): every link completion called scheduleGroupAlgo, which
// called cancelGroupAlgoLocked, which SIGKILLed the in-flight pass and restarted
// it from zero. On a corpus where the pass takes longer than the inter-trigger
// interval that is an infinite churn loop that never produces an overlay while
// keeping the daemon permanently "busy".
//
// These tests pin the two halves of the contract:
//  1. a re-arm arriving while a pass is IN FLIGHT must let that pass finish;
//  2. the re-arm must not be dropped — a follow-up pass must still run.

// TestScheduleGroupAlgo_InFlightPassIsNotCancelledByRearm: the core anti-churn
// guarantee. A pass that is already running must observe no cancellation when a
// fresh trigger (link completion, overlay sweep, reindex) arrives, and must run
// to completion.
func TestScheduleGroupAlgo_InFlightPassIsNotCancelledByRearm(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var cancelled atomic.Bool
	var completed atomic.Int32

	s := New(Config{
		Workers:           1,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(ctx context.Context, _ string) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				cancelled.Store(true)
				return ctx.Err()
			}
			completed.Add(1)
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("acme")
	<-entered // pass 1 is in flight and blocked

	// A burst of fresh triggers arrives mid-pass — exactly what a busy daemon
	// does on every link completion.
	for i := 0; i < 5; i++ {
		s.scheduleGroupAlgo("acme")
		time.Sleep(2 * time.Millisecond)
	}

	if cancelled.Load() {
		t.Fatal("re-arm cancelled the in-flight group-algo pass — this is the churn loop that never produces an overlay (91 starts / 2 completions)")
	}

	close(release)
	waitFor(t, 2*time.Second, func() bool { return completed.Load() >= 1 })
	if cancelled.Load() {
		t.Fatal("in-flight group-algo pass was cancelled instead of being allowed to finish")
	}
}

// TestScheduleGroupAlgo_RearmDuringPassStillProducesFollowUp: letting the
// in-flight pass finish must NOT silently drop the newer request. The overlay
// the running pass writes reflects the graph snapshot it loaded; a trigger that
// arrived after that snapshot means the overlay will be stale on completion, so
// a follow-up pass has to be armed once the current one returns.
func TestScheduleGroupAlgo_RearmDuringPassStillProducesFollowUp(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var runs atomic.Int32

	s := New(Config{
		Workers:           1,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			n := runs.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			if n == 1 {
				<-release // hold pass 1 open so the re-arm lands mid-pass
			}
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("acme")
	<-entered // pass 1 in flight

	s.scheduleGroupAlgo("acme") // newer content arrived mid-pass
	close(release)

	waitFor(t, 3*time.Second, func() bool { return runs.Load() >= 2 })
}

// TestGroupAlgoRerunPending_VisibleAsPending: a request deferred behind an
// in-flight pass must be OBSERVABLE. "My communities are missing" has to be
// diagnosable from `grafel status` / the Status RPC, so the group shows up in
// PendingAlgo for the whole time it is waiting on the running pass.
func TestGroupAlgoRerunPending_VisibleAsPending(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	s := New(Config{
		Workers:           1,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("acme")
	<-entered

	snap := s.Snapshot()
	if len(snap.GroupAlgoRunning) != 1 || snap.GroupAlgoRunning[0] != "acme" {
		t.Fatalf("an in-flight annotation pass must be visible in Snapshot.GroupAlgoRunning, got %v", snap.GroupAlgoRunning)
	}

	s.scheduleGroupAlgo("acme") // queued behind the running pass
	snap = s.Snapshot()
	found := false
	for _, g := range snap.PendingAlgo {
		if g == "acme" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a group-algo request queued behind a running pass must stay visible in PendingAlgo, got %v", snap.PendingAlgo)
	}

	close(release)
}

// TestFireGroupAlgo_LateReturnKeepsSuccessorCancel (#6001): fireGroupAlgo used
// to blind-delete s.groupAlgoCancel[group] on return, so a pass that returns
// late — after a CancelGroup cleared the map and a successor registered its own
// handle — dropped the LIVE successor's cancel func. A subsequent CancelGroup /
// Stop then had nothing to cancel and the successor ran to completion for a
// deleted group. fireLinks already guards this with a token identity check.
func TestFireGroupAlgo_LateReturnKeepsSuccessorCancel(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var passCtx atomic.Value // context.Context of pass 1

	s := New(Config{
		Workers:           1,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(ctx context.Context, _ string) error {
			passCtx.Store(ctx)
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("acme")
	<-entered // pass 1 is in flight and has registered its handle

	// A CancelGroup drains the registry, then a SUCCESSOR pass registers its own
	// live handle — the window in which the old blind delete destroyed it.
	var successorCancelled atomic.Bool
	successor := &groupAlgoPassCancel{cancel: func() { successorCancelled.Store(true) }}
	s.mu.Lock()
	delete(s.groupAlgoCancel, "acme")
	s.groupAlgoCancel["acme"] = successor
	s.mu.Unlock()

	// Pass 1 now returns LATE. fireGroupAlgo cancels its own ctx as the last
	// thing it does, strictly after the registry cleanup — so observing the ctx
	// die means the cleanup has already happened.
	close(release)
	waitFor(t, 2*time.Second, func() bool {
		ctx, _ := passCtx.Load().(context.Context)
		return ctx != nil && ctx.Err() != nil
	})

	s.mu.Lock()
	live := s.groupAlgoCancel["acme"]
	s.mu.Unlock()
	if live != successor {
		t.Fatalf("#6001: a late-returning group-algo pass dropped the live successor's cancel handle (registry now %v)", live)
	}

	s.CancelGroup("acme")
	if !successorCancelled.Load() {
		t.Fatal("#6001: CancelGroup could not reach the successor pass")
	}
}
