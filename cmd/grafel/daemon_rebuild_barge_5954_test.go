package main

// daemon_rebuild_barge_5954_test.go — the WIRING half of the #5954 foreground
// barge.
//
// internal/daemon/sched owns the barge mechanism and tests it directly. What
// those tests cannot pin is the thing that actually went wrong in production:
// the rebuild path never told the scheduler it existed. `grafel reset` and the
// rebuild RPC index through makeDaemonRebuildFunc → indexFn → Index(...)
// DIRECTLY — they never call scheduler.Enqueue, so s.inflight stays empty, the
// #5993 stage gate reads an idle machine, and a background group-algo pass
// starts alongside a multi-GB foreground index (measured: peak 4258 → 5430 MB,
// `index child 2979 MB + group-algo 1618 MB` co-resident at the peak instant).
//
// So these tests assert on daemonRebuildFuncCore itself: the barge is HELD
// while the rebuild runs (covering both the per-repo index batch and the
// cross-repo link pass), and it is RELEASED on every exit path. Deleting the
// `defer releaseBarge()` in daemonRebuildFuncCore must fail the release tests;
// deleting the acquisition must fail the held test.

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// startBargeObserverScheduler starts a minimal real scheduler so the process
// bridge (sched.BargeForeground) resolves to something observable, and returns
// a func reporting how many foreground barge holds are live right now.
//
// It counts ALL live holds. Use startRebuildBargeObserver in tests whose
// subject is the rebuild's own hold: the post-rebuild analytics batch takes its
// own barge from a goroutine that outlives daemonRebuildFuncCore (#5954), so an
// unscoped count is both wrong and racy for those assertions.
func startBargeObserverScheduler(t *testing.T) func() int {
	names := startBargeObserverSchedulerNames(t)
	return func() int { return len(names()) }
}

// startBargeObserverSchedulerNames is the same observer, returning the live
// holder names so a caller can scope its assertion to one of them.
func startBargeObserverSchedulerNames(t *testing.T) func() []string {
	t.Helper()
	s := sched.New(sched.Config{
		Workers:            1,
		LinkDebounce:       time.Hour,
		GroupAlgoDebounce:  time.Hour,
		StageGateHoldMax:   time.Hour,
		StageGateBargeMax:  time.Hour,
		Index:              func(_ context.Context, _ string, _ string) error { return nil },
		Links:              func(_ context.Context, _ string) error { return nil },
		GroupAlgo:          func(_ context.Context, _ string) error { return nil },
		GroupsForRepo:      func(_ string) []string { return nil },
		MemReleaseDisabled: true,
	})
	s.Start()
	t.Cleanup(s.Stop)
	return func() []string { return s.Snapshot().Barging }
}

// startRebuildBargeObserver is startBargeObserverScheduler scoped to the
// REBUILD's own foreground hold. The three tests below assert that
// daemonRebuildFuncCore acquires and unwinds its barge; the analytics batch's
// independent, asynchronous hold is not their subject and must not make them
// flake. Deleting the rebuild's acquisition still fails them.
func startRebuildBargeObserver(t *testing.T) func() int {
	t.Helper()
	all := startBargeObserverSchedulerNames(t)
	return func() int {
		n := 0
		for _, name := range all() {
			if strings.HasPrefix(name, "rebuild:") {
				n++
			}
		}
		return n
	}
}

// TestRebuild_HoldsForegroundBargeAcrossIndexAndLinks: while daemonRebuildFuncCore
// is inside the per-repo index batch AND inside the cross-repo link pass, a
// foreground barge must be registered with the scheduler. Both phases are
// sampled because both allocate a large slice of the group graph and both run
// entirely outside the scheduler's view.
func TestRebuild_HoldsForegroundBargeAcrossIndexAndLinks(t *testing.T) {
	group := setupTestGroup(t, "barge-group", []string{"first", "second"})
	barges := startRebuildBargeObserver(t)

	var duringIndex, duringLinks atomic.Int32
	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		if n := barges(); int32(n) > duringIndex.Load() {
			duringIndex.Store(int32(n))
		}
		return nil
	}
	linksFn := func(_ context.Context, _ string) error {
		duringLinks.Store(int32(barges()))
		return nil
	}

	if _, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, linksFn); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if duringIndex.Load() < 1 {
		t.Errorf("no foreground barge registered during the per-repo index batch: " +
			"a rebuild never populates s.inflight, so without the barge the stage gate sees an idle machine " +
			"and lets a background group-algo pass go co-resident with a multi-GB index child (#5954)")
	}
	if duringLinks.Load() < 1 {
		t.Errorf("no foreground barge registered during the cross-repo link pass: " +
			"the rebuild's own link pass runs outside the scheduler and was the SCOPE LIMIT flagged in #5993")
	}
	if n := barges(); n != 0 {
		t.Errorf("barge count = %d after a successful rebuild returned; want 0", n)
	}
}

// TestRebuild_ReleasesForegroundBargeOnError: no wedge. A rebuild that fails
// early (unknown group — returns before any repo is touched) must still leave
// the ledger clean, or every subsequent background link/group-algo pass defers
// forever behind a rebuild that is long gone.
func TestRebuild_ReleasesForegroundBargeOnError(t *testing.T) {
	_ = setupTestGroup(t, "barge-err-group", []string{"only"})
	barges := startRebuildBargeObserver(t)

	failIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error { return nil }
	_, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: "no-such-group"}, failIndexFn, noopLinksFn)
	if err == nil {
		t.Fatal("expected an error for an unknown group")
	}
	if n := barges(); n != 0 {
		t.Fatalf("barge count = %d after a FAILED rebuild; want 0 — a leaked barge wedges every background stage", n)
	}
}

// TestRebuild_ReleasesForegroundBargeOnGroupCancel: a `grafel delete <group>`
// mid-rebuild (daemon.CancelGroupRebuild) short-circuits the loop with a
// cancellation error. That path must release the barge too.
func TestRebuild_ReleasesForegroundBargeOnGroupCancel(t *testing.T) {
	group := setupTestGroup(t, "barge-cancel-group", []string{"first", "second"})
	barges := startRebuildBargeObserver(t)

	var once sync.Once
	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		once.Do(func() { daemon.CancelGroupRebuild(group) })
		return nil
	}
	_, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected a 'rebuild cancelled' error, got: %v", err)
	}
	if n := barges(); n != 0 {
		t.Fatalf("barge count = %d after a CANCELLED rebuild; want 0", n)
	}
}
