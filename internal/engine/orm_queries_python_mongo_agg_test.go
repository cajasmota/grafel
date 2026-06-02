package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runMongoAggPy drives scanPythonMongoAggregation over `src` and collects the
// emitted stage entities + join edges.
func runMongoAggPy(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	return runMongoAggPyXFile(t, src, nil)
}

// runMongoAggPyXFile drives the scan with an in-memory cross-file builder
// resolver (builderName → defining-module source). Pass nil to disable
// cross-file follow (same-file behaviour).
func runMongoAggPyXFile(
	t *testing.T, src string, modules map[string]string,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("python", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	var resolver mongoAggPyCrossFileResolver
	if modules != nil {
		resolver = func(name string) string { return modules[name] }
	}
	scanPythonMongoAggregation(src, funcs, "svc/agg.py", "python", resolver,
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)
	return ents, rels
}

func pyStageSubtypesInOrder(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		out = append(out, e.Subtype)
	}
	return out
}

func pyFindStage(ents []types.EntityRecord, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func pyFindJoinTo(rels []types.RelationshipRecord, toClass string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].Kind == string(types.RelationshipKindJoinsCollection) &&
			rels[i].ToID == "Class:"+toClass {
			return &rels[i]
		}
	}
	return nil
}

// THE important case — variable-bound pipeline, the legacy-Django/pymongo
// shape: three $lookups to specific `from` collections then a $match. Asserts
// the three JOINS_COLLECTION edges to the SPECIFIC collections, the stage
// entities and their order.
func TestMongoAggPy_VariableBound_ThreeLookups(t *testing.T) {
	src := `
import pymongo
from pymongo import MongoClient

def inspection_report(db):
    pipeline = [
        {"$lookup": {"from": "inspection_groups", "localField": "group_id", "foreignField": "_id", "as": "groups"}},
        {"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "devices"}},
        {"$lookup": {"from": "m_buildings", "localField": "building_id", "foreignField": "_id", "as": "buildings"}},
        {"$match": {"status": "active"}},
    ]
    return db.inspections.aggregate(pipeline)
`
	ents, rels := runMongoAggPy(t, src)

	// 3 join edges to the SPECIFIC from-collections.
	wantTo := []string{"inspection_groups", "m_devices", "m_buildings"}
	for _, coll := range wantTo {
		j := pyFindJoinTo(rels, capitalisedSingular(coll))
		if j == nil {
			t.Fatalf("expected JOINS_COLLECTION edge to %s; rels=%+v", coll, rels)
		}
		if j.FromID != "Class:"+capitalisedSingular("inspections") {
			t.Errorf("join from %s = %q, want aggregating collection inspections", coll, j.FromID)
		}
		if j.Properties["stage"] != "lookup" {
			t.Errorf("join to %s stage = %q, want lookup", coll, j.Properties["stage"])
		}
	}
	if n := len(rels); n != 3 {
		t.Fatalf("expected exactly 3 join edges, got %d: %+v", n, rels)
	}

	// Local/foreign field props captured on the first lookup edge.
	jg := pyFindJoinTo(rels, capitalisedSingular("inspection_groups"))
	if jg.Properties["local_field"] != "group_id" || jg.Properties["foreign_field"] != "_id" || jg.Properties["as"] != "groups" {
		t.Errorf("inspection_groups join props = %+v", jg.Properties)
	}

	// Stage entities + order.
	got := pyStageSubtypesInOrder(ents)
	want := []string{"$lookup", "$lookup", "$lookup", "$match"}
	if len(got) != len(want) {
		t.Fatalf("stage subtypes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
	// stage_index preserved on each entity.
	for i, e := range ents {
		if e.Properties["stage_index"] != itoa(i) {
			t.Errorf("stage[%d] stage_index = %q, want %d", i, e.Properties["stage_index"], i)
		}
		if e.Properties["collection"] != "inspections" {
			t.Errorf("stage[%d] collection = %q, want inspections", i, e.Properties["collection"])
		}
		if e.Kind != string(types.EntityKindDataAccess) {
			t.Errorf("stage[%d] kind = %q, want SCOPE.DataAccess", i, e.Kind)
		}
	}
	if c := pyFindStage(ents, "$lookup").Properties["from"]; c != "inspection_groups" {
		t.Errorf("first $lookup from = %q, want inspection_groups", c)
	}
}

// THE rewrite-BLOCKING case — variable-bound pipeline whose $lookup stages use
// the CORRELATED sub-pipeline form (`from` + `let` + `pipeline` + `as`, NO
// localField/foreignField). The nested `pipeline: [...]` array and `let: {...}`
// object inside each stage must not break the multiline RHS capture nor the
// stage split. Mirrors `_get_me_inspections` from the legacy repo.
func TestMongoAggPy_VariableBound_CorrelatedLookups(t *testing.T) {
	src := `
import pymongo
from pymongo import MongoClient

def _get_me_inspections():
    inspections_cls = MongoDBConnection.get_collection(INSPECTIONS)
    pipeline = [
        {"$project": {"_id": 1, "checklist_type": 1}},
        {"$match": {"checklist_type": 4, "status": "active"}},
        {"$lookup": {"from": "inspection_groups", "let": {"gid": "$group_id"}, "pipeline": [{"$match": {"$expr": {"$eq": ["$_id", "$$gid"]}}}], "as": "inspections_group"}},
        {"$unwind": {"path": "$inspections_group", "preserveNullAndEmptyArrays": True}},
        {"$lookup": {"from": "m_devices", "let": {"did": "$device_id"}, "pipeline": [{"$match": {"$expr": {"$eq": ["$_id", "$$did"]}}}], "as": "device"}},
        {"$unwind": {"path": "$device", "preserveNullAndEmptyArrays": True}},
        {"$lookup": {"from": "m_buildings", "let": {"bid": "$building_id"}, "pipeline": [{"$match": {"$expr": {"$eq": ["$_id", "$$bid"]}}}], "as": "building"}},
        {"$unwind": {"path": "$building"}},
        {"$project": {"name": 1}},
    ]
    return list(inspections_cls.aggregate(pipeline))
`
	ents, rels := runMongoAggPy(t, src)

	// 3 join edges to the SPECIFIC correlated from-collections.
	wantTo := []string{"inspection_groups", "m_devices", "m_buildings"}
	for _, coll := range wantTo {
		j := pyFindJoinTo(rels, capitalisedSingular(coll))
		if j == nil {
			t.Fatalf("expected JOINS_COLLECTION edge to %s; rels=%+v", coll, rels)
		}
		// The `INSPECTIONS` constant receiver (imported cross-module, no in-file
		// value) must resolve to the SAME canonical collection node as the
		// string-literal `"inspections"` viewset forms: Class:Inspection, NOT a
		// phantom Class:INSPECTIONS. This is the real-vs-fixture fix.
		if j.FromID != "Class:"+capitalisedSingular("inspections") {
			t.Errorf("join to %s from = %q, want aggregating collection Class:Inspection", coll, j.FromID)
		}
		if j.Properties["stage"] != "lookup" {
			t.Errorf("join to %s stage = %q, want lookup", coll, j.Properties["stage"])
		}
	}
	if n := len(rels); n != 3 {
		t.Fatalf("expected exactly 3 join edges, got %d: %+v", n, rels)
	}

	// The correlated `as` alias is captured even without local/foreign fields.
	jg := pyFindJoinTo(rels, capitalisedSingular("inspection_groups"))
	if jg.Properties["as"] != "inspections_group" {
		t.Errorf("inspection_groups join as = %q, want inspections_group", jg.Properties["as"])
	}

	// Stage entities + order: project, match, lookup, unwind, lookup, unwind,
	// lookup, unwind, project (9 stages, order preserved).
	got := pyStageSubtypesInOrder(ents)
	want := []string{"$project", "$match", "$lookup", "$unwind", "$lookup", "$unwind", "$lookup", "$unwind", "$project"}
	if len(got) != len(want) {
		t.Fatalf("stage subtypes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

// REAL-FORM regression — verbatim copy of the production
// `core.tasks.maintenance_evaluation_notifications._get_me_inspections`
// pipeline (multiline stage objects, nested `let`/`pipeline`/`$project` on
// their OWN lines, `$first` accessor, `True` Python literals). The earlier
// #3567 fixture collapsed each `$lookup` onto ONE line, which hid a stage-split
// failure that occurs on the real multiline form. Must emit the 3 correlated
// joins (inspection_groups, m_devices, m_buildings) and the full 8-stage order.
func TestMongoAggPy_RealForm_GetMeInspections(t *testing.T) {
	src := `
import logging
from core.helper.mongo_helper import MongoDBConnection
from core.mongodb_collections import INSPECTIONS


def _get_me_inspections():
    inspections_cls = MongoDBConnection.get_collection(INSPECTIONS)
    pipeline = [
        {
            "$project": {
                "device_id": 1,
                "inspectionGroupId": 1,
                "status": 1,
                "checklist_id": {"$first": "$inspection_checklists.id"},
                "checklist_type": {"$first": "$inspection_checklists.type"},
            }
        },
        {"$match": {"checklist_type": 4, "status": {"$in": ["Results Reviewed", "Assembling Report"]}}},
        {
            "$lookup": {
                "from": "inspection_groups",
                "let": {"inspections_group_id": "$inspectionGroupId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$_id", "$$inspections_group_id"]}}},
                    {"$project": {"_id": 1, "buildingId": 1, "groupId": 1}},
                ],
                "as": "inspections_group",
            }
        },
        {"$unwind": {"path": "$inspections_group", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_devices",
                "let": {"device_id": "$device_id"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$device_id"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "name": 1}},
                ],
                "as": "device",
            }
        },
        {"$unwind": {"path": "$device", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_buildings",
                "let": {"building_id": "$inspections_group.buildingId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$building_id"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "name": 1}},
                ],
                "as": "building",
            }
        },
        {"$unwind": {"path": "$building", "preserveNullAndEmptyArrays": True}},
    ]
    return list(inspections_cls.aggregate(pipeline))
`
	ents, rels := runMongoAggPy(t, src)

	// The 3 correlated joins to the SPECIFIC from-collections.
	wantTo := []string{"inspection_groups", "m_devices", "m_buildings"}
	for _, coll := range wantTo {
		j := pyFindJoinTo(rels, capitalisedSingular(coll))
		if j == nil {
			t.Fatalf("expected JOINS_COLLECTION edge to %s; rels=%+v", coll, rels)
		}
		// Cross-module `INSPECTIONS` constant resolves to the canonical
		// Class:Inspection node — identical to the string-literal viewset forms.
		if j.FromID != "Class:"+capitalisedSingular("inspections") {
			t.Errorf("join to %s from = %q, want aggregating collection Class:Inspection", coll, j.FromID)
		}
		if j.Properties["stage"] != "lookup" {
			t.Errorf("join to %s stage = %q, want lookup", coll, j.Properties["stage"])
		}
	}
	if n := len(rels); n != 3 {
		t.Fatalf("expected exactly 3 join edges, got %d: %+v", n, rels)
	}

	jg := pyFindJoinTo(rels, capitalisedSingular("inspection_groups"))
	if jg.Properties["as"] != "inspections_group" {
		t.Errorf("inspection_groups join as = %q, want inspections_group", jg.Properties["as"])
	}

	// 8 stages, order preserved: project, match, lookup, unwind, lookup, unwind,
	// lookup, unwind.
	got := pyStageSubtypesInOrder(ents)
	want := []string{"$project", "$match", "$lookup", "$unwind", "$lookup", "$unwind", "$lookup", "$unwind"}
	if len(got) != len(want) {
		t.Fatalf("stage subtypes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

// Same-file constant definition: `INSPECTIONS = "inspections"` at module scope,
// then `get_collection(INSPECTIONS)`. The constant must resolve to its literal
// VALUE "inspections" → Class:Inspection, exactly as a quoted receiver would.
func TestMongoAggPy_CollConstResolvedToValue_SameFile(t *testing.T) {
	src := `
import pymongo
from core.helper.mongo_helper import MongoDBConnection

INSPECTIONS = "inspections"


def run():
    inspections_cls = MongoDBConnection.get_collection(INSPECTIONS)
    pipeline = [
        {"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "device"}},
    ]
    return list(inspections_cls.aggregate(pipeline))
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 1 {
		t.Fatalf("expected 1 join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("join from = %q, want Class:Inspection (constant resolved to value)", rels[0].FromID)
	}
	if len(ents) == 0 || ents[0].Properties["collection"] != "inspections" {
		t.Errorf("stage collection = %q, want inspections", ents[0].Properties["collection"])
	}
}

// REGRESSION GUARD for the 57 string-literal viewset aggregations: a quoted
// `get_collection("inspections")` receiver via the same variable-binding follow
// must keep producing Class:Inspection. The constant-resolution change must not
// touch this path.
func TestMongoAggPy_ViewsetStringLiteralReceiver_Unchanged(t *testing.T) {
	src := `
from core.helper.mongo_helper import MongoDBConnection


def get_inspection_details(self):
    inspections_group_cln = MongoDBConnection.get_collection("inspection_groups")
    pipeline = [
        {"$lookup": {"from": "inspections", "localField": "_id", "foreignField": "inspectionGroupId", "as": "inspections"}},
        {"$lookup": {"from": "m_buildings", "localField": "buildingId", "foreignField": "postgresql_id", "as": "building"}},
    ]
    return inspections_group_cln.aggregate(pipeline)
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 2 {
		t.Fatalf("expected 2 join edges, got %d: %+v", len(rels), rels)
	}
	for _, r := range rels {
		if r.FromID != "Class:"+capitalisedSingular("inspection_groups") {
			t.Errorf("join from = %q, want aggregating collection Class:Inspection_group", r.FromID)
		}
	}
	if pyFindJoinTo(rels, capitalisedSingular("inspections")) == nil {
		t.Errorf("expected join to inspections; rels=%+v", rels)
	}
	if pyFindJoinTo(rels, capitalisedSingular("m_buildings")) == nil {
		t.Errorf("expected join to m_buildings; rels=%+v", rels)
	}
	if len(ents) != 2 {
		t.Fatalf("expected 2 stage entities, got %d", len(ents))
	}
}

// BUILDER INDIRECTION (#3866) — direct call argument:
// `coll.aggregate(build_fn())` where `build_fn` is a same-file builder that
// `return [ ... ]`s a pipeline holding a $lookup. The builder body must be
// scanned and the join attributed to the AGGREGATING collection (m_devices),
// landing on the NAMED looked-up collection node Class:M_device.
func TestMongoAggPy_Builder_DirectCallArg(t *testing.T) {
	src := `
import pymongo
from pymongo import MongoClient

def build_pipe():
    return [{"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "d"}}]

def run(db):
    return db.inspections.aggregate(build_pipe())
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 1 {
		t.Fatalf("expected exactly 1 join edge from builder body, got %d: %+v", len(rels), rels)
	}
	j := rels[0]
	if j.ToID != "Class:"+capitalisedSingular("m_devices") {
		t.Errorf("join ToID = %q, want Class:M_device (named builder $lookup target)", j.ToID)
	}
	if j.FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("join FromID = %q, want Class:Inspection (aggregating collection at executor)", j.FromID)
	}
	if j.Properties["as"] != "d" || j.Properties["local_field"] != "device_id" {
		t.Errorf("join props = %+v, want as=d local_field=device_id", j.Properties)
	}
	if len(ents) != 1 || ents[0].Subtype != "$lookup" {
		t.Fatalf("expected 1 $lookup stage entity, got %+v", ents)
	}
	if ents[0].Properties["collection"] != "inspections" {
		t.Errorf("stage collection = %q, want inspections", ents[0].Properties["collection"])
	}
}

// BUILDER INDIRECTION (#3866) — call-binding then use:
// `pipeline = build_pipe(); coll.aggregate(pipeline)`. The bare-var follow finds
// no list-literal binding, falls through to the builder-call binding, resolves
// the builder body. Same named-collection result as the direct-call form.
func TestMongoAggPy_Builder_CallBindingThenUse(t *testing.T) {
	src := `
import pymongo

def build_pipe():
    return [{"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "d"}}]

def run(insp):
    pipeline = build_pipe()
    return insp.aggregate(pipeline)
`
	_, rels := runMongoAggPy(t, src)
	if len(rels) != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].ToID != "Class:"+capitalisedSingular("m_devices") {
		t.Errorf("join ToID = %q, want Class:M_device", rels[0].ToID)
	}
}

// BUILDER INDIRECTION (#3866) — builder uses the `pipeline = [ ... ]; return
// pipeline` shape (Shape B) and the executor uses get_collection(CONST) so BOTH
// new features compose: the builder body is scanned AND the aggregating
// collection resolves to the named Class:Inspection node. Mirrors the real
// `_build_inspections_pipeline` / `get_inspection_devices_pipeline` forms.
func TestMongoAggPy_Builder_ReturnVar_WithGetCollectionConst(t *testing.T) {
	src := `
import pymongo
from core.helper.mongo_helper import MongoDBConnection

INSPECTIONS = "inspections"

def _build_inspections_pipeline():
    pipeline = [
        {"$lookup": {"from": "inspection_groups", "localField": "group_id", "foreignField": "_id", "as": "g"}},
        {"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "d"}},
        {"$match": {"status": "active"}},
    ]
    return pipeline

def run():
    built = _build_inspections_pipeline()
    return MongoDBConnection.get_collection(INSPECTIONS).aggregate(built)
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 2 {
		t.Fatalf("expected 2 join edges from builder body, got %d: %+v", len(rels), rels)
	}
	for _, coll := range []string{"inspection_groups", "m_devices"} {
		j := pyFindJoinTo(rels, capitalisedSingular(coll))
		if j == nil {
			t.Fatalf("expected JOINS_COLLECTION to %s; rels=%+v", coll, rels)
		}
		if j.FromID != "Class:"+capitalisedSingular("inspections") {
			t.Errorf("join to %s FromID = %q, want Class:Inspection (const-resolved, NOT ext:get_collection)", coll, j.FromID)
		}
	}
	got := pyStageSubtypesInOrder(ents)
	want := []string{"$lookup", "$lookup", "$match"}
	if len(got) != len(want) {
		t.Fatalf("stage subtypes = %v, want %v", got, want)
	}
}

// DIRECT get_collection(CONST) at the call site (#3866) — no intermediate
// variable: `get_collection(INSPECTIONS).aggregate([...])`. CONST resolves to
// the named collection node so the JOINS_COLLECTION FromID is Class:Inspection,
// NOT a phantom Class:INSPECTIONS / shared ext:get_collection node.
func TestMongoAggPy_DirectGetCollectionConst_Inline(t *testing.T) {
	src := `
import pymongo
from core.helper.mongo_helper import MongoDBConnection

INSPECTIONS = "inspections"

def run():
    return MongoDBConnection.get_collection(INSPECTIONS).aggregate([
        {"$lookup": {"from": "inspection_groups", "localField": "_id", "foreignField": "gid", "as": "g"}},
    ])
`
	_, rels := runMongoAggPy(t, src)
	if len(rels) != 1 {
		t.Fatalf("expected 1 join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("FromID = %q, want Class:Inspection (direct const-resolved receiver)", rels[0].FromID)
	}
	if rels[0].ToID != "Class:"+capitalisedSingular("inspection_groups") {
		t.Errorf("ToID = %q, want Class:Inspection_group", rels[0].ToID)
	}
}

// DIRECT get_collection(CONST) with a cross-module (no in-file value) UPPER_SNAKE
// constant — resolves via the lowercase-name convention to Class:Inspection.
func TestMongoAggPy_DirectGetCollectionConst_CrossModule(t *testing.T) {
	src := `
from core.helper.mongo_helper import MongoDBConnection
from core.mongodb_collections import INSPECTIONS

def run():
    return MongoDBConnection.get_collection(INSPECTIONS).aggregate([
        {"$lookup": {"from": "m_devices", "localField": "_id", "foreignField": "did", "as": "d"}},
    ])
`
	_, rels := runMongoAggPy(t, src)
	if len(rels) != 1 {
		t.Fatalf("expected 1 join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("FromID = %q, want Class:Inspection (cross-module const via name convention)", rels[0].FromID)
	}
}

// NEGATIVE (#3866): a DYNAMIC lowercase variable passed to get_collection
// (`get_collection(coll_var)`) is NOT a constant and must stay unresolved — we
// keep the bare receiver behavior, no Class:Inspection fabrication. The bare
// lowercase token `coll_var` is the receiver fallback; it must not be lowercased
// and treated as a collection-name constant.
func TestMongoAggPy_DirectGetCollection_DynamicVar_Unresolved(t *testing.T) {
	src := `
import pymongo
from core.helper.mongo_helper import MongoDBConnection

def run(coll_var):
    return MongoDBConnection.get_collection(coll_var).aggregate([
        {"$lookup": {"from": "m_devices", "localField": "_id", "foreignField": "did", "as": "d"}},
    ])
`
	_, rels := runMongoAggPy(t, src)
	// Honest-partial: the join may still be emitted (from the inline literal) but
	// its FromID must NOT be falsely resolved to Class:Inspection — it falls back
	// to the bare token coll_var.
	for _, r := range rels {
		if r.FromID == "Class:"+capitalisedSingular("inspections") {
			t.Fatalf("dynamic get_collection(coll_var) must NOT resolve to Class:Inspection: %+v", r)
		}
	}
	// The bare-var fallback anchors on the variable token, not a phantom const.
	if len(rels) == 1 && rels[0].FromID != "Class:"+capitalisedSingular("coll_var") {
		t.Errorf("FromID = %q, want bare-var fallback Class:Coll_var", rels[0].FromID)
	}
}

// NEGATIVE (#3866): builder defined in ANOTHER module (not in this file) → no
// body to scan → honest unresolved, no fabricated join/stage.
func TestMongoAggPy_Builder_CrossModule_Unresolved(t *testing.T) {
	src := `
import pymongo
from other.module import build_remote_pipeline

def run(db):
    return db.inspections.aggregate(build_remote_pipeline())
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("cross-module builder must emit nothing, got rels=%d ents=%d", len(rels), len(ents))
	}
}

// NEGATIVE (#3866): builder whose return is itself a CALL / dynamic dispatch
// (`return assemble(...)`, no same-function list literal) → unresolved.
func TestMongoAggPy_Builder_NonLiteralReturn_Unresolved(t *testing.T) {
	src := `
import pymongo

def build_pipe():
    return assemble_stages(extra=True)

def run(db):
    return db.inspections.aggregate(build_pipe())
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("non-literal builder return must emit nothing, got rels=%d ents=%d", len(rels), len(ents))
	}
}

// IMPERATIVE BUILDER (#3928, deploy-7 P0-B) — THE rewrite-blocking real shape.
// A builder `def get_pipe()` starts `pipeline = []`, then `.append({$lookup})`
// for one stage and `.extend([{$lookup}])` for more, then `return pipeline`; the
// executor does `coll.aggregate(get_pipe())`. Both `from` collections
// (inspection_groups via append, m_devices via extend) must resolve to their
// NAMED collection nodes Class:Inspection_group AND Class:M_device, attributed
// to the aggregating collection Class:Inspection. Mirrors
// get_inspection_devices_pipeline in core/services/building/queries.py.
func TestMongoAggPy_Imperative_AppendExtend_BuilderReturn(t *testing.T) {
	src := `
import pymongo
from pymongo import MongoClient

def get_pipe():
    pipeline = []
    pipeline.append({"$lookup": {"from": "inspection_groups", "localField": "group_id", "foreignField": "_id", "as": "g"}})
    pipeline.extend([{"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "d"}}])
    return pipeline

def run(db):
    return db.inspections.aggregate(get_pipe())
`
	ents, rels := runMongoAggPy(t, src)

	if len(rels) != 2 {
		t.Fatalf("expected exactly 2 join edges from append+extend, got %d: %+v", len(rels), rels)
	}
	// Named collection nodes (not raw strings dropped on the floor).
	jg := pyFindJoinTo(rels, capitalisedSingular("inspection_groups"))
	if jg == nil {
		t.Fatalf("expected JOINS_COLLECTION to Class:Inspection_group (append stage); rels=%+v", rels)
	}
	if jg.ToID != "Class:"+capitalisedSingular("inspection_groups") {
		t.Errorf("append join ToID = %q, want Class:Inspection_group", jg.ToID)
	}
	if jg.FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("append join FromID = %q, want Class:Inspection (aggregating coll)", jg.FromID)
	}
	if jg.Properties["as"] != "g" || jg.Properties["local_field"] != "group_id" {
		t.Errorf("append join props = %+v, want as=g local_field=group_id", jg.Properties)
	}
	jd := pyFindJoinTo(rels, capitalisedSingular("m_devices"))
	if jd == nil {
		t.Fatalf("expected JOINS_COLLECTION to Class:M_device (extend stage); rels=%+v", rels)
	}
	if jd.ToID != "Class:"+capitalisedSingular("m_devices") {
		t.Errorf("extend join ToID = %q, want Class:M_device", jd.ToID)
	}
	if jd.FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("extend join FromID = %q, want Class:Inspection", jd.FromID)
	}
	// Two stage nodes, append before extend (source order preserved).
	got := pyStageSubtypesInOrder(ents)
	want := []string{"$lookup", "$lookup"}
	if len(got) != len(want) {
		t.Fatalf("stage subtypes = %v, want %v", got, want)
	}
	if ents[0].Properties["from"] != "inspection_groups" {
		t.Errorf("stage[0] from = %q, want inspection_groups (append first)", ents[0].Properties["from"])
	}
	if ents[1].Properties["from"] != "m_devices" {
		t.Errorf("stage[1] from = %q, want m_devices (extend second)", ents[1].Properties["from"])
	}
	for i, e := range ents {
		if e.Properties["collection"] != "inspections" {
			t.Errorf("stage[%d] collection = %q, want inspections", i, e.Properties["collection"])
		}
		if e.Kind != string(types.EntityKindDataAccess) {
			t.Errorf("stage[%d] kind = %q, want SCOPE.DataAccess", i, e.Kind)
		}
	}
}

// IMPERATIVE DIRECT EXECUTOR (#3928) — single inline append then aggregate,
// no builder fn: `pipeline = []; pipeline.append({$lookup}); coll.aggregate(
// pipeline)`. The bare-var follow finds no complete list literal, the imperative
// reconstruction collects the appended stage → one join to the NAMED collection.
func TestMongoAggPy_Imperative_DirectExecutor_SingleAppend(t *testing.T) {
	src := `
import pymongo

def run(db):
    pipeline = []
    pipeline.append({"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "d"}})
    return db.inspections.aggregate(pipeline)
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 1 {
		t.Fatalf("expected exactly 1 join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].ToID != "Class:"+capitalisedSingular("m_devices") {
		t.Errorf("join ToID = %q, want Class:M_device", rels[0].ToID)
	}
	if rels[0].FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("join FromID = %q, want Class:Inspection", rels[0].FromID)
	}
	if len(ents) != 1 || ents[0].Subtype != "$lookup" {
		t.Fatalf("expected 1 $lookup stage, got %+v", ents)
	}
}

// IMPERATIVE with NON-EMPTY list-literal init + appends interleaved (#3928):
// `pipeline = [{$match}]; pipeline.append({$lookup A}); pipeline.append(
// {$lookup B})`. Init stage + both appends accumulate in source order; both
// $lookup `from` collections resolve.
func TestMongoAggPy_Imperative_LiteralInitPlusAppends(t *testing.T) {
	src := `
import pymongo

def get_pipe():
    pipeline = [{"$match": {"status": "active"}}]
    pipeline.append({"$lookup": {"from": "inspection_groups", "localField": "g", "foreignField": "_id", "as": "grp"}})
    pipeline.append({"$lookup": {"from": "m_buildings", "localField": "b", "foreignField": "_id", "as": "bld"}})
    return pipeline

def run(db):
    return db.inspections.aggregate(get_pipe())
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 2 {
		t.Fatalf("expected 2 join edges, got %d: %+v", len(rels), rels)
	}
	if pyFindJoinTo(rels, capitalisedSingular("inspection_groups")) == nil {
		t.Errorf("expected join to Class:Inspection_group; rels=%+v", rels)
	}
	if pyFindJoinTo(rels, capitalisedSingular("m_buildings")) == nil {
		t.Errorf("expected join to Class:M_building; rels=%+v", rels)
	}
	got := pyStageSubtypesInOrder(ents)
	want := []string{"$match", "$lookup", "$lookup"}
	if len(got) != len(want) {
		t.Fatalf("stage subtypes = %v, want %v (init match then 2 appends)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// IMPERATIVE $graphLookup via extend (#3928): a $graphLookup added through
// `.extend([...])` produces a graphLookup join to the named target.
func TestMongoAggPy_Imperative_GraphLookupViaExtend(t *testing.T) {
	src := `
import pymongo

def get_pipe():
    pipeline = []
    pipeline.extend([{"$graphLookup": {"from": "categories", "startWith": "$cat", "connectFromField": "parent", "connectToField": "_id", "as": "tree"}}])
    return pipeline

def run(db):
    return db["orders"].aggregate(get_pipe())
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 1 {
		t.Fatalf("expected 1 join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].ToID != "Class:"+capitalisedSingular("categories") {
		t.Errorf("join ToID = %q, want Class:Category", rels[0].ToID)
	}
	if rels[0].Properties["stage"] != "graphLookup" {
		t.Errorf("join stage = %q, want graphLookup", rels[0].Properties["stage"])
	}
	if len(ents) != 1 || ents[0].Subtype != "$graphLookup" {
		t.Fatalf("expected 1 $graphLookup stage, got %+v", ents)
	}
}

// NEGATIVE (#3928): `pipeline.append(stage_var)` where the argument is a
// NON-literal variable must NOT fabricate a join/stage from that append. Only the
// literal-dict appends contribute; a var append is an honest skip.
func TestMongoAggPy_Imperative_NonLiteralAppend_Skipped(t *testing.T) {
	src := `
import pymongo

def get_pipe(stage_var):
    pipeline = []
    pipeline.append(stage_var)
    return pipeline

def run(db, sv):
    return db.inspections.aggregate(get_pipe(sv))
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("non-literal append must emit nothing, got rels=%d ents=%d", len(rels), len(ents))
	}
}

// NEGATIVE (#3928): a $lookup whose `from` is a DYNAMIC value (a variable, not a
// string literal) in an appended dict yields NO join edge — the stage node may
// exist but mongoAggParseLookup finds no static `from`. Honest-partial.
func TestMongoAggPy_Imperative_DynamicFrom_NoJoin(t *testing.T) {
	src := `
import pymongo

def get_pipe(coll_name):
    pipeline = []
    pipeline.append({"$lookup": {"from": coll_name, "localField": "x", "foreignField": "_id", "as": "y"}})
    return pipeline

def run(db, cn):
    return db.inspections.aggregate(get_pipe(cn))
`
	_, rels := runMongoAggPy(t, src)
	if len(rels) != 0 {
		t.Fatalf("dynamic $lookup from must emit NO join edge, got %d: %+v", len(rels), rels)
	}
}

// NEGATIVE (#3928): `pipeline.extend(other_pipe)` where the arg is a non-literal
// variable contributes nothing. With no literal init and no literal additions,
// the whole pipeline stays unresolved — no fabrication.
func TestMongoAggPy_Imperative_NonLiteralExtend_Unresolved(t *testing.T) {
	src := `
import pymongo

def get_pipe(other_pipe):
    pipeline = []
    pipeline.extend(other_pipe)
    return pipeline

def run(db, op):
    return db.inspections.aggregate(get_pipe(op))
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("non-literal extend must emit nothing, got rels=%d ents=%d", len(rels), len(ents))
	}
}

// Inline list-literal pipeline with $lookup + $graphLookup + $group + $facet,
// receiver via db["collname"] subscript. Asserts both join kinds, the $group
// _id/accumulators, and the $facet keys.
func TestMongoAggPy_InlineLiteral_LookupGraphGroupFacet(t *testing.T) {
	src := `
from motor.motor_asyncio import AsyncIOMotorClient

async def stats(db):
    return await db["orders"].aggregate([
        {"$lookup": {"from": "customers", "localField": "cust_id", "foreignField": "_id", "as": "cust"}},
        {"$graphLookup": {"from": "categories", "startWith": "$cat", "connectFromField": "parent", "connectToField": "_id", "as": "tree"}},
        {"$group": {"_id": "$status", "total": {"$sum": "$amount"}, "count": {"$sum": 1}}},
        {"$facet": {"byStatus": [{"$match": {}}], "byMonth": [{"$match": {}}]}},
    ])
`
	ents, rels := runMongoAggPy(t, src)

	if j := pyFindJoinTo(rels, capitalisedSingular("customers")); j == nil {
		t.Fatalf("expected $lookup join to customers; rels=%+v", rels)
	} else if j.Properties["stage"] != "lookup" {
		t.Errorf("customers join stage = %q, want lookup", j.Properties["stage"])
	}
	if j := pyFindJoinTo(rels, capitalisedSingular("categories")); j == nil {
		t.Fatalf("expected $graphLookup join to categories; rels=%+v", rels)
	} else if j.Properties["stage"] != "graphLookup" {
		t.Errorf("categories join stage = %q, want graphLookup", j.Properties["stage"])
	}
	if len(rels) != 2 {
		t.Fatalf("expected 2 join edges, got %d: %+v", len(rels), rels)
	}

	if got := pyStageSubtypesInOrder(ents); len(got) != 4 ||
		got[0] != "$lookup" || got[1] != "$graphLookup" || got[2] != "$group" || got[3] != "$facet" {
		t.Fatalf("stage subtypes = %v, want [$lookup $graphLookup $group $facet]", got)
	}

	g := pyFindStage(ents, "$group")
	// _id value is captured as raw text; Python string literals keep quotes.
	if g.Properties["group_id"] != `"$status"` {
		t.Errorf("$group group_id = %q, want \"$status\"", g.Properties["group_id"])
	}
	if g.Properties["accumulators"] != "total,count" {
		t.Errorf("$group accumulators = %q, want total,count", g.Properties["accumulators"])
	}

	f := pyFindStage(ents, "$facet")
	if f.Properties["facets"] != "byStatus,byMonth" {
		t.Errorf("$facet facets = %q, want byStatus,byMonth", f.Properties["facets"])
	}

	// Receiver resolved from db["orders"] subscript.
	if ents[0].Properties["collection"] != "orders" {
		t.Errorf("collection = %q, want orders", ents[0].Properties["collection"])
	}
}

// get_collection("coll") receiver idiom.
func TestMongoAggPy_GetCollectionReceiver(t *testing.T) {
	src := `
import pymongo

def run(db):
    coll = db.get_collection("events")
    return db.get_collection("events").aggregate([
        {"$lookup": {"from": "users", "localField": "uid", "foreignField": "_id", "as": "u"}},
    ])
`
	ents, rels := runMongoAggPy(t, src)
	if j := pyFindJoinTo(rels, capitalisedSingular("users")); j == nil {
		t.Fatalf("expected join to users; rels=%+v", rels)
	} else if j.FromID != "Class:"+capitalisedSingular("events") {
		t.Errorf("join from = %q, want aggregating collection events", j.FromID)
	}
	if len(ents) != 1 || ents[0].Properties["collection"] != "events" {
		t.Errorf("collection = %q, want events", ents[0].Properties["collection"])
	}
}

// NEGATIVE: variable bound to a NON-literal (a builder call) must NOT fabricate
// any join or stage. Honest unresolved.
func TestMongoAggPy_NegativeBuilderBinding_NoFabrication(t *testing.T) {
	src := `
import pymongo

def run(db):
    pipeline = build_pipeline(db, status="active")
    return db.inspections.aggregate(pipeline)
`
	ents, rels := runMongoAggPy(t, src)
	if len(rels) != 0 {
		t.Fatalf("builder-bound pipeline must emit NO join edges, got %d: %+v", len(rels), rels)
	}
	if len(ents) != 0 {
		t.Fatalf("builder-bound pipeline must emit NO stage entities, got %d: %+v", len(ents), ents)
	}
}

// NEGATIVE: identifier argument that is an EXPRESSION (`pipeline + extra`) is
// not a single-binding follow → unresolved, no fabrication.
func TestMongoAggPy_NegativeExpressionArg_NoFabrication(t *testing.T) {
	src := `
import pymongo

def run(db):
    pipeline = [{"$lookup": {"from": "users", "localField": "u", "foreignField": "_id", "as": "x"}}]
    extra = []
    return db.inspections.aggregate(pipeline + extra)
`
	_, rels := runMongoAggPy(t, src)
	if len(rels) != 0 {
		t.Fatalf("expression-arg pipeline must emit NO join edges, got %d: %+v", len(rels), rels)
	}
}

// NEGATIVE: no pymongo/motor surface in the file → gate skips entirely even if
// some `.aggregate([...])` is present (e.g. pandas).
func TestMongoAggPy_NegativeGate_NoMongoSurface(t *testing.T) {
	src := `
import pandas as pd

def run(frame):
    return frame.aggregate([
        {"$lookup": {"from": "users", "localField": "u", "foreignField": "_id", "as": "x"}},
    ])
`
	ents, rels := runMongoAggPy(t, src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("non-mongo file must be skipped by gate, got ents=%d rels=%d", len(ents), len(rels))
	}
}

// Single-binding follow picks the NEAREST preceding assignment in the same
// function; an earlier same-name binding in a different function is out of scope.
func TestMongoAggPy_BindingScopedToFunction(t *testing.T) {
	src := `
import pymongo

def other(db):
    pipeline = [{"$lookup": {"from": "WRONG", "localField": "a", "foreignField": "_id", "as": "x"}}]
    return db.x.aggregate(pipeline)

def run(db):
    pipeline = [{"$lookup": {"from": "right_coll", "localField": "b", "foreignField": "_id", "as": "y"}}]
    return db.inspections.aggregate(pipeline)
`
	_, rels := runMongoAggPy(t, src)
	if pyFindJoinTo(rels, capitalisedSingular("right_coll")) == nil {
		t.Fatalf("expected join to right_coll (in-scope binding); rels=%+v", rels)
	}
	// The WRONG binding belongs to other(); run()'s aggregate must not use it.
	for _, r := range rels {
		if r.ToID == "Class:"+capitalisedSingular("WRONG") && r.FromID == "Class:"+capitalisedSingular("inspections") {
			t.Fatalf("run() aggregate wrongly resolved to other()'s binding: %+v", r)
		}
	}
}

// =====================================================================
// deploy-8 #3969: cross-file builder resolution + nested $lookup recursion.
// =====================================================================

// CROSS-FILE BUILDER (#3969 ask 1) — THE rewrite-blocking real shape:
// the `.aggregate()` executor lives in `service.py` but the pipeline builder is
// IMPORTED from a SIBLING file `queries.py`. Same-file scan finds no def; the
// cross-file resolver supplies queries.py's source and the builder body is
// scanned there. Assert the NAMED JOINS_COLLECTION edge resolves cross-file:
// Class:Inspection (aggregating coll) → Class:Inspection ($lookup from
// "inspections"), proving the builder body in the OTHER file was read.
func TestMongoAggPy_CrossFileBuilder_Resolved(t *testing.T) {
	// Executor file: imports the builder, then aggregates with its result.
	service := `
import pymongo
from .queries import get_inspection_devices_pipeline

def run(db):
    return db.devices.aggregate(get_inspection_devices_pipeline(db))
`
	// Defining file (queries.py): the builder returns a $lookup-to-inspections.
	queries := `
def get_inspection_devices_pipeline(db):
    return [{"$lookup": {"from": "inspections", "localField": "inspection_id", "foreignField": "_id", "as": "insp"}}]
`
	modules := map[string]string{
		"get_inspection_devices_pipeline": queries,
	}
	ents, rels := runMongoAggPyXFile(t, service, modules)

	if len(rels) != 1 {
		t.Fatalf("expected exactly 1 cross-file join edge, got %d: %+v", len(rels), rels)
	}
	j := rels[0]
	if j.ToID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("cross-file join ToID = %q, want Class:Inspection (builder $lookup from in queries.py)", j.ToID)
	}
	if j.FromID != "Class:"+capitalisedSingular("devices") {
		t.Errorf("cross-file join FromID = %q, want Class:Device (aggregating collection at executor)", j.FromID)
	}
	if j.Properties["local_field"] != "inspection_id" || j.Properties["as"] != "insp" {
		t.Errorf("cross-file join props = %+v, want local_field=inspection_id as=insp", j.Properties)
	}
	if len(ents) != 1 || ents[0].Subtype != "$lookup" {
		t.Fatalf("expected 1 $lookup stage entity from cross-file builder, got %+v", ents)
	}
}

// CROSS-FILE BUILDER, DIRECT-CALL-ARG variant: `coll.aggregate(build())` with
// `build` imported (absolute dotted import this time). Same resolution path.
func TestMongoAggPy_CrossFileBuilder_DirectCallArg(t *testing.T) {
	service := `
import pymongo
from core.services.building.queries import build_pipe

def run(insp):
    return insp.aggregate(build_pipe())
`
	queries := `
def build_pipe():
    return [{"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "d"}}]
`
	_, rels := runMongoAggPyXFile(t, service, map[string]string{"build_pipe": queries})
	if len(rels) != 1 {
		t.Fatalf("expected exactly 1 cross-file join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].ToID != "Class:"+capitalisedSingular("m_devices") {
		t.Errorf("cross-file join ToID = %q, want Class:M_device", rels[0].ToID)
	}
}

// NEGATIVE (#3969): builder imported from a module the resolver cannot locate
// (unresolvable import → resolver returns "") MUST emit NO edge — honest skip,
// no fabrication. Driven with an empty module map so the resolver yields "".
func TestMongoAggPy_CrossFileBuilder_UnresolvableImport_NoEdge(t *testing.T) {
	service := `
import pymongo
from .missing import build_pipe

def run(db):
    return db.inspections.aggregate(build_pipe())
`
	ents, rels := runMongoAggPyXFile(t, service, map[string]string{}) // empty: nothing resolves
	if len(rels) != 0 || len(ents) != 0 {
		t.Fatalf("unresolvable cross-file builder must emit nothing, got rels=%d ents=%d", len(rels), len(ents))
	}
}

// NESTED $lookup (#3969 ask 2) — THE correlated-join shape the top-level
// splitter misses: a `$lookup` against m_contracts carries a `pipeline:[...]`
// whose OWN `$lookup` targets m_group_device_settings. BOTH joins must be
// emitted: Class:M_contract (outer) AND Class:M_group_device_setting (nested).
func TestMongoAggPy_NestedLookup_CorrelatedSubPipeline(t *testing.T) {
	src := `
import pymongo

def run(db):
    pipeline = [
        {"$lookup": {"from": "m_contracts", "let": {"cid": "$contract_id"}, "pipeline": [
            {"$match": {"$expr": {"$eq": ["$_id", "$$cid"]}}},
            {"$lookup": {"from": "m_group_device_settings", "localField": "gid", "foreignField": "_id", "as": "gds"}}
        ], "as": "contract"}},
    ]
    return db.inspections.aggregate(pipeline)
`
	_, rels := runMongoAggPy(t, src)

	outer := pyFindJoinTo(rels, capitalisedSingular("m_contracts"))
	if outer == nil {
		t.Fatalf("expected OUTER join to Class:M_contract; rels=%+v", rels)
	}
	if outer.FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("outer join FromID = %q, want Class:Inspection", outer.FromID)
	}
	nested := pyFindJoinTo(rels, capitalisedSingular("m_group_device_settings"))
	if nested == nil {
		t.Fatalf("expected NESTED join to Class:M_group_device_setting; rels=%+v", rels)
	}
	if nested.FromID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("nested join FromID = %q, want Class:Inspection (correlated against aggregating coll)", nested.FromID)
	}
	if nested.Properties["local_field"] != "gid" || nested.Properties["as"] != "gds" {
		t.Errorf("nested join props = %+v, want local_field=gid as=gds", nested.Properties)
	}
	// Exactly the two joins — no phantom edges from the inner $match.
	if len(rels) != 2 {
		t.Fatalf("expected exactly 2 join edges (outer + nested), got %d: %+v", len(rels), rels)
	}
}

// NESTED + CROSS-FILE compose (#3969): the builder in the sibling file returns a
// correlated $lookup with a nested $lookup; both the cross-file follow AND the
// nested recursion must fire — Class:M_contract AND Class:M_group_device_setting.
func TestMongoAggPy_NestedLookup_InsideCrossFileBuilder(t *testing.T) {
	service := `
import pymongo
from .queries import get_inspection_devices_pipeline

def run(db):
    return db.inspections.aggregate(get_inspection_devices_pipeline(db))
`
	queries := `
def get_inspection_devices_pipeline(db):
    return [
        {"$lookup": {"from": "m_contracts", "pipeline": [
            {"$lookup": {"from": "m_group_device_settings", "localField": "gid", "foreignField": "_id", "as": "gds"}}
        ], "as": "contract"}},
    ]
`
	_, rels := runMongoAggPyXFile(t, service, map[string]string{"get_inspection_devices_pipeline": queries})
	if pyFindJoinTo(rels, capitalisedSingular("m_contracts")) == nil {
		t.Fatalf("expected OUTER join to Class:M_contract (cross-file); rels=%+v", rels)
	}
	if pyFindJoinTo(rels, capitalisedSingular("m_group_device_settings")) == nil {
		t.Fatalf("expected NESTED join to Class:M_group_device_setting (cross-file + nested); rels=%+v", rels)
	}
	if len(rels) != 2 {
		t.Fatalf("expected exactly 2 join edges, got %d: %+v", len(rels), rels)
	}
}

// NEGATIVE (#3969): a nested $lookup whose `from` is DYNAMIC (a variable/
// expression, not a string literal) must NOT emit an edge — we never fabricate a
// join target we cannot statically read.
func TestMongoAggPy_NestedLookup_DynamicFrom_NoEdge(t *testing.T) {
	src := `
import pymongo

def run(db, target):
    pipeline = [
        {"$lookup": {"from": "m_contracts", "pipeline": [
            {"$lookup": {"from": target, "localField": "gid", "foreignField": "_id", "as": "gds"}}
        ], "as": "contract"}},
    ]
    return db.inspections.aggregate(pipeline)
`
	_, rels := runMongoAggPy(t, src)
	// Only the outer (literal) join resolves; the dynamic nested from is skipped.
	if pyFindJoinTo(rels, capitalisedSingular("m_contracts")) == nil {
		t.Fatalf("expected OUTER literal join to Class:M_contract; rels=%+v", rels)
	}
	if len(rels) != 1 {
		t.Fatalf("dynamic nested from must not emit an edge; want 1 join, got %d: %+v", len(rels), rels)
	}
}

// ON-DISK cross-file resolution (#3969) — exercises the PRODUCTION resolver
// factory `newMongoAggPyCrossFileResolver` end-to-end against a real temp repo:
// `service.py` imports `get_pipe` from the sibling `queries.py`, the resolver
// reads queries.py off disk and the builder body is scanned. Proves the import →
// module-path → file resolution works for a relative import.
func TestMongoAggPy_CrossFileBuilder_OnDiskRelativeImport(t *testing.T) {
	repo := t.TempDir()
	pkg := filepath.Join(repo, "core", "services", "building")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "queries.py"), []byte(`
def get_pipe(db):
    return [{"$lookup": {"from": "inspections", "localField": "inspection_id", "foreignField": "_id", "as": "insp"}}]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join("core", "services", "building", "service.py")
	serviceSrc := `
import pymongo
from .queries import get_pipe

def run(db):
    return db.devices.aggregate(get_pipe(db))
`
	resolver := newMongoAggPyCrossFileResolver(repo, servicePath, serviceSrc)
	funcs := indexEnclosingFunctions("python", serviceSrc)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanPythonMongoAggregation(serviceSrc, funcs, servicePath, "python", resolver,
		func(e types.EntityRecord) { ents = append(ents, e) },
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)
	if len(rels) != 1 {
		t.Fatalf("expected 1 on-disk cross-file join edge, got %d: %+v", len(rels), rels)
	}
	if rels[0].ToID != "Class:"+capitalisedSingular("inspections") {
		t.Errorf("on-disk cross-file join ToID = %q, want Class:Inspection", rels[0].ToID)
	}
	if rels[0].FromID != "Class:"+capitalisedSingular("devices") {
		t.Errorf("on-disk cross-file join FromID = %q, want Class:Device", rels[0].FromID)
	}
}
