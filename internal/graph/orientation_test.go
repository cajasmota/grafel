package graph

import (
	"strings"
	"testing"
)

// intp / fp are small helpers for the *int / *float64 Pass-4 attribute fields.
func intp(v int) *int       { return &v }
func fp(v float64) *float64 { return &v }

// buildOrientFixture constructs a tiny two-community graph with a known bridge,
// a peripheral->hub edge, a cross-layer edge, an ambiguous edge, and an isolated
// node — so every analysis branch is exercised by one fixture.
//
// Communities:
//
//	community 0: api_handler (hub), helper_a
//	community 1: data_repo (hub), helper_b
//
// bridge: api_handler -> data_repo crosses communities AND layers (api->data).
// peripheral: leaf -> api_handler (leaf has degree 1, api_handler is a hub).
// ambiguous: api_handler -> dynamic_target (confidence 0.4).
// isolated: orphan (no edges).
func buildOrientFixture() ([]Entity, []Relationship) {
	entities := []Entity{
		{ID: "api_handler", Name: "ApiHandler", Kind: "function", SourceFile: "src/api/handler.go",
			CommunityID: intp(0), Centrality: fp(0.90), PageRank: fp(0.30)},
		{ID: "data_repo", Name: "DataRepo", Kind: "function", SourceFile: "src/db/repository.go",
			CommunityID: intp(1), Centrality: fp(0.40), PageRank: fp(0.20)},
		{ID: "helper_a", Name: "HelperA", Kind: "function", SourceFile: "src/api/util.go",
			CommunityID: intp(0), Centrality: fp(0.05), PageRank: fp(0.05)},
		{ID: "helper_b", Name: "HelperB", Kind: "function", SourceFile: "src/db/util.go",
			CommunityID: intp(1), Centrality: fp(0.05), PageRank: fp(0.05)},
		{ID: "leaf", Name: "Leaf", Kind: "function", SourceFile: "src/api/leaf.go",
			CommunityID: intp(0), Centrality: fp(0.0), PageRank: fp(0.01)},
		{ID: "dynamic_target", Name: "DynamicTarget", Kind: "function", SourceFile: "src/api/dyn.go",
			CommunityID: intp(0), Centrality: fp(0.0), PageRank: fp(0.01)},
		{ID: "orphan", Name: "Orphan", Kind: "function", SourceFile: "src/misc/orphan.go",
			CommunityID: intp(-1), Centrality: fp(0.0), PageRank: fp(0.0)},
	}
	rels := []Relationship{
		// api_handler is the central hub: edges to helper_a, data_repo, helper_b, dyn.
		{FromID: "api_handler", ToID: "helper_a", Kind: "CALLS"},
		{FromID: "api_handler", ToID: "helper_b", Kind: "CALLS"},
		{FromID: "api_handler", ToID: "data_repo", Kind: "CALLS"}, // cross-community + cross-layer bridge
		{FromID: "data_repo", ToID: "helper_b", Kind: "CALLS"},
		{FromID: "leaf", ToID: "api_handler", Kind: "CALLS"}, // peripheral -> hub
		// ambiguous low-confidence edge.
		{FromID: "api_handler", ToID: "dynamic_target", Kind: "CALLS", Confidence: 0.4},
	}
	return entities, rels
}

func TestAnalyzeOrientation_KeyEntityRanking(t *testing.T) {
	entities, rels := buildOrientFixture()
	res := AnalyzeOrientation(entities, rels, DefaultOrientationOptions())

	if len(res.KeyEntities) == 0 {
		t.Fatal("expected key entities, got none")
	}
	// api_handler has the highest degree AND highest betweenness — must rank #1.
	if res.KeyEntities[0].ID != "api_handler" {
		t.Errorf("expected api_handler as top key entity, got %q (full: %+v)",
			res.KeyEntities[0].ID, res.KeyEntities)
	}
	// Scores must be monotonically non-increasing (sorted).
	for i := 1; i < len(res.KeyEntities); i++ {
		if res.KeyEntities[i].Score > res.KeyEntities[i-1].Score {
			t.Errorf("key entities not sorted by score at %d: %v > %v",
				i, res.KeyEntities[i].Score, res.KeyEntities[i-1].Score)
		}
	}
	// Isolated node must NOT appear as a key entity (degree 0).
	for _, k := range res.KeyEntities {
		if k.ID == "orphan" {
			t.Error("isolated node 'orphan' must not be ranked as a key entity")
		}
		if k.Degree <= 0 {
			t.Errorf("key entity %q has non-positive degree %d", k.ID, k.Degree)
		}
	}
}

func TestAnalyzeOrientation_CrossCommunityEdgeScoresHigh(t *testing.T) {
	entities, rels := buildOrientFixture()
	res := AnalyzeOrientation(entities, rels, DefaultOrientationOptions())

	if len(res.CrossCutEdges) == 0 {
		t.Fatal("expected cross-cutting edges, got none")
	}
	// The api_handler->data_repo edge bridges communities (0->1) AND layers
	// (api->data); it must be the highest-scoring cross-cutting edge.
	top := res.CrossCutEdges[0]
	if top.FromID != "api_handler" || top.ToID != "data_repo" {
		t.Errorf("expected api_handler->data_repo as top cross-cut edge, got %s->%s",
			top.FromID, top.ToID)
	}
	if !hasReason(top.Reasons, "bridges_communities") {
		t.Errorf("top edge missing bridges_communities reason: %v", top.Reasons)
	}
	var foundLayer bool
	for _, r := range top.Reasons {
		if strings.HasPrefix(r, "crosses_layer:") {
			foundLayer = true
		}
	}
	if !foundLayer {
		t.Errorf("top edge missing crosses_layer reason: %v", top.Reasons)
	}
	// Intra-community helper edges (api_handler->helper_a) must not appear with a
	// bridges_communities reason.
	for _, e := range res.CrossCutEdges {
		if e.FromID == "api_handler" && e.ToID == "helper_a" && hasReason(e.Reasons, "bridges_communities") {
			t.Error("intra-community edge wrongly flagged as bridging communities")
		}
	}
}

func TestAnalyzeOrientation_PeripheralToHub(t *testing.T) {
	entities, rels := buildOrientFixture()
	res := AnalyzeOrientation(entities, rels, DefaultOrientationOptions())
	var found bool
	for _, e := range res.CrossCutEdges {
		if e.FromID == "leaf" && e.ToID == "api_handler" {
			found = true
			if !hasReason(e.Reasons, "peripheral_to_hub") {
				t.Errorf("leaf->api_handler missing peripheral_to_hub reason: %v", e.Reasons)
			}
		}
	}
	if !found {
		t.Error("expected leaf->api_handler peripheral-to-hub edge in cross-cut output")
	}
}

func TestAnalyzeOrientation_QuestionsGenerated(t *testing.T) {
	entities, rels := buildOrientFixture()
	res := AnalyzeOrientation(entities, rels, DefaultOrientationOptions())

	if len(res.Questions) == 0 {
		t.Fatal("expected orientation questions, got none")
	}
	sources := map[string]bool{}
	var ambiguousMentionsDyn, isolatedMentionsOrphan bool
	for _, q := range res.Questions {
		sources[q.Source] = true
		if q.Source == "ambiguous_edge" && containsAll(q.Entities, "dynamic_target") {
			ambiguousMentionsDyn = true
		}
		if q.Source == "isolated_node" && containsAll(q.Entities, "orphan") {
			isolatedMentionsOrphan = true
		}
		if strings.TrimSpace(q.Question) == "" {
			t.Error("generated an empty question")
		}
	}
	for _, want := range []string{"bridge_node", "ambiguous_edge", "isolated_node"} {
		if !sources[want] {
			t.Errorf("expected at least one question from source %q; got sources %v", want, sources)
		}
	}
	if !ambiguousMentionsDyn {
		t.Error("expected an ambiguous-edge question referencing dynamic_target")
	}
	if !isolatedMentionsOrphan {
		t.Error("expected an isolated-node question referencing orphan")
	}
}

func TestAnalyzeOrientation_Caps(t *testing.T) {
	entities, rels := buildOrientFixture()
	res := AnalyzeOrientation(entities, rels, OrientationOptions{
		TopEntities: 2, TopEdges: 1, MaxQuestions: 2,
	})
	if len(res.KeyEntities) > 2 {
		t.Errorf("TopEntities cap not honored: got %d", len(res.KeyEntities))
	}
	if len(res.CrossCutEdges) > 1 {
		t.Errorf("TopEdges cap not honored: got %d", len(res.CrossCutEdges))
	}
	if len(res.Questions) > 2 {
		t.Errorf("MaxQuestions cap not honored: got %d", len(res.Questions))
	}
}

func TestAnalyzeOrientation_Deterministic(t *testing.T) {
	entities, rels := buildOrientFixture()
	a := AnalyzeOrientation(entities, rels, DefaultOrientationOptions())
	b := AnalyzeOrientation(entities, rels, DefaultOrientationOptions())
	if len(a.KeyEntities) != len(b.KeyEntities) {
		t.Fatal("non-deterministic key-entity count")
	}
	for i := range a.KeyEntities {
		if a.KeyEntities[i].ID != b.KeyEntities[i].ID {
			t.Errorf("non-deterministic key-entity order at %d: %s vs %s",
				i, a.KeyEntities[i].ID, b.KeyEntities[i].ID)
		}
	}
	for i := range a.Questions {
		if a.Questions[i].Question != b.Questions[i].Question {
			t.Errorf("non-deterministic question order at %d", i)
		}
	}
}

func hasReason(rs []string, want string) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
