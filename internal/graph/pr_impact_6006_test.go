// pr_impact_6006_test.go — issue #6006: merge-risk must not answer "no
// conflicts" when the community data the answer depends on was never computed.
//
// The dangerous shape is a FALSE NEGATIVE on a safety question. These tests pin
// the discrimination directly: two refs with NO community data and two refs with
// REAL, DISJOINT community data both produce zero risky pairs — the result must
// still tell them apart.
package graph

import (
	"reflect"
	"testing"
)

// stripCommunities returns a copy of ents with every CommunityID cleared, which
// is exactly what a graph.fb loaded from disk looks like today (the per-repo
// Pass-4 algo pass was removed; the fb scalars are permanent sentinels).
func stripCommunities(ents []Entity) []Entity {
	out := make([]Entity, len(ents))
	copy(out, ents)
	for i := range out {
		out[i].CommunityID = nil
	}
	return out
}

// A graph that DOES carry communities reports community data as available; the
// same graph with the ids stripped reports it as unavailable. The two fixtures
// differ only in the community stamps, so this cannot pass by accident.
func TestAnalyzePRImpact_CommunityDataAvailability(t *testing.T) {
	ents, rels := prImpactFixture()
	change := ChangeSet{Modified: []DiffEntityEntry{{ID: "c"}}}

	withComm := AnalyzePRImpact(ents, rels, change, DefaultPRImpactOptions())
	if !withComm.CommunityDataAvailable {
		t.Errorf("graph carries community ids; CommunityDataAvailable = false, want true")
	}

	without := AnalyzePRImpact(stripCommunities(ents), rels, change, DefaultPRImpactOptions())
	if without.CommunityDataAvailable {
		t.Errorf("graph carries NO community ids; CommunityDataAvailable = true, want false")
	}
	// And the community-derived output really is empty in that case — which is
	// precisely why the flag has to exist.
	if len(without.ImpactedCommunityIDs()) != 0 {
		t.Errorf("expected no impacted community ids without community data, got %v",
			without.ImpactedCommunityIDs())
	}
}

// The ungrouped bucket must not be presented as a real community. The fixture
// mixes a grouped changed entity with an ungrouped one so the filter has
// something to remove AND something to keep — a fixture with only ungrouped
// entities could not tell "filtered" from "empty".
func TestAnalyzePRImpact_UngroupedBucketNotReportedAsCommunity(t *testing.T) {
	ents := []Entity{
		{ID: "a", Name: "A", Kind: "function", CommunityID: ci(0)},
		{ID: "u", Name: "U", Kind: "function"}, // no community
	}
	change := ChangeSet{Modified: []DiffEntityEntry{{ID: "a"}, {ID: "u"}}}
	res := AnalyzePRImpact(ents, nil, change, DefaultPRImpactOptions())

	for _, c := range res.ImpactedCommunities {
		if c.CommunityID < 0 {
			t.Errorf("impacted_communities surfaced the ungrouped bucket: %+v (all: %+v)",
				c, res.ImpactedCommunities)
		}
	}
	if res.CommunityCount != 1 {
		t.Errorf("impacted_community_count = %d, want 1 (community 0 only); got %+v",
			res.CommunityCount, res.ImpactedCommunities)
	}
	// The grouped community is still fully reported.
	if len(res.ImpactedCommunities) != 1 || res.ImpactedCommunities[0].CommunityID != 0 ||
		res.ImpactedCommunities[0].ChangedCount != 1 {
		t.Errorf("community 0 rollup lost: %+v", res.ImpactedCommunities)
	}
}

// THE critical test. Both scenarios yield zero risky pairs; the result must make
// them distinguishable.
func TestAnalyzeMergeRisk_UnavailableIsDistinguishableFromNoConflicts(t *testing.T) {
	// Genuine "no conflicts": community data present, sets disjoint.
	genuine := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{1}, CommunityDataAvailable: true},
		{Ref: "pr-b", Communities: []int{2}, CommunityDataAvailable: true},
	})
	if genuine.RiskyPairs != 0 {
		t.Fatalf("disjoint communities should be 0 risky pairs, got %+v", genuine.Pairs)
	}
	if !genuine.CommunityDataAvailable {
		t.Errorf("genuine no-conflict result reported community data unavailable")
	}
	if len(genuine.RefsWithoutCommunityData) != 0 {
		t.Errorf("genuine no-conflict result listed refs without community data: %v",
			genuine.RefsWithoutCommunityData)
	}

	// "Not computed": no community data at all.
	unknown := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", CommunityDataAvailable: false},
		{Ref: "pr-b", CommunityDataAvailable: false},
	})
	if unknown.RiskyPairs != 0 {
		t.Fatalf("no community data cannot produce risky pairs, got %+v", unknown.Pairs)
	}
	if unknown.CommunityDataAvailable {
		t.Errorf("uncomputed result claimed community data was available — this is the #6006 false negative")
	}
	if !reflect.DeepEqual(unknown.RefsWithoutCommunityData, []string{"pr-a", "pr-b"}) {
		t.Errorf("RefsWithoutCommunityData = %v, want [pr-a pr-b]", unknown.RefsWithoutCommunityData)
	}
}

// A partial outage — one ref has community data, one does not — is still not a
// trustworthy "no conflicts", and the specific ref must be named.
func TestAnalyzeMergeRisk_PartialUnavailabilityIsReported(t *testing.T) {
	res := AnalyzeMergeRisk([]ChangeImpact{
		{Ref: "pr-a", Communities: []int{1}, CommunityDataAvailable: true},
		{Ref: "pr-b", CommunityDataAvailable: false},
	})
	if res.CommunityDataAvailable {
		t.Errorf("one ref without community data must make the whole result untrusted")
	}
	if !reflect.DeepEqual(res.RefsWithoutCommunityData, []string{"pr-b"}) {
		t.Errorf("RefsWithoutCommunityData = %v, want [pr-b]", res.RefsWithoutCommunityData)
	}
}
