package graph

import (
	"reflect"
	"testing"
)

func ci(v int) *int { return &v }

// fixture: two communities.
//
//	community 0 (api): a -> b -> c   (c is the leaf "changed" thing)
//	community 1 (db):  d -> e
//
// edges are FROM=caller TO=callee (caller depends on callee).
func prImpactFixture() ([]Entity, []Relationship) {
	ents := []Entity{
		{ID: "a", Name: "A", Kind: "function", SourceFile: "api/a.go", CommunityID: ci(0)},
		{ID: "b", Name: "B", Kind: "function", SourceFile: "api/b.go", CommunityID: ci(0)},
		{ID: "c", Name: "C", Kind: "function", SourceFile: "api/c.go", CommunityID: ci(0)},
		{ID: "d", Name: "D", Kind: "function", SourceFile: "db/d.go", CommunityID: ci(1)},
		{ID: "e", Name: "E", Kind: "function", SourceFile: "db/e.go", CommunityID: ci(1)},
	}
	rels := []Relationship{
		{FromID: "a", ToID: "b", Kind: "CALLS"}, // a depends on b
		{FromID: "b", ToID: "c", Kind: "CALLS"}, // b depends on c
		{FromID: "d", ToID: "e", Kind: "CALLS"}, // d depends on e
	}
	return ents, rels
}

// Changing leaf entity "c" should: mark c changed (community 0), and the blast
// radius (inbound dependents) should reach b (hop 1) and a (hop 2), all in
// community 0. community 1 is untouched.
func TestAnalyzePRImpact_BlastRadiusAndCommunities(t *testing.T) {
	ents, rels := prImpactFixture()
	change := ChangeSet{
		Modified: []DiffEntityEntry{{ID: "c", Name: "C", Kind: "function", SourceFile: "api/c.go"}},
	}
	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())

	if res.ChangedCount != 1 || res.ChangedEntities[0].ID != "c" {
		t.Fatalf("expected 1 changed entity c, got %+v", res.ChangedEntities)
	}
	if res.ChangedEntities[0].Change != "modified" {
		t.Errorf("expected change=modified, got %q", res.ChangedEntities[0].Change)
	}
	if res.ChangedEntities[0].CommunityID != 0 {
		t.Errorf("expected c in community 0, got %d", res.ChangedEntities[0].CommunityID)
	}

	// Blast radius: b (hop1), a (hop2). Sorted nearest-first.
	if res.BlastRadiusCount != 2 {
		t.Fatalf("expected blast radius 2 (a,b), got %d: %+v", res.BlastRadiusCount, res.BlastRadius)
	}
	if res.BlastRadius[0].ID != "b" || res.BlastRadius[0].HopDistance != 1 {
		t.Errorf("expected b at hop1 first, got %+v", res.BlastRadius[0])
	}
	if res.BlastRadius[1].ID != "a" || res.BlastRadius[1].HopDistance != 2 {
		t.Errorf("expected a at hop2 second, got %+v", res.BlastRadius[1])
	}

	// Only community 0 is impacted; db community 1 untouched.
	if res.CommunityCount != 1 {
		t.Fatalf("expected 1 impacted community, got %d: %+v", res.CommunityCount, res.ImpactedCommunities)
	}
	c0 := res.ImpactedCommunities[0]
	if c0.CommunityID != 0 || c0.ChangedCount != 1 || c0.BlastRadiusHit != 2 {
		t.Errorf("community 0 rollup wrong: %+v", c0)
	}
	if ids := res.ImpactedCommunityIDs(); !reflect.DeepEqual(ids, []int{0}) {
		t.Errorf("ImpactedCommunityIDs = %v, want [0]", ids)
	}
}

// Hops cap bounds the blast radius depth.
func TestAnalyzePRImpact_HopsCap(t *testing.T) {
	ents, rels := prImpactFixture()
	change := ChangeSet{Modified: []DiffEntityEntry{{ID: "c"}}}
	res := AnalyzePRImpact(ents, rels, change, PRImpactOptions{Hops: 1})
	// hops=1 reaches only b, not a.
	if res.BlastRadiusCount != 1 || res.BlastRadius[0].ID != "b" {
		t.Fatalf("hops=1 should reach only b, got %+v", res.BlastRadius)
	}
}

// A removed entity (gone from HEAD) is still reported as changed, using the diff
// record for its metadata, and contributes no downstream (it has no HEAD edges).
func TestAnalyzePRImpact_RemovedEntity(t *testing.T) {
	ents, rels := prImpactFixture()
	change := ChangeSet{
		Removed: []DiffEntityEntry{{ID: "gone", Name: "Gone", Kind: "function", SourceFile: "api/gone.go"}},
	}
	res := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())
	if res.ChangedCount != 1 || res.ChangedEntities[0].ID != "gone" {
		t.Fatalf("expected changed=gone, got %+v", res.ChangedEntities)
	}
	if res.ChangedEntities[0].Change != "removed" || res.ChangedEntities[0].Name != "Gone" {
		t.Errorf("removed entity metadata wrong: %+v", res.ChangedEntities[0])
	}
	if res.ChangedEntities[0].CommunityID != -1 {
		t.Errorf("removed entity should be ungrouped (-1), got %d", res.ChangedEntities[0].CommunityID)
	}
	if res.BlastRadiusCount != 0 {
		t.Errorf("removed-only change should have empty blast radius, got %d", res.BlastRadiusCount)
	}
}

// Two changes touching the SAME community are a merge risk; a disjoint change is
// not. The risky pair must be ranked first with the shared community listed.
func TestAnalyzeMergeRisk_OverlapVsDisjoint(t *testing.T) {
	impacts := []ChangeImpact{
		{Ref: "pr-a", Communities: []int{0, 2}}, // api + shared
		{Ref: "pr-b", Communities: []int{0, 3}}, // api + shared  -> overlaps pr-a on {0}
		{Ref: "pr-c", Communities: []int{9}},    // disjoint from everyone
	}
	res := AnalyzeMergeRisk(impacts)
	if res.RefCount != 3 {
		t.Errorf("expected ref_count 3, got %d", res.RefCount)
	}
	if res.RiskyPairs != 1 {
		t.Fatalf("expected exactly 1 risky pair, got %d: %+v", res.RiskyPairs, res.Pairs)
	}
	p := res.Pairs[0]
	if p.RefA != "pr-a" || p.RefB != "pr-b" {
		t.Errorf("expected pair (pr-a,pr-b), got (%s,%s)", p.RefA, p.RefB)
	}
	if p.SharedCount != 1 || !reflect.DeepEqual(p.SharedCommunities, []int{0}) {
		t.Errorf("expected shared {0}, got count=%d comms=%v", p.SharedCount, p.SharedCommunities)
	}
}

// Ranking: a pair sharing more communities ranks above one sharing fewer; ties
// broken by ref labels for determinism.
func TestAnalyzeMergeRisk_Ranking(t *testing.T) {
	impacts := []ChangeImpact{
		{Ref: "x", Communities: []int{1, 2, 3}},
		{Ref: "y", Communities: []int{1, 2, 3}}, // shares 3 with x
		{Ref: "z", Communities: []int{3}},       // shares 1 with x and y
	}
	res := AnalyzeMergeRisk(impacts)
	if res.RiskyPairs != 3 {
		t.Fatalf("expected 3 risky pairs, got %d: %+v", res.RiskyPairs, res.Pairs)
	}
	// Highest-overlap pair (x,y sharing 3) ranks first.
	if res.Pairs[0].RefA != "x" || res.Pairs[0].RefB != "y" || res.Pairs[0].SharedCount != 3 {
		t.Errorf("expected (x,y,3) first, got %+v", res.Pairs[0])
	}
	// Remaining two pairs each share exactly community 3; tie broken by ref.
	for _, p := range res.Pairs[1:] {
		if p.SharedCount != 1 || !reflect.DeepEqual(p.SharedCommunities, []int{3}) {
			t.Errorf("expected the 1-overlap pairs to share {3}, got %+v", p)
		}
	}
	if res.Pairs[1].RefA != "x" || res.Pairs[1].RefB != "z" {
		t.Errorf("tiebreak: expected (x,z) before (y,z), got %+v", res.Pairs[1])
	}
}

// Ungrouped (-1) communities never count as overlap.
func TestAnalyzeMergeRisk_UngroupedIgnored(t *testing.T) {
	impacts := []ChangeImpact{
		{Ref: "a", Communities: []int{-1}},
		{Ref: "b", Communities: []int{-1}},
	}
	res := AnalyzeMergeRisk(impacts)
	if res.RiskyPairs != 0 {
		t.Errorf("ungrouped-only changes should not be flagged as risks, got %+v", res.Pairs)
	}
}
