// group_algo_overlay_reapply_5403_test.go — #5403: a cached/already-loaded group
// must re-apply the group-algo overlay when its <group>-algo.json file mtime
// advances mid-session, WITHOUT a full reload.
//
// Root cause: applyGroupAlgoOverlay is called only at group LOAD (Reload). A
// long-running daemon caches the group, so when the overlay file is recomputed
// (scheduler after a reindex, or `grafel group-algo --write`) the cached group
// keeps serving the last-applied state until a restart — the apply was never
// re-called. The fix hooks the canonical group-serving entry path (State.Group,
// used by clusters/inspect/orient/stats): it os.Stats the overlay and re-applies
// only when the file mtime advanced past the memoized grp.algoMt.
package mcp

import (
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

// TestGroupReapplyOverlayOnMtimeAdvance loads a group with overlay v1 applied,
// then rewrites the overlay file with a NEWER mtime + a different community
// assignment and issues a plain State.Group lookup. With the #5403 fix the
// cached group reflects the NEW overlay without a Reload. It also asserts the
// cheap path (unchanged mtime → no re-apply) and absence-tolerance.
func TestGroupReapplyOverlayOnMtimeAdvance(t *testing.T) {
	// OFF-path pin (ADR-0027 mmap default-on flip): the mtime-advance re-apply
	// trigger (State.Group re-stamps when the overlay file mtime moves) is
	// flag-independent, but every community/pagerank assertion reads lr.Doc.Entities
	// via entityByID — header-only-empty under flag-ON by design, values then live
	// in the Reader + overlay side-table (covered by overlay_sidetable_parity_test).
	forceServeFromMMap(t, false)
	st, overlayPath, cur, beID, feID, mobID, _, _ := setupThreeRepoApplyGroup(t)

	// --- v1 overlay: mobile → community 80.
	ovV1 := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			beID:  {CommunityID: 39, PageRank: 0.0065, Centrality: 10415.99, IsGodNode: true},
			feID:  {CommunityID: 203, PageRank: 0.004, Centrality: 16521.23, IsGodNode: true},
			mobID: {CommunityID: 80, PageRank: 0.001, Centrality: 6706, IsArticulationPoint: true},
		},
		Communities: []graph.CommunityResult{
			{ID: 39, Size: 2, AutoName: "serializer-serializers-core"},
			{ID: 80, Size: 1, AutoName: "features-types-props"},
		},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ovV1); err != nil {
		t.Fatalf("write overlay v1: %v", err)
	}
	v1mt := time.Now().Add(-time.Hour)
	if err := os.Chtimes(overlayPath, v1mt, v1mt); err != nil {
		t.Fatalf("set overlay v1 mtime: %v", err)
	}

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload v1: %v", err)
	}
	if mob := entityByID(st.Group("acme"), mobID); mob == nil || mob.CommunityID == nil || *mob.CommunityID != 80 {
		t.Fatalf("v1: mobile not stamped to overlay community 80: %v", mob)
	}

	// --- Cheap path: a second Group() lookup with an UNCHANGED overlay file must
	// NOT change anything (and, importantly, not re-read/re-stamp — verified by
	// the value staying put with mtime unmoved).
	grpBefore := st.Group("acme")
	algoMtBefore := grpBefore.algoMt
	if mob := entityByID(st.Group("acme"), mobID); mob == nil || *mob.CommunityID != 80 {
		t.Fatalf("cheap path: mobile community changed unexpectedly: %v", mob)
	}
	if !grpBefore.algoMt.Equal(algoMtBefore) {
		t.Fatalf("cheap path: algoMt advanced without a file change")
	}

	// --- v2 overlay: rewrite the SAME file in place with a NEWER mtime and move
	// mobile to a DIFFERENT community (81). No Reload() is issued — this is the
	// settled-daemon case (#5403): the scheduler/CLI rewrote the overlay on disk
	// but never poked the cached group.
	ovV2 := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			beID:  {CommunityID: 39, PageRank: 0.0065, Centrality: 10415.99, IsGodNode: true},
			feID:  {CommunityID: 203, PageRank: 0.004, Centrality: 16521.23, IsGodNode: true},
			mobID: {CommunityID: 81, PageRank: 0.009, Centrality: 9999, IsArticulationPoint: true},
		},
		Communities: []graph.CommunityResult{
			{ID: 39, Size: 2, AutoName: "serializer-serializers-core"},
			{ID: 81, Size: 1, AutoName: "features-types-props-v2"},
		},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ovV2); err != nil {
		t.Fatalf("write overlay v2: %v", err)
	}
	v2mt := time.Now() // strictly after v1mt (one hour ago)
	if err := os.Chtimes(overlayPath, v2mt, v2mt); err != nil {
		t.Fatalf("set overlay v2 mtime: %v", err)
	}

	// A plain serve lookup must now reflect the NEW overlay (community 81) WITHOUT
	// any Reload().
	grp := st.Group("acme")
	mob := entityByID(grp, mobID)
	if mob == nil {
		t.Fatalf("mobile entity missing after overlay v2")
	}
	if mob.CommunityID == nil || *mob.CommunityID != 81 {
		t.Fatalf("#5403: cached group did not re-apply overlay v2; mobile community_id=%v want 81", mob.CommunityID)
	}
	if mob.PageRank == nil || *mob.PageRank != 0.009 {
		t.Errorf("#5403: cached group did not re-apply overlay v2 pagerank; got %v want 0.009", mob.PageRank)
	}
	// grp.Communities reflects the v2 summary too (clusters path).
	var seen81 bool
	for _, c := range grp.Communities {
		if c.ID == 81 {
			seen81 = true
		}
	}
	if !seen81 {
		t.Errorf("#5403: grp.Communities did not refresh to the v2 overlay summary (community 81 missing)")
	}

	// --- Absence tolerance: remove the overlay file and confirm a lookup is a
	// no-op (does not panic; entity keeps the last-applied v2 value — per-query we
	// are non-destructive; a clear happens on the next Reload).
	if err := os.Remove(overlayPath); err != nil {
		t.Fatalf("remove overlay: %v", err)
	}
	grp2 := st.Group("acme")
	if grp2 == nil {
		t.Fatalf("absent overlay: Group returned nil")
	}
	if mob := entityByID(grp2, mobID); mob == nil || mob.CommunityID == nil || *mob.CommunityID != 81 {
		t.Fatalf("absent overlay: expected non-destructive no-op keeping v2 community 81, got %v", mob)
	}
}
