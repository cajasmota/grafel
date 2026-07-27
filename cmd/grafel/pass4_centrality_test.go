package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// zeroBetweennessDoc is a path A→B→C plus an isolated D. Only B sits on a
// shortest path, so A, C and D all have betweenness 0 — and since #5954 they
// are ABSENT from AlgorithmResults.Centrality rather than carrying an explicit
// zero.
func zeroBetweennessDoc() *graph.Document {
	return &graph.Document{
		Repo: "pass4-zero",
		Entities: []graph.Entity{
			{ID: "A", Name: "A", Kind: "function"},
			{ID: "B", Name: "B", Kind: "function"},
			{ID: "C", Name: "C", Kind: "function"},
			{ID: "D", Name: "D", Kind: "function"},
		},
		Relationships: []graph.Relationship{
			{FromID: "A", ToID: "B", Kind: "CALLS"},
			{FromID: "B", ToID: "C", Kind: "CALLS"},
		},
	}
}

// TestPass4Attachment_ZeroBetweennessKeepsExplicitCentrality pins the
// serialisation boundary #5954 depends on.
//
// Entity.Centrality is *float64 with `json:",omitempty"`: a nil pointer DROPS
// the key from graph.json, a pointer-to-zero emits `"centrality": 0`. Before
// #5954 every entity in the pass had an explicit 0 in the result map, so the
// attachment loop's `if c, ok := res.Centrality[e.ID]; ok` was always true. The
// map is now sparse, so the loop keys off PageRank membership (still dense over
// the entity set) and reads centrality with the zero default.
//
// Reverting that to a Centrality comma-ok lookup would silently drop
// `"centrality": 0` for every zero-betweenness entity — ~9% of a real corpus.
// This test fails if that happens.
func TestPass4Attachment_ZeroBetweennessKeepsExplicitCentrality(t *testing.T) {
	doc := zeroBetweennessDoc()
	(&Indexer{}).runPass4Algorithms(doc)

	byID := map[string]*graph.Entity{}
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}

	// Sanity: the fixture really does produce a sparse Centrality map, i.e. the
	// zero-valued entities below are absent from it rather than present-with-0.
	if b := byID["B"]; b == nil || b.Centrality == nil || *b.Centrality <= 0 {
		t.Fatalf("B must carry a positive betweenness (it is on the A→C path); got %v", b.Centrality)
	}

	for _, id := range []string{"A", "C", "D"} {
		e := byID[id]
		if e == nil {
			t.Fatalf("entity %s missing", id)
		}
		if e.Centrality == nil {
			t.Errorf("%s.Centrality is nil — a zero-betweenness entity lost its explicit 0, "+
				"so graph.json will omit the \"centrality\" key it has always emitted", id)
			continue
		}
		if *e.Centrality != 0 {
			t.Errorf("%s.Centrality = %v, want 0", id, *e.Centrality)
		}
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		if !strings.Contains(string(raw), `"centrality":0`) {
			t.Errorf("%s serialises without \"centrality\":0 — %s", id, raw)
		}
	}
}
