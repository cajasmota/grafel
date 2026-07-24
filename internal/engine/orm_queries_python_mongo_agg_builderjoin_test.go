package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Deploy-8 item-2 parity fix: Mongo `$lookup` → JOINS_COLLECTION for a builder
// pipeline assembled with a list-literal init + `pipeline.append({...})` +
// `pipeline.extend([...])`, returned cross-file and consumed by `.aggregate()`.
//
// REAL FAILING TARGET (reproduced here):
//   acme-core core/services/building/queries.py:get_inspection_devices_pipeline
//   (builder) imported via a MULTI-LINE parenthesised `from ... import (...)` by
//   service.py and run as `inspections_cln.aggregate(get_inspection_devices_
//   pipeline(params))`.
//
// ROOT CAUSE the production resolver `newMongoAggPyCrossFileResolver` only parsed
// SINGLE-LINE imports, so the multi-line list never mapped the builder name →
// module; the builder stayed unresolved and every `$lookup.from` orphaned
// (depth-2 subgraph grep for JOINS_COLLECTION = 0). See TestMongoAggPy_Builder
// JoinFix_OnDisk_MultilineImport which reproduces the empty-rels orphan against
// the production factory.

// mongoAggBuilderJoinQueries is the queries.py builder body shared by the
// fixtures below: list-literal init carrying a correlated nested-pipeline
// $lookup, an append of a NON-lookup $match, and an extend carrying a $lookup.
const mongoAggBuilderJoinQueries = `
from pymongo import ASCENDING, DESCENDING

def get_inspection_devices_pipeline(params):
    pipeline = [
        {"$lookup": {"from": "inspections", "localField": "inspection_id", "foreignField": "_id", "as": "insp"}},
        {"$lookup": {"from": "m_contracts", "let": {"cid": "$contractId"}, "pipeline": [
            {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$cid"]}}},
            {"$lookup": {"from": "m_devices", "localField": "device_id", "foreignField": "_id", "as": "dev"}},
        ], "as": "contract"}},
    ]
    if params.get("statuses"):
        pipeline.append({"$match": {"status": {"$in": params["statuses"]}}})
    pipeline.extend([
        {"$lookup": {"from": "inspections_history", "localField": "_id", "foreignField": "parent_id", "as": "history"}},
    ])
    return pipeline
`

// assertBuilderJoinTargets asserts the four NAMED JOINS_COLLECTION edges the real
// target produces — to the SPECIFIC `from:` collections, not len>0:
//
//	inspections          — init list-literal $lookup
//	m_contracts          — init correlated $lookup (outer)
//	m_devices            — nested sub-pipeline $lookup (inner, correlated)
//	inspections_history  — extend([...]) $lookup
func assertBuilderJoinTargets(t *testing.T, rels []types.RelationshipRecord) {
	t.Helper()
	want := []string{"inspections", "m_contracts", "m_devices", "inspections_history"}
	for _, raw := range want {
		coll := capitalisedSingular(raw)
		if pyFindJoinTo(rels, coll) == nil {
			t.Fatalf("MISSING JOINS_COLLECTION to Class:%s (raw %q); rels=%+v", coll, raw, rels)
		}
	}
	if len(rels) != len(want) {
		t.Fatalf("expected exactly %d join edges, got %d: %+v", len(want), len(rels), rels)
	}
	// FromID must anchor on the aggregating collection (inspections), proving the
	// cross-file receiver resolved — not the builder var or an orphan source.
	for _, raw := range want {
		j := pyFindJoinTo(rels, capitalisedSingular(raw))
		if j.FromID != "Class:"+capitalisedSingular("inspections") {
			t.Errorf("join to %s FromID = %q, want Class:%s (aggregating coll)",
				raw, j.FromID, capitalisedSingular("inspections"))
		}
	}
}

// scanWithOnDiskQueries writes the shared queries.py + a service.py with the
// given import line, runs the PRODUCTION cross-file resolver, and returns the
// emitted join edges.
func scanWithOnDiskQueries(t *testing.T, importStmt string) []types.RelationshipRecord {
	t.Helper()
	repo := t.TempDir()
	pkg := filepath.Join(repo, "core", "services", "building")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "queries.py"),
		[]byte(mongoAggBuilderJoinQueries), 0o644); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join("core", "services", "building", "service.py")
	serviceSrc := `
import pymongo
from core.helper.mongo_helper import MongoDBConnection
from core.mongodb_collections import INSPECTIONS
` + importStmt + `

def get_inspection_devices(params):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    return inspections_cln.aggregate(get_inspection_devices_pipeline(params)).next()
`
	resolver := newMongoAggPyCrossFileResolver(repo, servicePath, serviceSrc)
	funcs := indexEnclosingFunctions("python", serviceSrc)
	var rels []types.RelationshipRecord
	scanPythonMongoAggregation(serviceSrc, funcs, servicePath, "python", resolver,
		func(e types.EntityRecord) {},
		func(r types.RelationshipRecord) {
			// #4244 — drop the node-anchored JOINS_COLLECTION twin so the
			// count/identity assertions below see the collection-anchored
			// edge set they were written against.
			if r.Properties.Get("anchor") == "stage_node" {
				return
			}
			rels = append(rels, r)
		},
	)
	return rels
}

// (a)+(b)+(c): in-memory cross-file resolver — append-assembled, extend-assembled,
// and nested/correlated pipeline-form $lookup all resolve from the builder body.
func TestMongoAggPy_BuilderJoinFix_AppendExtendNested(t *testing.T) {
	service := `
import pymongo
from core.helper.mongo_helper import MongoDBConnection
from core.mongodb_collections import INSPECTIONS
from core.services.building.queries import (
    get_inspection_devices_pipeline,
    get_inspection_devices_filters_pipeline,
)

def get_inspection_devices(params):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    return inspections_cln.aggregate(get_inspection_devices_pipeline(params)).next()
`
	_, rels := runMongoAggPyXFile(t, service,
		map[string]string{"get_inspection_devices_pipeline": mongoAggBuilderJoinQueries})
	assertBuilderJoinTargets(t, rels)
}

// THE ORPHAN REPRODUCER + FIX: production on-disk resolver with the EXACT
// multi-line parenthesised import shape from service.py. Before the fix this
// yielded ZERO join edges (the builder name never mapped to its module).
func TestMongoAggPy_BuilderJoinFix_OnDisk_MultilineImport(t *testing.T) {
	rels := scanWithOnDiskQueries(t, `from core.services.building.queries import (
    get_inspection_devices_pipeline,
    get_inspection_devices_filters_pipeline,
)`)
	assertBuilderJoinTargets(t, rels)
}

// SINGLE-LINE parenthesised import on ONE line (`from x import (a, b)`) must also
// resolve — the new paren regex owns this shape (the single-line regex now
// excludes a leading `(`). Guards the disjointness of the two import matchers.
func TestMongoAggPy_BuilderJoinFix_SingleLineParenImport(t *testing.T) {
	rels := scanWithOnDiskQueries(t,
		`from core.services.building.queries import (get_inspection_devices_pipeline, other_fn)`)
	assertBuilderJoinTargets(t, rels)
}

// SINGLE-LINE non-paren import still resolves (regression guard for the tightened
// single-line regex `[^\n(#]...`).
func TestMongoAggPy_BuilderJoinFix_SingleLinePlainImport(t *testing.T) {
	rels := scanWithOnDiskQueries(t,
		`from core.services.building.queries import get_inspection_devices_pipeline`)
	assertBuilderJoinTargets(t, rels)
}

// (d) NEGATIVE — a builder whose appended/extended stages are ONLY $match /
// $project (no $lookup) must emit NO JOINS_COLLECTION. We never fabricate a join.
func TestMongoAggPy_BuilderJoinFix_Negative_NoLookupStages(t *testing.T) {
	service := `
import pymongo
from core.services.building.queries import (
    build_match_only,
    other_fn,
)

def run(db):
    return db.inspections.aggregate(build_match_only(db))
`
	queries := `
def build_match_only(db):
    pipeline = [{"$match": {"active": True}}]
    pipeline.append({"$project": {"_id": 1, "name": 1}})
    pipeline.extend([{"$sort": {"name": 1}}])
    return pipeline
`
	_, rels := runMongoAggPyXFile(t, service, map[string]string{"build_match_only": queries})
	if len(rels) != 0 {
		t.Fatalf("a lookup-free builder must emit no JOINS_COLLECTION; got %d: %+v", len(rels), rels)
	}
}

// (d) NEGATIVE — an appended/extended $lookup whose `from:` is a NON-LITERAL
// variable (not a quoted string) must NOT fabricate a collection name. Only the
// literal-from extend stage produces an edge; the variable-from append does not.
func TestMongoAggPy_BuilderJoinFix_Negative_DynamicFrom(t *testing.T) {
	service := `
import pymongo
from core.services.building.queries import (
    build_dynamic,
    other_fn,
)

def run(db):
    return db.inspections.aggregate(build_dynamic(db, "m_users"))
`
	queries := `
def build_dynamic(db, target):
    pipeline = []
    pipeline.append({"$lookup": {"from": target, "localField": "uid", "foreignField": "_id", "as": "u"}})
    pipeline.extend([{"$lookup": {"from": "m_clients", "localField": "cid", "foreignField": "_id", "as": "c"}}])
    return pipeline
`
	_, rels := runMongoAggPyXFile(t, service, map[string]string{"build_dynamic": queries})
	// Only the literal `m_clients` from the extend resolves; the variable `target`
	// from the append must NOT produce a (fabricated) join.
	if pyFindJoinTo(rels, capitalisedSingular("m_clients")) == nil {
		t.Fatalf("expected literal JOINS_COLLECTION to Class:M_client; rels=%+v", rels)
	}
	if len(rels) != 1 {
		t.Fatalf("dynamic `from` var must not emit an edge; want exactly 1 join, got %d: %+v", len(rels), rels)
	}
}
