// group_algo_overlay_readpath_5729_test.go — #5401 residual (#5729): the
// group-algo overlay must keep surfacing on the READ path (State.Group, used
// by clusters/inspect/orient/stats) even when a sub-repo's graph.fb is
// reparsed in-memory WITHOUT the overlay file's own mtime advancing.
//
// Root cause: refreshGroupAlgoOverlayLocked (the read-path refresh called from
// State.Group) early-returns whenever the overlay FILE mtime has not advanced
// past the memoized grp.algoMt:
//
//	if grp.algoApplied && !fi.ModTime().After(grp.algoMt) {
//		return
//	}
//
// applyGroupAlgoOverlay itself is per-repo-memoized (#5400/#5401) and would
// happily re-stamp exactly the reparsed repo — but the read path's own
// early-return above never lets it run when only a repo (not the overlay
// file) changed. A daemon that serves State.Group() without an intervening
// full Reload() (e.g. after some other code path swaps a sub-repo's Doc/mtime
// in memory) leaves that repo's entities pinned at the per-repo sentinel
// community id (-1), and grafel_stats / grafel_inspect / clusters report
// communities: 0 for it.
package mcp

import (
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

// buildOverlayFor5729 mirrors the overlay shape used by the #5400 restamp
// test: backend/frontend/mobile each get a distinct cross-repo community.
func buildOverlayFor5729(cur map[string]int64, beID, feID, mobID string) *groupalgo.Overlay {
	return &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			beID:  {CommunityID: 39, PageRank: 0.0065, Centrality: 10415.99, IsGodNode: true},
			feID:  {CommunityID: 203, PageRank: 0.004, Centrality: 16521.23, IsGodNode: true},
			mobID: {CommunityID: 80, PageRank: 0.001, Centrality: 6706, IsArticulationPoint: true},
		},
		Communities: []graph.CommunityResult{
			{ID: 39, Size: 2, AutoName: "serializer-serializers-core"},
			{ID: 203, Size: 1, AutoName: "use-hooks-actions"},
			{ID: 80, Size: 1, AutoName: "features-types-props"},
		},
	}
}

func writeOverlay5729(t *testing.T, path string, ov *groupalgo.Overlay) {
	t.Helper()
	if err := groupalgo.WriteOverlayTo(path, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
}

func chtimes5729(t *testing.T, path string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

// TestGroupReadPath_ReStampsStaleRepoWithoutOverlayFileChange reproduces the
// #5401 residual: after the overlay is applied once (via Reload), a sub-repo's
// in-memory Doc/mtime is mutated to simulate a bare per-repo reparse (fresh
// entities carrying the -1 sentinel, lr.mtime advanced) with NO change to the
// overlay file's own mtime. Driving the read path (State.Group) must still
// re-stamp that repo back to its overlay community, because applyGroupAlgoOverlay
// is memoized PER REPO, not just by the overlay file's mtime.
func TestGroupReadPath_ReStampsStaleRepoWithoutOverlayFileChange(t *testing.T) {
	// OFF-path pin (ADR-0027 mmap default-on flip): the read-path per-repo
	// re-stamp trigger is flag-independent, but this test drives it by MUTATING
	// mobLR.Doc.Entities[i].CommunityID and reading the result via entityByID.
	// Under flag-ON the Doc is header-only-empty by design (values flow through the
	// Reader + overlay side-table), so both the mutation and the read are no-ops.
	forceServeFromMMap(t, false)
	st, overlayPath, cur, beID, feID, mobID, _, _ := setupThreeRepoApplyGroup(t)

	ov := buildOverlayFor5729(cur, beID, feID, mobID)
	writeOverlay5729(t, overlayPath, ov)

	// Pin the overlay file mtime so it does NOT advance across this test —
	// isolating the read-path repo-staleness re-apply from any overlay-file
	// mtime change.
	pinned := time.Now().Add(-time.Hour)
	chtimes5729(t, overlayPath, pinned)

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload v1: %v", err)
	}
	// Sanity: mobile stamped to overlay community 80 on first apply.
	if mob := entityByID(st.Group("acme"), mobID); mob == nil || mob.CommunityID == nil || *mob.CommunityID != 80 {
		t.Fatalf("v1: mobile not stamped to overlay community 80: %v", mob)
	}

	grp := st.Group("acme")
	mobLR := grp.Repos["mobile"]
	if mobLR == nil || mobLR.Doc == nil {
		t.Fatalf("mobile repo not loaded")
	}

	// --- Simulate a BARE per-repo reparse: fresh in-memory entities carrying
	// the per-repo sentinel community id (-1), and lr.mtime advanced — but the
	// overlay FILE mtime (grp.algoMt) is left untouched. This models a reparse
	// that landed in memory without an intervening full Reload() that would
	// otherwise call applyGroupAlgoOverlay unconditionally.
	for i := range mobLR.Doc.Entities {
		mobLR.Doc.Entities[i].CommunityID = ip(-1)
	}
	mobLR.mtime = mobLR.mtime.Add(time.Second)
	// grp.algoMt / grp.algoApplied intentionally left unchanged.

	// Drive the READ path directly — this is the entry point clusters/inspect/
	// orient/stats all funnel through.
	grp2 := st.Group("acme")

	mob := entityByID(grp2, mobID)
	if mob == nil {
		t.Fatalf("mobile entity missing after simulated reparse")
	}
	if mob.CommunityID == nil || *mob.CommunityID != 80 {
		t.Fatalf("#5401 residual: read-path (State.Group) failed to re-stamp reparsed mobile repo; community_id=%v, want overlay 80", mob.CommunityID)
	}

	mobileComm := statsCommunitiesForRepo(t, st, "mobile")
	if mobileComm < 1 {
		t.Errorf("#5401 residual: grafel_stats reports %d communities for mobile via read path; want >=1", mobileComm)
	}
}

// TestGroupReadPath_SteadyStateDoesNotReApplyWhenNothingStale guards against a
// regression in the fix: when the overlay file is unchanged AND no repo is
// stale (every lr.mtime already equals its lr.algoStampedMt), a read-path
// State.Group() call must stay a no-op (grp.algoMt must not change) — the fix
// must fall through to applyGroupAlgoOverlay ONLY when some repo is actually
// stale, not on every call.
func TestGroupReadPath_SteadyStateDoesNotReApplyWhenNothingStale(t *testing.T) {
	// OFF-path pin (ADR-0027 mmap default-on flip): the steady-state no-op guard
	// (grp.algoMt must not advance) is flag-independent, but the final community
	// assertion reads lr.Doc.Entities via entityByID — header-only-empty flag-ON.
	forceServeFromMMap(t, false)
	st, overlayPath, cur, beID, feID, mobID, _, _ := setupThreeRepoApplyGroup(t)

	ov := buildOverlayFor5729(cur, beID, feID, mobID)
	writeOverlay5729(t, overlayPath, ov)

	pinned := time.Now().Add(-time.Hour)
	chtimes5729(t, overlayPath, pinned)

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	grp := st.Group("acme")
	if grp == nil {
		t.Fatalf("group not loaded")
	}
	steadyMt := grp.algoMt

	// Steady-state: nothing changed. Repeated read-path calls must not advance
	// grp.algoMt (i.e. must not treat this as a fresh apply every time).
	for i := 0; i < 3; i++ {
		grp2 := st.Group("acme")
		if !grp2.algoMt.Equal(steadyMt) {
			t.Fatalf("steady-state read-path call unexpectedly changed grp.algoMt: %v -> %v", steadyMt, grp2.algoMt)
		}
	}

	mob := entityByID(grp, mobID)
	if mob == nil || mob.CommunityID == nil || *mob.CommunityID != 80 {
		t.Fatalf("steady-state: mobile lost its overlay community: %v", mob)
	}
}
