package main

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestCarryForwardAlgoAttrs verifies that a fast reactive re-index (algo pass
// skipped) preserves the prior graph's community/algorithm output instead of
// stripping it (#1620). Per-entity attrs are matched by ID; the aggregate
// Communities list + AlgorithmStats are carried wholesale; brand-new entities
// stay un-annotated.
func TestCarryForwardAlgoAttrs(t *testing.T) {
	cid := 3
	pr := 0.12
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", CommunityID: &cid, PageRank: &pr, IsGodNode: true},
			{ID: "b"},
		},
		Communities: []graph.CommunityResult{
			{ID: 3, Size: 10, AutoName: "core"},
		},
		AlgorithmStats: &graph.AlgorithmStats{NumCommunities: 1, LouvainModularity: 0.7},
	}
	// cur is the fast re-index output: same entity "a", new entity "c",
	// dropped "b", and zero community data.
	cur := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a"},
			{ID: "c"},
		},
	}

	carryForwardAlgoAttrs(cur, prev)

	// Aggregate community list + stats carried forward.
	if len(cur.Communities) != 1 || cur.Communities[0].AutoName != "core" {
		t.Fatalf("communities not carried forward: %+v", cur.Communities)
	}
	if cur.AlgorithmStats == nil || cur.AlgorithmStats.NumCommunities != 1 {
		t.Fatalf("algorithm_stats not carried forward: %+v", cur.AlgorithmStats)
	}

	// Entity "a" keeps its prior algo attrs.
	if cur.Entities[0].CommunityID == nil || *cur.Entities[0].CommunityID != 3 {
		t.Errorf("entity a community_id not preserved: %v", cur.Entities[0].CommunityID)
	}
	if cur.Entities[0].PageRank == nil || *cur.Entities[0].PageRank != 0.12 {
		t.Errorf("entity a pagerank not preserved: %v", cur.Entities[0].PageRank)
	}
	if !cur.Entities[0].IsGodNode {
		t.Errorf("entity a is_god_node not preserved")
	}

	// New entity "c" (no prior match) stays un-annotated.
	if cur.Entities[1].CommunityID != nil {
		t.Errorf("new entity c should be un-annotated, got %v", cur.Entities[1].CommunityID)
	}

	// Mutating the carried pointer must not alias the prior doc.
	*cur.Entities[0].CommunityID = 99
	if *prev.Entities[0].CommunityID != 3 {
		t.Errorf("carry-forward aliased prior doc community_id: %d", *prev.Entities[0].CommunityID)
	}
}
