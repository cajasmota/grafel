package main

import "testing"

// #6002: taking the group-algo annotation pass off the critical path of a
// user-awaited rebuild is only acceptable if something reliably produces the
// overlay AFTERWARDS. The overlay sweep is that backstop — but it used to skip
// any group with no overlay file, on the assumption that "the first-compute
// link-pass chain" would cover them.
//
// It does not. `grafel rebuild` / `grafel reset` runs its own in-process link
// pass and never touches the scheduler, so runLinks — the only caller of
// scheduleGroupAlgo — never fires. A freshly reset group therefore had a
// complete, queryable graph and NO path that would ever compute its
// communities.
func TestGroupAlgoSweepWants_FirstComputeForIndexedGroupWithNoOverlay(t *testing.T) {
	no := func(string) bool { return false }
	yes := func(string) bool { return true }

	if !groupAlgoSweepWants("acme", no /*needsRecompute*/, no /*hasOverlay*/, yes /*hasIndexedMember*/) {
		t.Fatal("a group with indexed members and no overlay must be swept for a first compute — otherwise `grafel reset` leaves it permanently without communities")
	}
}

// A group that has never been indexed has nothing to assemble; sweeping it
// would be pure waste. It gets picked up after its first index.
func TestGroupAlgoSweepWants_SkipsNeverIndexedGroup(t *testing.T) {
	no := func(string) bool { return false }

	if groupAlgoSweepWants("acme", no, no /*hasOverlay*/, no /*hasIndexedMember*/) {
		t.Fatal("a group with no indexed member must not be swept — assembling an empty union is wasted work")
	}
}

// The pre-existing contract is preserved: a present overlay that the
// content-gated predicate says is fine must NOT be re-armed. This is the
// property that keeps an idle daemon from firing Louvain every sweep interval.
func TestGroupAlgoSweepWants_PresentFreshOverlayIsLeftAlone(t *testing.T) {
	no := func(string) bool { return false }
	yes := func(string) bool { return true }

	if groupAlgoSweepWants("acme", no /*needsRecompute*/, yes /*hasOverlay*/, yes) {
		t.Fatal("a present, fresh overlay must not be re-armed by the sweep")
	}
}

// And a stale-with-changed-content overlay still wins regardless of the rest —
// including the algorithm-version invalidation, which reports through the same
// needsRecompute probe.
func TestGroupAlgoSweepWants_NeedsRecomputeAlwaysWins(t *testing.T) {
	yes := func(string) bool { return true }

	if !groupAlgoSweepWants("acme", yes /*needsRecompute*/, yes /*hasOverlay*/, yes) {
		t.Fatal("needsRecompute (stale content, or a changed algorithm version) must always arm a pass")
	}
}
