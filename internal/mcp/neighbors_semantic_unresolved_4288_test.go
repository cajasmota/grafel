package mcp

// neighbors_semantic_unresolved_4288_test.go — #4288: grafel_neighbors must
// surface semantic edges (JOINS_COLLECTION etc.) that inspect's semantic_edges
// section already shows, INCLUDING when the far-side target is not a backing
// indexed entity (the real acme-core case: a DataAccess node JOINS_COLLECTION
// acme-core::Class:Inspection where Class:Inspection has no Entity record).
//
// Pre-fix neighbors(out)/find_callees silently dropped any out-edge whose
// ToID did not resolve in byID — so the JOINS_COLLECTION neighbour vanished
// while inspect still listed it. This mirrors the asymmetry #4242 fixed for the
// inbound side, but the bug here is the unresolved-target drop on the out side.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// makeJoinsCollectionUnresolvedDoc models the acme case: a DataAccess node
// with an outbound JOINS_COLLECTION edge to a class id that is NOT an indexed
// entity (no Entity record for "Class:Inspection").
func makeJoinsCollectionUnresolvedDoc() *graph.Document {
	return &graph.Document{
		Repo: "api",
		Entities: []graph.Entity{
			{ID: "da", Name: "InspectionDataAccess", Kind: "SCOPE.DataAccess", SourceFile: "da.ts", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			// far side "Class:Inspection" has no Entity record (unresolved).
			graph.Relationship{ID: "j1", FromID: "da", ToID: "Class:Inspection", Kind: "JOINS_COLLECTION"}.WithProperties(map[string]string{"line": "9"}),
		},
	}
}

// TestNeighbors_4288_SurfacesUnresolvedJoinsCollection asserts neighbors(both)
// surfaces the JOINS_COLLECTION edge with edge_kind=JOINS_COLLECTION even though
// the target is not a backing indexed entity.
func TestNeighbors_4288_SurfacesUnresolvedJoinsCollection(t *testing.T) {
	srv := newTestServer(t, makeJoinsCollectionUnresolvedDoc())
	out := callNeighbors3834(t, srv, "da", "out")
	raw, _ := out["callees"].([]any)
	var got map[string]any
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if m["edge_kind"] == "JOINS_COLLECTION" {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("neighbors(out) did not surface JOINS_COLLECTION edge; callees=%v", raw)
	}
	if got["id"] != "api::Class:Inspection" {
		t.Errorf("JOINS_COLLECTION neighbour id = %v, want api::Class:Inspection", got["id"])
	}
}

// TestNeighbors_4288_ResolvedSemanticTargetStillSurfaces guards the common case
// where the semantic-edge target IS an indexed entity — it must continue to
// surface with its semantic edge_kind.
func TestNeighbors_4288_ResolvedSemanticTargetStillSurfaces(t *testing.T) {
	srv := newTestServer(t, makeSemanticEdgeDoc())
	out := callNeighbors3834(t, srv, "model", "out")
	raw, _ := out["callees"].([]any)
	var got map[string]any
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == "api::orders" {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("neighbors(out) did not surface JOINS_COLLECTION neighbour orders; callees=%v", raw)
	}
	if got["edge_kind"] != "JOINS_COLLECTION" {
		t.Errorf("edge_kind = %v, want JOINS_COLLECTION", got["edge_kind"])
	}
}
