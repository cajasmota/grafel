package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// #4244 RE-FIX — a file with SEVERAL `coll.aggregate(...)` calls on the SAME
// collection. Each call independently restarts pipeline-stage indexing at #0,
// and the graph entity ID is graph.EntityID(repo, kind, Name, file) — which
// IGNORES StartLine AND the looked-up `from`. With a Name scheme that omitted
// the call line, stage #N of call A and stage #N of call B produced the
// IDENTICAL Name → IDENTICAL graph ID, COLLAPSING two distinct `$lookup` stages
// (different `from`) into ONE node, which then carried BOTH stages' joins —
// neighbors(node) returned a CROSS-STAGE mix.
//
// This test reproduces the REAL shape (two aggregations on `db.inspections`,
// each with a `$lookup` at the same stage index but a DISTINCT `from`), stamps
// IDs exactly like production (graph.EntityID), then emits the node-anchored
// twins the SAME way the post-stamp pass does (FromID = the stamped node id, via
// MongoAggStageNodeJoinRel) and asserts:
//
//   - the two `$lookup` stages occupy DISTINCT graph nodes (no collapse), and
//   - each `$lookup` node has a JOINS_COLLECTION edge whose FromID == that node's
//     id to ITS OWN `from` collection and to NO OTHER (no cross-stage mis-link).
//
// NON-VACUOUS: under a Name scheme without the `@L<callLine>` segment the two
// stages share an ID, so the distinctness assertion fails AND the surviving
// node carries BOTH joins. The FromID==node-id assertion (not merely
// "an edge exists") is what makes this catch the live isolation bug.
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
	scanPythonMongoAggregation(src, funcs, path, "python", nil,
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) {},
	)

	// Stamp IDs exactly as production does (cmd/archigraph stampEntityIDs).
	for i := range ents {
		if ents[i].Name == "" {
			continue
		}
		ents[i].ID = graph.EntityID(repoTag, ents[i].Kind, ents[i].Name, ents[i].SourceFile)
	}

	// Emit the node-anchored twins exactly as buildMongoAggStageJoinRels does:
	// FromID = the stamped stage node id, one edge per recorded `from`.
	type lookupNode struct {
		id   string
		from string // the Class the node SHOULD (and only should) join
	}
	var lookups []lookupNode
	var rels []types.RelationshipRecord
	for i := range ents {
		e := &ents[i]
		if e.Subtype != "$lookup" {
			continue
		}
		from := e.Properties["from"]
		if from == "" {
			t.Fatalf("stage %q has empty props[from]", e.Name)
		}
		lookups = append(lookups, lookupNode{id: e.ID, from: "Class:" + capitalisedSingular(from)})
		rels = append(rels, MongoAggStageNodeJoinRel(e.ID, from))
	}
	if len(lookups) != 2 {
		t.Fatalf("expected 2 $lookup stage entities, got %d (ents=%+v)", len(lookups), ents)
	}

	// (1) NO COLLAPSE: the two stages must have DISTINCT graph IDs.
	if lookups[0].id == lookups[1].id {
		t.Fatalf("COLLAPSE: the two $lookup stages share graph ID %s — distinct stages merged into one node (pre-fix #4244 bug)", lookups[0].id)
	}
	if lookups[0].from == lookups[1].from {
		t.Fatalf("test setup: expected two distinct `from` targets, got %q and %q", lookups[0].from, lookups[1].from)
	}

	// (2) Each $lookup node's outgoing node-anchored JOINS_COLLECTION edge must
	// have FromID == that node's graph id and point ONLY at its own `from`.
	for _, ln := range lookups {
		var targets []string
		for j := range rels {
			r := &rels[j]
			if r.Kind != string(types.RelationshipKindJoinsCollection) {
				continue
			}
			if r.FromID != ln.id {
				continue // FromID MUST be the node id — the whole point of #4244.
			}
			targets = append(targets, r.ToID)
		}
		if len(targets) == 0 {
			t.Fatalf("ISOLATED: $lookup node %s has no outgoing JOINS_COLLECTION with FromID==node-id (expected -> %s)", ln.id, ln.from)
		}
		for _, tgt := range targets {
			if tgt != ln.from {
				t.Fatalf("CROSS-STAGE MIS-LINK: $lookup node %s joins %s but should join ONLY %s", ln.id, tgt, ln.from)
			}
		}
	}
}
