package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// #4244 RE-FIX — the original #4247 fixture used a SINGLE `$lookup`, so it
// never exercised the production failure observed live on upvate-core
// (building/service.py): a file with SEVERAL `coll.aggregate(...)` calls on the
// SAME collection. Each call independently restarts pipeline-stage indexing at
// #0, and the graph entity ID is graph.EntityID(repo, kind, Name, file) — which
// IGNORES StartLine AND the looked-up `from`. With the old Name scheme
// (`<coll>.aggregate#<idx> <op>`), stage #N of call A and stage #N of call B
// produced the IDENTICAL Name → IDENTICAL graph ID, COLLAPSING two distinct
// `$lookup` stages (different `from`) into ONE node. That node then carried the
// node-anchored JOINS_COLLECTION twins of BOTH stages, so neighbors(node)
// returned a CROSS-STAGE mix — and the `find`-able stage node was effectively
// unusable. The single-lookup fixture could never catch this.
//
// This test reproduces the REAL shape: two aggregations on `db.inspections`,
// each with a `$lookup` at the SAME stage index but a DISTINCT `from`. It
// stamps IDs exactly like production (graph.EntityID) and runs the REAL
// resolver, then asserts:
//
//   - the two `$lookup` stages occupy DISTINCT graph nodes (no collapse), and
//   - each `$lookup` node has a JOINS_COLLECTION edge to ITS OWN `from`
//     collection and to NO OTHER collection (no cross-stage mis-link).
//
// NON-VACUOUS PROOF: under the pre-fix Name scheme the two stages share an ID,
// so the distinctness assertion fails AND each surviving node carries BOTH
// joins — the assertions below fail on origin/main and pass only after the
// `@L<callLine>` Name segment is added (mongoAggStageName).
func TestMongoAggPy_MultiLookupSameCollection_NoCollapse_4244(t *testing.T) {
	src := `
from pymongo import MongoClient

class InspectionService:
    def report_a(self, db):
        pipeline = [{"$match": {"x": 1}}, {"$lookup": {"from": "buildings", "as": "b"}}]
        return db.inspections.aggregate(pipeline)

    def report_b(self, db):
        pipeline = [{"$match": {"y": 2}}, {"$lookup": {"from": "contracts", "as": "c"}}]
        return db.inspections.aggregate(pipeline)
`
	const path = "core/services/inspection/service.py"
	const repoTag = "upvate-core"

	funcs := indexEnclosingFunctions("python", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanPythonMongoAggregation(src, funcs, path, "python", nil,
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)

	// Stamp IDs exactly as production does (cmd/archigraph stampEntityIDs).
	for i := range ents {
		if ents[i].Name == "" {
			continue
		}
		ents[i].ID = graph.EntityID(repoTag, ents[i].Kind, ents[i].Name, ents[i].SourceFile)
	}

	// Collect the two $lookup stage nodes by their `from` (via the looked-up
	// collection recorded on the node-anchored twin before resolution).
	type lookupNode struct {
		id   string
		from string // the Class the node SHOULD (and only should) join
	}
	var lookups []lookupNode
	for i := range ents {
		e := &ents[i]
		if e.Subtype != "$lookup" {
			continue
		}
		// Find the node-anchored twin whose stub names THIS entity, to learn
		// its intended `from` target.
		var to string
		for j := range rels {
			r := &rels[j]
			if r.Properties["anchor"] != "stage_node" {
				continue
			}
			// The stub FromID ends with ":<entity Name>".
			if len(r.FromID) >= len(e.Name) && r.FromID[len(r.FromID)-len(e.Name):] == e.Name {
				to = r.ToID
			}
		}
		lookups = append(lookups, lookupNode{id: e.ID, from: to})
	}
	if len(lookups) != 2 {
		t.Fatalf("expected 2 $lookup stage entities, got %d (ents=%+v)", len(lookups), ents)
	}

	// (1) NO COLLAPSE: the two stages must have DISTINCT graph IDs. Pre-fix
	// they share an ID because Name (= aggregate#1 $lookup for both) + file +
	// kind are identical.
	if lookups[0].id == lookups[1].id {
		t.Fatalf("COLLAPSE: the two $lookup stages share graph ID %s — distinct stages merged into one node (pre-fix #4244 bug)", lookups[0].id)
	}
	if lookups[0].from == lookups[1].from || lookups[0].from == "" || lookups[1].from == "" {
		t.Fatalf("test setup: expected two distinct non-empty `from` targets, got %q and %q", lookups[0].from, lookups[1].from)
	}

	// Run the REAL resolver over the emitted edges.
	idx := resolve.BuildIndex(ents)
	resolve.References(rels, idx)

	// (2) NO CROSS-STAGE MIS-LINK: each $lookup node's outgoing
	// node-anchored JOINS_COLLECTION edges must point ONLY at its own `from`.
	for _, ln := range lookups {
		var targets []string
		for j := range rels {
			r := &rels[j]
			if r.Kind != string(types.RelationshipKindJoinsCollection) {
				continue
			}
			if r.Properties["anchor"] != "stage_node" {
				continue
			}
			if r.FromID == ln.id {
				targets = append(targets, r.ToID)
			}
		}
		if len(targets) == 0 {
			t.Fatalf("ISOLATED: $lookup node %s has no outgoing node-anchored JOINS_COLLECTION (expected -> %s)", ln.id, ln.from)
		}
		for _, tgt := range targets {
			if tgt != ln.from {
				t.Fatalf("CROSS-STAGE MIS-LINK: $lookup node %s joins %s but should join ONLY %s", ln.id, tgt, ln.from)
			}
		}
	}
}
