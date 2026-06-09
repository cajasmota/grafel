package mcp

import (
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func ip(v int) *int           { return &v }
func f64p(v float64) *float64 { return &v }

// Test_handleOrient_4290 verifies the archigraph_orient MCP handler returns the
// three orientation parts (key entities, cross-cutting edges, questions) for a
// loaded repo, reading the Pass-4 attributes off the in-memory doc.
func Test_handleOrient_4290(t *testing.T) {
	doc := &graph.Document{
		Repo: "demo",
		Entities: []graph.Entity{
			{ID: "api", Name: "Api", Kind: "function", SourceFile: "src/api/h.go",
				CommunityID: ip(0), Centrality: f64p(0.9), PageRank: f64p(0.3)},
			{ID: "repo", Name: "Repo", Kind: "function", SourceFile: "src/db/r.go",
				CommunityID: ip(1), Centrality: f64p(0.4), PageRank: f64p(0.2)},
			{ID: "h", Name: "Helper", Kind: "function", SourceFile: "src/api/u.go",
				CommunityID: ip(0), Centrality: f64p(0.05), PageRank: f64p(0.05)},
			{ID: "orphan", Name: "Orphan", Kind: "function", SourceFile: "src/x/o.go",
				CommunityID: ip(-1)},
		},
		Relationships: []graph.Relationship{
			{FromID: "api", ToID: "h", Kind: "CALLS"},
			{FromID: "api", ToID: "repo", Kind: "CALLS"},                  // cross-community bridge
			{FromID: "api", ToID: "repo", Kind: "CALLS", Confidence: 0.4}, // ambiguous dup-kind ok via diff conf
		},
	}
	srv := newTestServer(t, doc)
	text := callEndpointToolText(t, srv.handleOrient, map[string]any{"group": "test"})

	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, text)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 repo block, got %d: %s", len(got), text)
	}
	block := got[0]
	if block["repo"] != "demo" {
		t.Errorf("wrong repo: %v", block["repo"])
	}
	ke, _ := block["key_entities"].([]any)
	if len(ke) == 0 {
		t.Fatal("expected key_entities")
	}
	if first := ke[0].(map[string]any); first["id"] != "api" {
		t.Errorf("expected 'api' as top key entity, got %v", first["id"])
	}
	cc, _ := block["cross_cutting_edges"].([]any)
	if len(cc) == 0 {
		t.Error("expected cross_cutting_edges")
	}
	qs, _ := block["orientation_questions"].([]any)
	if len(qs) == 0 {
		t.Error("expected orientation_questions")
	}
}

// Test_handleOrient_4290_Caps verifies the cap args narrow the output.
func Test_handleOrient_4290_Caps(t *testing.T) {
	doc := &graph.Document{
		Repo: "demo",
		Entities: []graph.Entity{
			{ID: "a", Name: "A", SourceFile: "p/a.go", CommunityID: ip(0), Centrality: f64p(0.9)},
			{ID: "b", Name: "B", SourceFile: "p/b.go", CommunityID: ip(1), Centrality: f64p(0.5)},
			{ID: "c", Name: "C", SourceFile: "p/c.go", CommunityID: ip(0), Centrality: f64p(0.1)},
		},
		Relationships: []graph.Relationship{
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			{FromID: "a", ToID: "c", Kind: "CALLS"},
			{FromID: "b", ToID: "c", Kind: "CALLS"},
		},
	}
	srv := newTestServer(t, doc)
	text := callEndpointToolText(t, srv.handleOrient, map[string]any{
		"group":        "test",
		"top_entities": 1,
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, text)
	}
	ke, _ := got[0]["key_entities"].([]any)
	if len(ke) != 1 {
		t.Errorf("top_entities=1 should cap to 1 key entity, got %d", len(ke))
	}
}
