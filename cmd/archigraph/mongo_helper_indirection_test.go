// Full-pipeline integration test for the Mongo `$lookup → JOINS_COLLECTION`
// helper-indirection real shape (deploy-9 REFUTED item-1).
//
// The rewrite agent reported that on upvate-core the `$lookup.from` collection
// literals never reach JOINS_COLLECTION edges. The prior tests were
// extraction-only on crafted fixtures and missed it twice because the REAL
// access shape combines FOUR things together that no single crafted fixture
// exercised end-to-end:
//
//  1. The aggregating collection comes through a CONNECTION-HELPER indirection
//     — `MongoDBConnection.get_collection(INSPECTIONS)` — bound to a local
//     `inspections_cln` variable, not a direct `db.coll`.
//  2. The collection name is a MODULE-LEVEL CONSTANT (`INSPECTIONS`) imported
//     from a sibling module, not a quoted string at the call site.
//  3. The pipeline is built by a SEPARATE builder function in `queries.py`,
//     imported via a MULTI-LINE `from ... import (...)` clause, and passed as a
//     DIRECT CALL argument: `inspections_cln.aggregate(builder(params))`.
//  4. The builder ASSEMBLES the pipeline imperatively — `pipeline = [ ... ]`
//     then `pipeline.append({...})` / `pipeline.extend([ ... ])` — with both
//     top-level and NESTED correlated `$lookup` stages, then `return pipeline`.
//
// Two fixtures drive the entire production index pipeline (the same
// Indexer.Run the daemon invokes on rebuild):
//
//   - testdata/mongo_helper_indirection      — a minimal hand-written fixture
//     isolating the four conditions above with a nested correlated lookup.
//   - testdata/mongo_helper_indirection_real — the VERBATIM upvate-core source
//     (queries.py / service.py / mongo_helper.py / mongodb_collections.py) that
//     was REFUTED, copied read-only so the regression is pinned to the exact
//     bytes the rewrite agent saw.
//
// Both assert JOINS_COLLECTION edges to the SPECIFIC named collections survive
// all the way to the final graph.Document. An extraction-only unit test is
// explicitly NOT sufficient — that is what missed this twice.
package main

import "testing"

// TestMongoLookupHelperIndirection_MinimalShape pins the four-condition shape
// in a small hand-written fixture so a future regression points straight at the
// offending stage rather than drowning in the 1300-line real file.
func TestMongoLookupHelperIndirection_MinimalShape(t *testing.T) {
	doc := runIndexerOn(t, "testdata/mongo_helper_indirection", "mongo_helper_indirection", nil)

	type edge struct{ from, to string }
	var joins []edge
	for _, r := range doc.Relationships {
		if r.Kind != "JOINS_COLLECTION" {
			continue
		}
		// #4244 — skip the per-stage node-anchored twin (FromID = the $lookup
		// node's hex id). This test asserts the collection-anchored edges
		// (Class:Inspection -> Class:<from>); the node-anchored twins are
		// covered by TestMongoAggLookupNode_FromIDEqualsNodeID_4244.
		if r.Properties != nil && r.Properties["anchor"] == "stage_node" {
			continue
		}
		joins = append(joins, edge{from: r.FromID, to: r.ToID})
	}

	if len(joins) == 0 {
		kindCounts := map[string]int{}
		for _, r := range doc.Relationships {
			kindCounts[r.Kind]++
		}
		t.Logf("graph stats: entities=%d relationships=%d", len(doc.Entities), len(doc.Relationships))
		t.Logf("relationship kinds in graph: %v", kindCounts)
		t.Fatalf("deploy-9 item-1: expected JOINS_COLLECTION edges from the helper-indirection aggregate shape, got 0 — the $lookup.from literals never reached the graph")
	}

	// Every join originates at the resolved aggregating collection
	// (Class:Inspection), NOT a phantom (Class:INSPECTIONS / a mangled var
	// name). Receiver flow: inspections_cln -> get_collection(INSPECTIONS) ->
	// "inspections" -> capitalisedSingular -> "Inspection".
	const wantFrom = "Class:Inspection"

	wantTargets := map[string]bool{
		"Class:Inspection_group":       false, // top-level "inspection_groups"
		"Class:M_device":               false, // top-level "m_devices"
		"Class:M_contract":             false, // top-level "m_contracts"
		"Class:M_group_device_setting": false, // NESTED "m_group_device_settings"
	}
	for _, j := range joins {
		if _, ok := wantTargets[j.to]; ok {
			wantTargets[j.to] = true
			if j.from != wantFrom {
				t.Errorf("JOINS_COLLECTION to %s has FromID %q, want %q (aggregating collection must resolve through the get_collection helper indirection)", j.to, j.from, wantFrom)
			}
		}
	}
	for target, seen := range wantTargets {
		if !seen {
			t.Errorf("deploy-9 item-1: missing JOINS_COLLECTION edge %s -> %s; observed joins: %+v", wantFrom, target, joins)
		}
	}
}

// TestMongoLookupHelperIndirection_RealUpvateSource indexes the VERBATIM
// upvate-core building-service source that was reported REFUTED at deploy-9 and
// asserts that every DISTINCT `$lookup.from` collection in the real builder
// (15 of them, spanning top-level and nested correlated lookups, multi-line
// extend-assembled pipelines, and four builder functions) becomes a
// JOINS_COLLECTION edge from Class:Inspection through to the final graph.json.
func TestMongoLookupHelperIndirection_RealUpvateSource(t *testing.T) {
	doc := runIndexerOn(t, "testdata/mongo_helper_indirection_real", "upvate_building", nil)

	const wantFrom = "Class:Inspection"
	got := map[string]bool{}
	for _, r := range doc.Relationships {
		if r.Kind != "JOINS_COLLECTION" {
			continue
		}
		// #4244 — the per-stage `$lookup` node also emits a NODE-ANCHORED twin
		// JOINS_COLLECTION edge whose FromID is the stage node's graph id (a hex
		// id), not Class:Inspection. This test asserts the COLLECTION-anchored
		// edges (the helper-indirection resolution), so skip the node-anchored
		// twins (Properties["anchor"]=="stage_node").
		if r.Properties != nil && r.Properties["anchor"] == "stage_node" {
			continue
		}
		if r.FromID != wantFrom {
			t.Errorf("real source: JOINS_COLLECTION to %s has FromID %q, want %q — the get_collection(INSPECTIONS) helper indirection did not resolve to the real collection node", r.ToID, r.FromID, wantFrom)
		}
		got[r.ToID] = true
	}

	// The 15 distinct collections joined by the real builder's $lookup stages
	// (`grep -oE '"from": "[a-z_]+"' queries.py | sort -u`). Includes the two
	// real (mis)spellings of the alternate-address collection, the nested
	// correlated m_group_device_settings, and the extend-assembled targets.
	want := []string{
		"Class:Inspection_group",             // inspection_groups
		"Class:Inspections_history",          // inspections_history
		"Class:M_building_alternate_address", // m_building_alternate_addresses
		"Class:M_building_alternate_adress",  // m_building_alternate_adresses (real typo)
		"Class:M_building",                   // m_buildings
		"Class:M_client",                     // m_clients
		"Class:M_contract",                   // m_contracts
		"Class:M_device_equipment_type",      // m_device_equipment_types
		"Class:M_device",                     // m_devices
		"Class:M_group_device_setting",       // m_group_device_settings (nested)
		"Class:M_jurisdiction",               // m_jurisdictions
		"Class:M_user",                       // m_users
		"Class:Me_page_version",              // me_page_versions
		"Class:Me_page",                      // me_pages
		"Class:Me_report_inspections_group",  // me_report_inspections_group
	}

	if len(got) == 0 {
		kindCounts := map[string]int{}
		for _, r := range doc.Relationships {
			kindCounts[r.Kind]++
		}
		t.Logf("graph stats: entities=%d relationships=%d", len(doc.Entities), len(doc.Relationships))
		t.Logf("relationship kinds in graph: %v", kindCounts)
		t.Fatalf("deploy-9 item-1 (REAL source): expected JOINS_COLLECTION edges from the real upvate building service, got 0")
	}
	gotList := make([]string, 0, len(got))
	for k := range got {
		gotList = append(gotList, k)
	}
	for _, target := range want {
		if !got[target] {
			t.Errorf("deploy-9 item-1 (REAL source): missing JOINS_COLLECTION edge %s -> %s; got targets: %v", wantFrom, target, gotList)
		}
	}
}
