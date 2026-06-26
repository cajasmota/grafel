package groupalgo

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #5655: the periodic overlay-freshness sweep used a PURELY mtime-based
// predicate (OverlayNeedsRecompute), so any graph.fb mtime bump — including a
// docs-only / comment-only / config-only push, or an idle re-stat — re-armed a
// full Louvain+PageRank+betweenness recompute even when the community input
// graph was identical. That made an "idle" daemon burn a ~½-core burst every
// sweep interval (310 observed recomputes with no driving change).
//
// The fix routes the predicate through the SAME deterministic content gate
// (graph.CommunityInputHash) the incremental reindex path uses: an mtime drift
// is only "needs recompute" when the input hash actually changed. An unchanged
// input settles the overlay's source_mtimes in place (no algorithms) and
// reports false, so the sweep skips.

// writeFreshOverlay computes the group's real algo result and writes it with a
// correct input_hash + current source_mtimes — the state right after a compute.
func writeFreshOverlay(t *testing.T, group string) *GroupAlgoResult {
	t.Helper()
	res, err := RunGroupAlgorithms(group)
	if err != nil {
		t.Fatalf("RunGroupAlgorithms: %v", err)
	}
	if err := WriteOverlayFromResult(res); err != nil {
		t.Fatalf("WriteOverlayFromResult: %v", err)
	}
	return res
}

// TestOverlayNeedsRecompute_UnchangedContent_Skips proves the periodic-sweep
// win: a graph.fb mtime drift whose community input graph is IDENTICAL must NOT
// report needs-recompute (so the sweep skips the heavy pass), the overlay must
// be PRESERVED verbatim, and the mtime gate must be SETTLED so the next sweep
// tick does not re-trip.
func TestOverlayNeedsRecompute_UnchangedContent_Skips(t *testing.T) {
	group, pathA, _, _ := setupIncrGroup(t)
	first := writeFreshOverlay(t, group)

	path, err := OverlayPath(group)
	if err != nil {
		t.Fatalf("OverlayPath: %v", err)
	}
	before := readOverlayUnconditional(path)
	if before == nil || before.InputHash == "" {
		t.Fatal("setup: overlay must exist with an input_hash")
	}

	// Docs-only push shape: rewrite repo-A's graph.fb with the SAME community
	// graph but a changed non-structural property → graph.fb mtime bumps, the
	// community input hash is unchanged. The mtime gate trips; the content gate
	// must veto the recompute.
	repoA, _, _ := fixtureGraphs()
	for i := range repoA.Entities {
		if repoA.Entities[i].Properties == nil {
			repoA.Entities[i].Properties = map[string]string{}
		}
		repoA.Entities[i].Properties["description"] = "edited docs"
	}
	writeFixtureRepo(t, "svc", pathA, repoA)

	// Sanity: the mtime gate alone WOULD trip (the overlay's recorded mtimes no
	// longer match) — so the skip is genuinely the content gate's doing.
	cur, err := CurrentSourceMtimes(group)
	if err != nil {
		t.Fatalf("CurrentSourceMtimes: %v", err)
	}
	if !IsOverlayStale(before, cur) {
		t.Fatal("setup: expected the mtime gate to trip after the docs edit")
	}

	if OverlayNeedsRecompute(group) {
		t.Fatal("unchanged community input must NOT report needs-recompute (sweep should skip)")
	}

	// Overlay preserved verbatim (only source_mtimes settled).
	after := readOverlayUnconditional(path)
	if after == nil {
		t.Fatal("overlay vanished after skip")
	}
	if after.InputHash != before.InputHash {
		t.Errorf("input_hash drifted across a skip: %s != %s", after.InputHash, before.InputHash)
	}
	if len(after.Results) != len(before.Results) {
		t.Errorf("results changed across a skip: %d != %d", len(after.Results), len(before.Results))
	}
	if after.InputHash != first.InputHash {
		t.Errorf("skip overlay hash != original compute hash")
	}

	// mtime gate SETTLED: a second sweep tick must also skip (no re-trip).
	if OverlayNeedsRecompute(group) {
		t.Fatal("second sweep re-tripped after the skip settled source_mtimes")
	}
	settled := readOverlayUnconditional(path)
	if IsOverlayStale(settled, cur) {
		t.Fatal("source_mtimes were not settled to the current graph.fb after the skip")
	}
}

// TestOverlayNeedsRecompute_ChangedContent_Recomputes proves correctness is
// preserved: a genuine structural change (new tightly-connected cluster +
// cross-cluster edge) flips the input hash, so the predicate reports
// needs-recompute and the sweep arms the full pass.
func TestOverlayNeedsRecompute_ChangedContent_Recomputes(t *testing.T) {
	group, pathA, _, serviceID := setupIncrGroup(t)
	writeFreshOverlay(t, group)

	// Structural change in repo-A: add a new cluster wired into the existing graph.
	repoA, _, _ := fixtureGraphs()
	mkEnt := func(name string) graph.Entity {
		return graph.Entity{ID: "svc:" + name, Name: name, Kind: "function", SourceFile: "svc/" + name + ".go", Language: "go"}
	}
	mkRel := func(from, to string) graph.Relationship {
		return graph.Relationship{ID: from + "->" + to, FromID: from, ToID: to, Kind: "CALLS"}
	}
	cluster := []string{"c1", "c2", "c3", "c4", "c5", "c6"}
	for _, n := range cluster {
		repoA.Entities = append(repoA.Entities, mkEnt(n))
	}
	for i := 0; i < len(cluster); i++ {
		for j := i + 1; j < len(cluster); j++ {
			repoA.Relationships = append(repoA.Relationships, mkRel("svc:"+cluster[i], "svc:"+cluster[j]))
		}
	}
	repoA.Relationships = append(repoA.Relationships, mkRel("svc:c1", serviceID))
	writeFixtureRepo(t, "svc", pathA, repoA)

	if !OverlayNeedsRecompute(group) {
		t.Fatal("a genuine structural change MUST report needs-recompute")
	}
}

// TestOverlayNeedsRecompute_LegacyOverlayNoHash_Recomputes: an overlay written
// before the input_hash field existed cannot be content-checked, so an mtime
// drift falls back to the mtime verdict (recompute) — never silently skipped.
func TestOverlayNeedsRecompute_LegacyOverlayNoHash_Recomputes(t *testing.T) {
	group, pathA, _, _ := setupIncrGroup(t)
	writeFreshOverlay(t, group)

	// Strip the input_hash to simulate a pre-#5309 overlay, keep stale mtimes.
	path, err := OverlayPath(group)
	if err != nil {
		t.Fatalf("OverlayPath: %v", err)
	}
	ov := readOverlayUnconditional(path)
	if ov == nil {
		t.Fatal("setup: overlay missing")
	}
	ov.InputHash = ""
	if err := WriteOverlayTo(path, ov); err != nil {
		t.Fatalf("rewrite legacy overlay: %v", err)
	}

	// Bump a graph.fb mtime (any content edit) so the mtime gate trips.
	repoA, _, _ := fixtureGraphs()
	repoA.Entities[0].Properties = map[string]string{"x": "y"}
	writeFixtureRepo(t, "svc", filepath.Join(pathA), repoA)

	if !OverlayNeedsRecompute(group) {
		t.Fatal("a hash-less (legacy) overlay must defer to the mtime verdict and recompute")
	}
}
