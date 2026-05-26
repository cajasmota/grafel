package graph

import (
	"reflect"
	"sort"
	"testing"
)

// makeModEntity builds a regular entity with a module tag.
func makeModEntity(id, name, mod, repo string) Entity {
	return Entity{
		ID:   id,
		Name: name,
		Kind: "Function",
		Properties: map[string]string{
			"module": mod,
			"repo":   repo,
		},
	}
}

// makeModContainer builds a synthetic Module container entity (post-#1383 shape).
func makeModContainer(id, name, repo string) Entity {
	return Entity{
		ID:   id,
		Name: name,
		Kind: kindModule,
		Properties: map[string]string{
			"module":    name,
			"repo":      repo,
			"synthetic": "true",
		},
	}
}

// TestRunModuleAlgorithms_EmptyInputs ensures the API never returns nil.
func TestRunModuleAlgorithms_EmptyInputs(t *testing.T) {
	t.Parallel()
	got := RunModuleAlgorithms(nil, nil)
	if got == nil {
		t.Fatal("expected non-nil results for empty input")
	}
	if got.Stats.NumModules != 0 || len(got.SCCs) != 0 || len(got.Centrality) != 0 {
		t.Fatalf("expected empty result, got %+v", got)
	}
}

// TestAggregation_EntityEdgesCollapseToModules verifies that entity edges in
// different modules collapse into module edges and intra-module ones are dropped.
func TestAggregation_EntityEdgesCollapseToModules(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		makeModEntity("e1", "A", "mod_a", "r"),
		makeModEntity("e2", "B", "mod_a", "r"),
		makeModEntity("e3", "C", "mod_b", "r"),
		makeModEntity("e4", "D", "mod_c", "r"),
	}
	rels := []Relationship{
		{ID: "r1", FromID: "e1", ToID: "e2", Kind: "CALLS"},   // intra-module — drop
		{ID: "r2", FromID: "e1", ToID: "e3", Kind: "CALLS"},   // mod_a → mod_b
		{ID: "r3", FromID: "e2", ToID: "e3", Kind: "CALLS"},   // mod_a → mod_b (weight++)
		{ID: "r4", FromID: "e3", ToID: "e4", Kind: "IMPORTS"}, // mod_b → mod_c
	}
	res := RunModuleAlgorithms(entities, rels)

	wantMods := []string{"mod_a", "mod_b", "mod_c"}
	if !reflect.DeepEqual(res.ModuleIDs, wantMods) {
		t.Fatalf("ModuleIDs: want %v, got %v", wantMods, res.ModuleIDs)
	}
	if len(res.Edges) != 2 {
		t.Fatalf("expected 2 module edges, got %d: %+v", len(res.Edges), res.Edges)
	}
	// mod_a→mod_b should have weight 2, mod_b→mod_c weight 1.
	got := map[string]int{}
	for _, e := range res.Edges {
		got[e.FromModule+"→"+e.ToModule] = e.Weight
	}
	if got["mod_a→mod_b"] != 2 || got["mod_b→mod_c"] != 1 {
		t.Fatalf("weights wrong: %+v", got)
	}
}

// TestAggregation_PrebakedModuleNodes verifies that when Module containers and
// DEPENDS_ON edges already exist (post-#1383 documents), they are used and
// their stored weight is respected.
func TestAggregation_PrebakedModuleNodes(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		makeModContainer("M_A", "mod_a", "r"),
		makeModContainer("M_B", "mod_b", "r"),
		makeModEntity("e1", "A", "mod_a", "r"),
		makeModEntity("e2", "B", "mod_b", "r"),
	}
	rels := []Relationship{
		{ID: "rd", FromID: "M_A", ToID: "M_B", Kind: relDependsOn, Properties: map[string]string{"weight": "7"}},
		// CONTAINS edges should be ignored.
		{ID: "rc1", FromID: "M_A", ToID: "e1", Kind: relContains},
		{ID: "rc2", FromID: "M_B", ToID: "e2", Kind: relContains},
	}
	res := RunModuleAlgorithms(entities, rels)
	if len(res.Edges) != 1 {
		t.Fatalf("expected 1 module edge, got %d", len(res.Edges))
	}
	e := res.Edges[0]
	if e.FromModule != "M_A" || e.ToModule != "M_B" || e.Weight != 7 {
		t.Fatalf("bad edge: %+v", e)
	}
	if res.ModuleNames["M_A"] != "mod_a" {
		t.Fatalf("expected name lookup, got %v", res.ModuleNames)
	}
}

// TestExternalBucketDropped — entities without a module label do not get
// collapsed into the synthetic "_external" bucket as a result.
func TestExternalBucketDropped(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		makeModContainer("M_EXT", moduleExternal, "r"),
		makeModContainer("M_A", "mod_a", "r"),
		makeModEntity("e1", "A", "mod_a", "r"),
		{ID: "e2", Name: "Untagged", Kind: "Function"}, // no module
	}
	rels := []Relationship{
		{ID: "r1", FromID: "e1", ToID: "e2", Kind: "CALLS"},
	}
	res := RunModuleAlgorithms(entities, rels)
	for _, m := range res.ModuleIDs {
		if m == "M_EXT" {
			t.Fatalf("_external module should be dropped, got %v", res.ModuleIDs)
		}
	}
	if len(res.Edges) != 0 {
		t.Fatalf("expected no edges (untagged endpoint), got %d", len(res.Edges))
	}
}

// TestSCCDetection_TwoModuleCycle — a 2-module cycle is detected.
func TestSCCDetection_TwoModuleCycle(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		makeModEntity("a", "A", "mod_a", "r"),
		makeModEntity("b", "B", "mod_b", "r"),
	}
	rels := []Relationship{
		{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "r2", FromID: "b", ToID: "a", Kind: "CALLS"},
	}
	res := RunModuleAlgorithms(entities, rels)
	if len(res.SCCs) != 1 {
		t.Fatalf("expected 1 SCC, got %d", len(res.SCCs))
	}
	scc := res.SCCs[0]
	if scc.Size != 2 {
		t.Fatalf("expected size 2, got %d", scc.Size)
	}
	want := []string{"mod_a", "mod_b"}
	got := append([]string{}, scc.Members...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("members: want %v, got %v", want, got)
	}
	if len(scc.Edges) != 2 {
		t.Fatalf("expected 2 internal edges, got %+v", scc.Edges)
	}
	if res.SCCOf["mod_a"] != scc.ID || res.SCCOf["mod_b"] != scc.ID {
		t.Fatalf("SCCOf not populated: %+v", res.SCCOf)
	}
	if res.Stats.NumSCCs != 1 || res.Stats.LargestSCCSize != 2 || res.Stats.NumModulesInCycle != 2 {
		t.Fatalf("stats wrong: %+v", res.Stats)
	}
}

// TestSCCDetection_NoCycle — a DAG produces no SCC and all SCCOf are -1.
func TestSCCDetection_NoCycle(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		makeModEntity("a", "A", "mod_a", "r"),
		makeModEntity("b", "B", "mod_b", "r"),
		makeModEntity("c", "C", "mod_c", "r"),
	}
	rels := []Relationship{
		{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "r2", FromID: "b", ToID: "c", Kind: "CALLS"},
	}
	res := RunModuleAlgorithms(entities, rels)
	if len(res.SCCs) != 0 {
		t.Fatalf("expected no SCCs in a DAG, got %d", len(res.SCCs))
	}
	for m, s := range res.SCCOf {
		if s != -1 {
			t.Fatalf("module %s should have SCCOf=-1, got %d", m, s)
		}
	}
}

// TestSCCDetection_ThreeModuleCycle — directed triangle a→b→c→a.
func TestSCCDetection_ThreeModuleCycle(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		makeModEntity("a", "A", "mod_a", "r"),
		makeModEntity("b", "B", "mod_b", "r"),
		makeModEntity("c", "C", "mod_c", "r"),
	}
	rels := []Relationship{
		{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "r2", FromID: "b", ToID: "c", Kind: "CALLS"},
		{ID: "r3", FromID: "c", ToID: "a", Kind: "CALLS"},
	}
	res := RunModuleAlgorithms(entities, rels)
	if len(res.SCCs) != 1 || res.SCCs[0].Size != 3 {
		t.Fatalf("expected 1 SCC of size 3, got %+v", res.SCCs)
	}
}

// TestCentrality_HubHasHighestPageRank — a hub module that everything
// depends on should sit at the top of the PageRank ranking.
func TestCentrality_HubHasHighestPageRank(t *testing.T) {
	t.Parallel()
	// hub is depended on by all four other modules.
	entities := []Entity{
		makeModEntity("h", "H", "hub", "r"),
		makeModEntity("a", "A", "mod_a", "r"),
		makeModEntity("b", "B", "mod_b", "r"),
		makeModEntity("c", "C", "mod_c", "r"),
		makeModEntity("d", "D", "mod_d", "r"),
	}
	rels := []Relationship{
		{ID: "r1", FromID: "a", ToID: "h", Kind: "CALLS"},
		{ID: "r2", FromID: "b", ToID: "h", Kind: "CALLS"},
		{ID: "r3", FromID: "c", ToID: "h", Kind: "CALLS"},
		{ID: "r4", FromID: "d", ToID: "h", Kind: "CALLS"},
	}
	res := RunModuleAlgorithms(entities, rels)
	top := TopByPageRank(res.Centrality, 1)
	if len(top) != 1 || top[0].ModuleID != "hub" {
		t.Fatalf("expected hub at the top of PageRank, got %+v", top)
	}
	// Hub should also have InDegree = 4.
	for _, c := range res.Centrality {
		if c.ModuleID == "hub" {
			if c.InDegree != 4 {
				t.Fatalf("hub InDegree: want 4, got %d", c.InDegree)
			}
		}
	}
}

// TestCentrality_BottleneckHasHighestBetweenness — a module that sits on
// every shortest path between two clusters has the highest betweenness.
func TestCentrality_BottleneckHasHighestBetweenness(t *testing.T) {
	t.Parallel()
	// Topology: a → mid → b ; c → mid → d.  mid is the only path.
	entities := []Entity{
		makeModEntity("a", "A", "mod_a", "r"),
		makeModEntity("b", "B", "mod_b", "r"),
		makeModEntity("c", "C", "mod_c", "r"),
		makeModEntity("d", "D", "mod_d", "r"),
		makeModEntity("m", "M", "mid", "r"),
	}
	rels := []Relationship{
		{ID: "r1", FromID: "a", ToID: "m", Kind: "CALLS"},
		{ID: "r2", FromID: "m", ToID: "b", Kind: "CALLS"},
		{ID: "r3", FromID: "c", ToID: "m", Kind: "CALLS"},
		{ID: "r4", FromID: "m", ToID: "d", Kind: "CALLS"},
	}
	res := RunModuleAlgorithms(entities, rels)
	top := TopByBetweenness(res.Centrality, 1)
	if len(top) != 1 || top[0].ModuleID != "mid" {
		t.Fatalf("expected mid at top of betweenness, got %+v", top)
	}
}

// TestDeterminism — running twice on the same input must produce identical
// SCC IDs / member orders / score values. This is a regression guard against
// gonum's map-iteration noise leaking through (cf. issue #481).
func TestDeterminism(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		makeModEntity("a", "A", "mod_a", "r"),
		makeModEntity("b", "B", "mod_b", "r"),
		makeModEntity("c", "C", "mod_c", "r"),
		makeModEntity("d", "D", "mod_d", "r"),
	}
	rels := []Relationship{
		{ID: "1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "2", FromID: "b", ToID: "a", Kind: "CALLS"},
		{ID: "3", FromID: "b", ToID: "c", Kind: "CALLS"},
		{ID: "4", FromID: "c", ToID: "d", Kind: "CALLS"},
		{ID: "5", FromID: "d", ToID: "a", Kind: "CALLS"},
	}
	for i := 0; i < 5; i++ {
		r1 := RunModuleAlgorithms(entities, rels)
		r2 := RunModuleAlgorithms(entities, rels)
		if !reflect.DeepEqual(r1, r2) {
			t.Fatalf("non-deterministic results on iteration %d:\n%+v\n---\n%+v", i, r1, r2)
		}
	}
}
