package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// #4244 (third fix) — the per-stage `$lookup` SCOPE.DataAccess node a consumer
// `find`s ("inspections.aggregate@L38#9 $lookup") must be traversable to its
// join target. The two PRIOR fixes emitted the node-anchored twin at EXTRACT
// time with a structural-ref STUB FromID (scope:dataaccess:...) and relied on
// the reference resolver to rewrite that stub to the node's graph id via
// byLocation[file][name]. That rewrite did NOT land on the node's id in
// production (the twin's FromID stayed a synthetic value ≠ the node id), so
// neighbors(<node>) returned empty live — twice. BOTH prior tests asserted only
// that "a JOINS_COLLECTION edge exists" and never that "an edge whose
// FromID == graph.EntityID(<the $lookup node>) exists" — that gap was the false
// pass.
//
// This fix abandons the stub+resolver entirely: the extract pass RECORDS join
// targets on the stage entity (props["from"] for the primary lookup,
// props["join_targets"] for the Python correlated nested froms) and a post-stamp
// pass (buildMongoAggStageJoinRels in cmd/archigraph) emits the twin edge with
// FromID = the stage node's already-stamped graph id. The two assertions below
// are unit-level guards on the engine-side contract that pass relies on; the
// FromID==node-id end-to-end assertion lives in
// cmd/archigraph/index_mongoagg_4244_test.go.

// TestMongoAggPy_StageRecordsJoinTargetsOnEntity asserts the extract pass NO
// LONGER emits a stub-FromID node-anchored twin, and instead records the join
// target(s) on the stage entity so the post-stamp pass can emit a real-id edge.
func TestMongoAggPy_StageRecordsJoinTargetsOnEntity(t *testing.T) {
	src := `
from pymongo import MongoClient

class InspectionService:
    def with_history(self, db):
        pipeline = [
            {"$lookup": {
                "from": "inspections_history",
                "localField": "_id",
                "foreignField": "inspection_id",
                "as": "history",
            }},
            {"$match": {"status": "open"}},
        ]
        return db.inspections.aggregate(pipeline)
`
	funcs := indexEnclosingFunctions("python", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanPythonMongoAggregation(src, funcs, "svc/agg.py", "python", nil,
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)

	lookupEnt := pyFindStage(ents, "$lookup")
	if lookupEnt == nil {
		t.Fatalf("expected a $lookup SCOPE.DataAccess stage entity; ents=%+v", ents)
	}
	if lookupEnt.Kind != string(types.EntityKindDataAccess) {
		t.Fatalf("stage kind = %q, want SCOPE.DataAccess", lookupEnt.Kind)
	}
	// The stage entity carries the join target so the post-stamp pass can emit
	// the node-anchored twin with a real FromID.
	if lookupEnt.Properties["from"] != "inspections_history" {
		t.Fatalf("stage entity props[from] = %q, want inspections_history",
			lookupEnt.Properties["from"])
	}

	// REGRESSION GUARD: the collection-anchored edge must still fire.
	classEdge := pyFindJoinTo(rels, capitalisedSingular("inspections_history"))
	if classEdge == nil {
		t.Fatalf("expected a collection-anchored JOINS_COLLECTION edge to inspections_history; rels=%+v", rels)
	}
	if classEdge.FromID != "Class:"+capitalisedSingular("inspections") {
		t.Fatalf("collection-anchored FromID = %q, want Class:%s",
			classEdge.FromID, capitalisedSingular("inspections"))
	}

	// THE ANTI-FALSE-PASS GUARD: the extract pass must NOT emit any
	// node-anchored twin at extract time — neither a `scope:` stub (the failed
	// approach) NOR a hex id (the node id is unknown here). The twin is emitted
	// exclusively by the post-stamp pass. Asserting zero stage-node edges here
	// proves we abandoned the resolver-dependent emission.
	for i := range rels {
		r := &rels[i]
		if r.Properties != nil && r.Properties["anchor"] == "stage_node" {
			t.Fatalf("extract pass emitted a node-anchored twin (FromID=%q) — it must be emitted post-stamp, not here", r.FromID)
		}
	}
}

// TestMongoAggPy_NestedCorrelatedFromsRecordedAsJoinTargets asserts a correlated
// `$lookup` carrying a nested sub-pipeline `$lookup` records BOTH the top-level
// and the nested `from` so the post-stamp pass emits one node-anchored twin per
// target (all anchored on the SAME stage node).
func TestMongoAggPy_NestedCorrelatedFromsRecordedAsJoinTargets(t *testing.T) {
	src := `
from pymongo import MongoClient

def q(db):
    pipeline = [
        {"$lookup": {
            "from": "m_contracts",
            "as": "contract",
            "pipeline": [
                {"$lookup": {"from": "m_group_device_settings", "as": "gds"}},
            ],
        }},
    ]
    return db.inspections.aggregate(pipeline)
`
	funcs := indexEnclosingFunctions("python", src)
	var ents []types.EntityRecord
	scanPythonMongoAggregation(src, funcs, "svc/agg.py", "python", nil,
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) {},
	)
	lookupEnt := pyFindStage(ents, "$lookup")
	if lookupEnt == nil {
		t.Fatalf("expected a $lookup stage entity; ents=%+v", ents)
	}
	if lookupEnt.Properties["from"] != "m_contracts" {
		t.Fatalf("props[from] = %q, want m_contracts", lookupEnt.Properties["from"])
	}
	if got := lookupEnt.Properties[mongoAggStageJoinTargetsKey]; got != "m_group_device_settings" {
		t.Fatalf("props[%s] = %q, want m_group_device_settings (nested correlated from)",
			mongoAggStageJoinTargetsKey, got)
	}
}

// TestMongoAggStageNodeJoinRel_FromIDIsTheNodeID asserts the edge builder uses a
// first-class FromID equal to the node id passed in (no stub), and the canonical
// Class:<from> ToID.
func TestMongoAggStageNodeJoinRel_FromIDIsTheNodeID(t *testing.T) {
	nodeID := "deadbeefdeadbeef" // stands in for graph.EntityID(...) output
	rel := mongoAggStageNodeJoinRel(nodeID, "m_contracts")
	if rel.FromID != nodeID {
		t.Fatalf("FromID = %q, want the node id %q (first-class, not a stub)", rel.FromID, nodeID)
	}
	if rel.ToID != "Class:"+capitalisedSingular("m_contracts") {
		t.Fatalf("ToID = %q, want Class:%s", rel.ToID, capitalisedSingular("m_contracts"))
	}
	if rel.Kind != string(types.RelationshipKindJoinsCollection) {
		t.Fatalf("Kind = %q, want JOINS_COLLECTION", rel.Kind)
	}
	if rel.Properties["anchor"] != "stage_node" {
		t.Fatalf("anchor prop = %q, want stage_node", rel.Properties["anchor"])
	}
}

// TestMongoAggAddStageJoinTarget_DedupAndOrder guards the join-targets recorder.
func TestMongoAggAddStageJoinTarget_DedupAndOrder(t *testing.T) {
	props := map[string]string{}
	mongoAggAddStageJoinTarget(props, "a")
	mongoAggAddStageJoinTarget(props, "b")
	mongoAggAddStageJoinTarget(props, "a") // dup — ignored
	mongoAggAddStageJoinTarget(props, "")  // empty — ignored
	if got := props[mongoAggStageJoinTargetsKey]; got != "a,b" {
		t.Fatalf("join_targets = %q, want a,b", got)
	}
}
