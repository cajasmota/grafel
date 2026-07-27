package main

// foreground_wallclock_5954_test.go — the WIRING half of the foreground /
// background cap split (#5954 wall-time regression).
//
// sched owns the policy and tests it directly. What it cannot see is whether
// the rebuild path — the one place in the product where a human is demonstrably
// waiting — actually announces itself. Without that announcement every child a
// `grafel reset` / rebuild RPC ultimately causes (the index child, the
// scheduler's follow-on link pass, and the debounced group-algo pass that
// finishes the graph) resolves BACKGROUND caps, and the user waits 30+ minutes
// for what used to take 10.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// TestRebuild_MarksTheGroupForeground: while the rebuild runs, and — critically
// — after it returns, the group must read as foreground so the follow-on
// stages the user is still waiting on are not spawned at background caps.
func TestRebuild_MarksTheGroupForeground(t *testing.T) {
	group := setupTestGroup(t, "fg-caps-group", []string{"first", "second"})
	t.Cleanup(func() { sched.ClearGroupForeground(group) })

	var duringIndex, duringLinks atomic.Bool
	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		if sched.GroupIsForeground(group) {
			duringIndex.Store(true)
		}
		return nil
	}
	linksFn := func(_ context.Context, _ string) error {
		duringLinks.Store(sched.GroupIsForeground(group))
		return nil
	}

	if _, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, linksFn); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if !duringIndex.Load() {
		t.Error("the group did not read as foreground during the rebuild's index batch — " +
			"every child spawned for it resolves the 25% background caps (#5954)")
	}
	if !duringLinks.Load() {
		t.Error("the group did not read as foreground during the rebuild's link pass")
	}
	if !sched.GroupIsForeground(group) {
		t.Error("the group stopped reading as foreground the moment the rebuild returned — " +
			"the debounced group-algo pass (measured 32–53 min at cap=2) fires AFTER this point " +
			"and is exactly what the user is still waiting for")
	}
}

// A rebuild that fails early must still not wedge an unrelated group into
// foreground caps, and must not leave a permanent hold.
func TestRebuild_ForegroundMarkDoesNotLeakToOtherGroups(t *testing.T) {
	group := setupTestGroup(t, "fg-caps-other", []string{"only"})
	t.Cleanup(func() { sched.ClearGroupForeground(group) })

	idx := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error { return nil }
	if _, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, idx, noopLinksFn); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if sched.GroupIsForeground("some-other-group") {
		t.Error("rebuilding one group marked an unrelated group foreground")
	}
}

// --- GOGC on the foreground index child --------------------------------------

// GOGC=50 costs ~4.7% wall on the index child and exists to trade GC CPU for
// RSS on work nobody is waiting on. On the foreground path someone IS waiting,
// and the standing product rule ("interactive work is never GC-capped", see the
// allow-list in index_gcpercent.go) already says so — the index child was only
// capped because the allow-list keys on argv[1], which is `index-internal` for
// BOTH the background reindex and the user's rebuild.
func TestIndexGCPercent_ForegroundIndexChildIsNotCapped(t *testing.T) {
	got, source := indexGCPercentDecision(backgroundIndexCommand, true, "", "")
	if got != gcPercentUnset {
		t.Errorf("foreground index child GC percent = %d, want uncapped — "+
			"a human-awaited rebuild must not pay GC time for RSS it is not short of (source=%s)", got, source)
	}
	if !strings.Contains(strings.ToLower(source), "foreground") {
		t.Errorf("decision source = %q, want it to name the foreground path", source)
	}
}

func TestIndexGCPercent_BackgroundIndexChildStillCapped(t *testing.T) {
	got, source := indexGCPercentDecision(backgroundIndexCommand, false, "", "")
	if got != indexGCPercentDefault {
		t.Errorf("background index child GC percent = %d, want %d (source=%s) — "+
			"the background RSS policy is a standing decision and must not regress",
			got, indexGCPercentDefault, source)
	}
}

// The two batch children keep GOGC=50 even in foreground: their live heap is
// UNMEASURED, and doubling the GC target on an unmeasured heap on a 16 GB box
// is the risk this repo has already been burned by.
func TestIndexGCPercent_BatchChildrenStayCappedEvenForeground(t *testing.T) {
	for _, cmd := range []string{backgroundGroupAlgoCommand, backgroundLinksCommand} {
		if got, source := indexGCPercentDecision(cmd, true, "", ""); got != indexGCPercentDefault {
			t.Errorf("%s foreground GC percent = %d, want %d (source=%s) — "+
				"this child's live heap is unmeasured; relaxing GC pacing there is unbounded risk",
				cmd, got, indexGCPercentDefault, source)
		}
	}
}

// The operator escape hatch keeps its precedence on the background path, and
// still cannot conjure a cap onto the foreground path.
func TestIndexGCPercent_EnvOverrideUnchanged(t *testing.T) {
	if got, _ := indexGCPercentDecision(backgroundIndexCommand, false, "35", ""); got != 35 {
		t.Errorf("GRAFEL_INDEX_GOGC=35 background = %d, want 35", got)
	}
	if got, _ := indexGCPercentDecision("rebuild", false, "35", ""); got != gcPercentUnset {
		t.Errorf("GRAFEL_INDEX_GOGC=35 on a non-allow-listed command = %d, want uncapped", got)
	}
}

// The foreground signal for the index child comes off its own argv — the child
// is already spawned with --interactive (#5135), so no new mechanism is needed.
func TestIndexChildArgvCarriesTheForegroundSignal(t *testing.T) {
	if !argvIsInteractive([]string{"--repo=/x", "--interactive"}) {
		t.Error("--interactive not recognised in the index child argv")
	}
	if !argvIsInteractive([]string{"-interactive", "--repo=/x"}) {
		t.Error("single-dash -interactive not recognised (Go's flag package accepts both)")
	}
	if !argvIsInteractive([]string{"--interactive=true"}) {
		t.Error("--interactive=true not recognised")
	}
	if argvIsInteractive([]string{"--interactive=false"}) {
		t.Error("--interactive=false read as foreground")
	}
	if argvIsInteractive([]string{"--repo=/x", "--incremental=/y"}) {
		t.Error("a background reindex argv read as foreground")
	}
}
