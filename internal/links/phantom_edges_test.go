package links

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestPromoteToPhantomEdges_BasicHTTP verifies that a CALLS/http Link
// between two repos produces one phantom CALLS relationship on the
// source repo's document.
func TestPromoteToPhantomEdges_BasicHTTP(t *testing.T) {
	docA := &graph.Document{Repo: "fixture-a"}
	docB := &graph.Document{
		Repo: "fixture-b",
		Entities: []graph.Entity{
			{ID: "caller1", Name: "fetchUsers", Kind: "SCOPE.Function"},
		},
	}

	links := []Link{
		{
			ID:       "link1",
			Source:   "fixture-b::caller1",
			Target:   "fixture-a::handler1",
			Relation: RelationCalls,
			Method:   MethodHTTP,
		},
	}

	docs := map[string]*graph.Document{
		"fixture-a": docA,
		"fixture-b": docB,
	}

	added, err := PromoteToPhantomEdges(links, docs, "test-group")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}

	// docB should have the phantom edge.
	if len(docB.Relationships) != 1 {
		t.Fatalf("docB.Relationships len = %d, want 1", len(docB.Relationships))
	}
	rel := docB.Relationships[0]
	if rel.Kind != "CALLS" {
		t.Errorf("Kind = %q, want CALLS", rel.Kind)
	}
	if rel.FromID != "caller1" {
		t.Errorf("FromID = %q, want caller1", rel.FromID)
	}
	if rel.ToID != "handler1" {
		t.Errorf("ToID = %q, want handler1", rel.ToID)
	}
	if rel.PropGet("cross_repo") != "true" {
		t.Errorf("cross_repo = %q, want true", rel.PropGet("cross_repo"))
	}
	if rel.PropGet("target_repo") != "fixture-a" {
		t.Errorf("target_repo = %q, want fixture-a", rel.PropGet("target_repo"))
	}
	if rel.PropGet("link_method") != MethodHTTP {
		t.Errorf("link_method = %q, want %s", rel.PropGet("link_method"), MethodHTTP)
	}
	if rel.PropGet("via") == "" {
		t.Errorf("via property is empty")
	}
}

// TestPromoteToPhantomEdges_KafkaAndWS verifies that kafka_topic and
// ws_channel method links are also promoted.
func TestPromoteToPhantomEdges_KafkaAndWS(t *testing.T) {
	docSrc := &graph.Document{Repo: "src"}
	docs := map[string]*graph.Document{"src": docSrc, "tgt": {Repo: "tgt"}}

	links := []Link{
		{ID: "k1", Source: "src::pub1", Target: "tgt::sub1", Relation: RelationCalls, Method: "kafka_topic"},
		{ID: "w1", Source: "src::ws1", Target: "tgt::ws2", Relation: RelationCalls, Method: "ws_channel"},
	}

	added, err := PromoteToPhantomEdges(links, docs, "grp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 2 {
		t.Errorf("added = %d, want 2 (kafka + ws)", added)
	}
	methods := make(map[string]bool)
	for _, r := range docSrc.Relationships {
		methods[r.PropGet("link_method")] = true
	}
	if !methods["kafka_topic"] {
		t.Errorf("expected kafka_topic phantom edge, got %v", methods)
	}
	if !methods["ws_channel"] {
		t.Errorf("expected ws_channel phantom edge, got %v", methods)
	}
}

// TestPromoteToPhantomEdges_SkipsNonCalls verifies that IMPORTS,
// SHARED_LABEL, and STRING_MATCH links are not promoted.
func TestPromoteToPhantomEdges_SkipsNonCalls(t *testing.T) {
	docSrc := &graph.Document{Repo: "src"}
	docs := map[string]*graph.Document{"src": docSrc, "tgt": {Repo: "tgt"}}

	links := []Link{
		{ID: "i1", Source: "src::e1", Target: "tgt::e2", Relation: RelationImports, Method: MethodImport},
		{ID: "l1", Source: "src::e1", Target: "tgt::e3", Relation: RelationSharedLabel, Method: MethodLabelMatch},
		{ID: "s1", Source: "src::e1", Target: "tgt::e4", Relation: RelationStringMatch, Method: MethodString},
	}

	added, err := PromoteToPhantomEdges(links, docs, "grp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 0 {
		t.Errorf("added = %d, want 0 for non-CALLS links", added)
	}
	if len(docSrc.Relationships) != 0 {
		t.Errorf("expected no phantom edges for non-CALLS links, got %d", len(docSrc.Relationships))
	}
}

// TestPromoteToPhantomEdges_Dedup verifies that duplicate links (same
// Source, Target, Method) produce only one phantom edge.
func TestPromoteToPhantomEdges_Dedup(t *testing.T) {
	docSrc := &graph.Document{Repo: "src"}
	docs := map[string]*graph.Document{"src": docSrc, "tgt": {Repo: "tgt"}}

	links := []Link{
		{ID: "a1", Source: "src::e1", Target: "tgt::e2", Relation: RelationCalls, Method: MethodHTTP},
		{ID: "a2", Source: "src::e1", Target: "tgt::e2", Relation: RelationCalls, Method: MethodHTTP}, // dup
	}

	added, err := PromoteToPhantomEdges(links, docs, "grp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 1 {
		t.Errorf("added = %d, want 1 (deduped)", added)
	}
	if len(docSrc.Relationships) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(docSrc.Relationships))
	}
}

// TestPromoteToPhantomEdges_SelfPairSkipped verifies that same-repo links
// are not promoted (they're intra-repo, not cross-repo).
func TestPromoteToPhantomEdges_SelfPairSkipped(t *testing.T) {
	docSrc := &graph.Document{Repo: "src"}
	docs := map[string]*graph.Document{"src": docSrc}

	links := []Link{
		{ID: "s1", Source: "src::e1", Target: "src::e2", Relation: RelationCalls, Method: MethodHTTP},
	}

	added, _ := PromoteToPhantomEdges(links, docs, "grp")
	if added != 0 {
		t.Errorf("added = %d, want 0 for same-repo link", added)
	}
}

// TestPromoteToPhantomEdges_MissingSourceDocSkipped verifies that when
// the source repo's doc is not in the map, the link is silently skipped.
func TestPromoteToPhantomEdges_MissingSourceDocSkipped(t *testing.T) {
	// Only tgt present, src missing.
	docs := map[string]*graph.Document{"tgt": {Repo: "tgt"}}

	links := []Link{
		{ID: "m1", Source: "src::e1", Target: "tgt::e2", Relation: RelationCalls, Method: MethodHTTP},
	}

	added, err := PromoteToPhantomEdges(links, docs, "grp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 0 {
		t.Errorf("added = %d, want 0 when source doc missing", added)
	}
}

// TestPromoteToPhantomEdges_Determinism verifies that running the function
// twice produces the same set of phantom edges (no random ordering).
func TestPromoteToPhantomEdges_Determinism(t *testing.T) {
	makeLinks := func() []Link {
		return []Link{
			{ID: "l3", Source: "src::e3", Target: "tgt::h3", Relation: RelationCalls, Method: MethodHTTP},
			{ID: "l1", Source: "src::e1", Target: "tgt::h1", Relation: RelationCalls, Method: MethodHTTP},
			{ID: "l2", Source: "src::e2", Target: "tgt::h2", Relation: RelationCalls, Method: MethodHTTP},
		}
	}
	makeDocs := func() map[string]*graph.Document {
		return map[string]*graph.Document{
			"src": {Repo: "src"},
			"tgt": {Repo: "tgt"},
		}
	}

	docs1 := makeDocs()
	docs2 := makeDocs()
	PromoteToPhantomEdges(makeLinks(), docs1, "grp") //nolint:errcheck
	PromoteToPhantomEdges(makeLinks(), docs2, "grp") //nolint:errcheck

	rels1 := docs1["src"].Relationships
	rels2 := docs2["src"].Relationships
	if len(rels1) != len(rels2) {
		t.Fatalf("length mismatch: %d vs %d", len(rels1), len(rels2))
	}
	for i := range rels1 {
		if rels1[i].ID != rels2[i].ID {
			t.Errorf("rel[%d] ID mismatch: %s vs %s", i, rels1[i].ID, rels2[i].ID)
		}
		if rels1[i].FromID != rels2[i].FromID {
			t.Errorf("rel[%d] FromID mismatch: %s vs %s", i, rels1[i].FromID, rels2[i].FromID)
		}
	}
}
