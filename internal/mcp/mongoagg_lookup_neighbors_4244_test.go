package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestMongoAggLookupNode_NeighborsSurfaceJoin_4244 drives the EXACT adjacency
// path the live neighbors()/find-callees query uses (buildAdjacency(...).
// Outgoing(nodeID)) and asserts the node-anchored JOINS_COLLECTION twin surfaces
// FROM the `$lookup` DataAccess node id.
//
// This is the consumer-side half of the #4244 fix: the indexer now emits the
// twin with FromID = graph.EntityID(repo, "SCOPE.DataAccess", name, file) (the
// stage node's own graph id). buildAdjacency keys out-edges on FromID, so a
// consumer that `find`s the `$lookup` node and asks for its neighbors must get
// the join target. The two prior fixes left the twin's FromID a synthetic stub
// that did NOT equal this id, so Outgoing(nodeID) was empty live — twice.
func TestMongoAggLookupNode_NeighborsSurfaceJoin_4244(t *testing.T) {
	const (
		repo = "acme-core"
		file = "core/services/building/service.py"
		// Mirror the live node name shape produced by mongoAggStageName:
		// "<coll>.aggregate@L<line>#<idx> $lookup".
		nodeName = "inspections.aggregate@L38#9 $lookup"
		kind     = "SCOPE.DataAccess"
	)
	// The node id computed exactly as the indexer's stampEntityIDs does.
	nodeID := graph.EntityID(repo, kind, nodeName, file)

	doc := &graph.Document{
		Repo: repo,
		Entities: []graph.Entity{
			{ID: nodeID, Kind: kind, Subtype: "$lookup", Name: nodeName, SourceFile: file},
			{ID: "Class:M_contract", Kind: "Class", Name: "M_contract"},
		},
		Relationships: []graph.Relationship{
			// Node-anchored twin: FromID == the $lookup node's graph id.
			graph.Relationship{
				FromID: nodeID,
				ToID:   "Class:M_contract",
				Kind:   "JOINS_COLLECTION",
			}.WithProperties(map[string]string{"anchor": "stage_node"}),
		},
	}

	a := buildAdjacency(doc, repo)
	out := a.Outgoing(nodeID)
	if len(out) == 0 {
		t.Fatalf("neighbors($lookup node %s) is EMPTY — the node is isolated from its join target (the live #4244 bug)", nodeID)
	}
	var found bool
	for _, e := range out {
		if e.kind == "JOINS_COLLECTION" && e.target == "Class:M_contract" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Outgoing(%s) = %+v; want a JOINS_COLLECTION edge -> Class:M_contract", nodeID, out)
	}
}
