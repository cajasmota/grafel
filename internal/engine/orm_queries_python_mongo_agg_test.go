package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runMongoAggPy drives scanPythonMongoAggregation over `src` and collects the
// emitted stage entities + join edges.
func runMongoAggPy(t *testing.T, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	funcs := indexEnclosingFunctions("python", src)
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	scanPythonMongoAggregation(src, funcs, "svc/agg.py", "python",
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
