package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// TestMongoAggLookupNode_FromIDEqualsNodeID_4244 is the anti-false-pass test for
// #4244. It indexes the REAL acme-shaped fixture (mongo_helper_indirection_real
// — multiple `inspections.aggregate(...)` calls on the SAME collection at
// DIFFERENT call-site lines, each `$lookup` with a distinct `from`, mirroring
// building/service.py L18/L28/L38/L57) through the FULL production indexer,
// including graph.EntityID stamping and the post-stamp buildMongoAggStageJoinRels
// pass.
//
// For EVERY `$lookup` / `$graphLookup` SCOPE.DataAccess stage entity it then:
//
//	a. recomputes the node id the SAME way production does —
//	   graph.EntityID(repoTag, kind, name, file) — and asserts it equals the
//	   stamped entity.ID;
//	b. asserts there EXISTS a JOINS_COLLECTION edge whose **FromID == that exact
//	   node id** AND whose ToID == the correct Class:<from> for THAT stage (not
//	   another stage's from);
//	c. drives the actual outgoing-adjacency lookup the live neighbors() query
//	   uses (keyed on the node id) and asserts the edge surfaces.
//
// This is the assertion both prior fixes omitted: they asserted only that "a
// JOINS_COLLECTION edge exists", never that "an edge whose FromID equals the
// $lookup node's graph id exists". The stub-based emission could satisfy the
// former while leaving the node isolated (FromID a synthetic stub) — which is
// exactly what failed live, twice.
func TestMongoAggLookupNode_FromIDEqualsNodeID_4244(t *testing.T) {
	const repoTag = "mongoagg4244"
	abs, err := filepath.Abs("testdata/mongo_helper_indirection_real")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	idx := newTestIndexer(t, repoTag, nil, "")
	doc, err := idx.Run(context.Background(), abs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Outgoing adjacency keyed on FromID — the SAME index the live
	// neighbors()/findCallees query builds (see internal/mcp buildAdjacency:
	// a.out[r.FromID] = append(..., r.ToID)). Building it from the emitted doc
	// proves the edge is reachable FROM the node id.
	type outEdge struct{ kind, to string }
	outgoing := make(map[string][]outEdge)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		outgoing[r.FromID] = append(outgoing[r.FromID], outEdge{r.Kind, r.ToID})
	}

	var lookupStages int
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != string(types.EntityKindDataAccess) {
			continue
		}
		if e.Subtype != "$lookup" && e.Subtype != "$graphLookup" {
			continue
		}
		if e.PropLen() == 0 || e.PropGet("from") == "" {
			continue // a $lookup with a dynamic `from` emits no join — skip.
		}
		lookupStages++

		// (a) Recompute the node id exactly as production does.
		wantID := graph.EntityID(repoTag, e.Kind, e.Name, e.SourceFile)
		if e.ID != wantID {
			t.Fatalf("stage %q: stamped ID %q != graph.EntityID(repo,kind,name,file)=%q",
				e.Name, e.ID, wantID)
		}

		// Collect every expected join target for THIS stage: the top-level
		// `from` plus any recorded nested correlated froms.
		wantTargets := map[string]bool{
			"Class:" + engine.CapitalisedSingular(e.PropGet("from")): true,
		}
		if extra := e.PropGet("join_targets"); extra != "" {
			for _, t := range splitCSV(extra) {
				wantTargets["Class:"+engine.CapitalisedSingular(t)] = true
			}
		}

		// (b) For each expected target, assert an edge FromID==node-id exists,
		// AND its ToID is the correct Class for THIS stage.
		gotTargets := map[string]bool{}
		for _, oe := range outgoing[e.ID] {
			if oe.kind == string(types.RelationshipKindJoinsCollection) {
				gotTargets[oe.to] = true
			}
		}
		for target := range wantTargets {
			if !gotTargets[target] {
				t.Fatalf("stage %q (id=%s): NO JOINS_COLLECTION edge with FromID==node-id to %q; "+
					"node is isolated from its join target (the live bug). got outgoing=%v",
					e.Name, e.ID, target, outgoing[e.ID])
			}
		}
		// Cross-stage-mix guard: the node must NOT carry a join to a `from`
		// that belongs to a different stage. Every JOINS_COLLECTION edge from
		// this node must be one of THIS stage's targets.
		for _, oe := range outgoing[e.ID] {
			if oe.kind != string(types.RelationshipKindJoinsCollection) {
				continue
			}
			if !wantTargets[oe.to] {
				t.Fatalf("stage %q (id=%s): carries a CROSS-STAGE join to %q not among its own targets %v",
					e.Name, e.ID, oe.to, keysOfBoolSet(wantTargets))
			}
		}
	}

	// The fixture mirrors building/service.py and has many `$lookup` stages
	// across four separate aggregations; a regression that stops emitting stage
	// entities (or froms) would zero this out.
	if lookupStages < 10 {
		t.Fatalf("expected >=10 $lookup stages with a static `from` on the acme fixture, got %d", lookupStages)
	}
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func keysOfBoolSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
