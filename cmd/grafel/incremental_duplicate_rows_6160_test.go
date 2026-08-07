package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6160 — Path B (`Index(..., WithIncremental(stateDir))`) persisted the SAME
// row twice, by two independent mechanisms. Both are direct regressions, not
// parity comparisons: the content-parity gate in
// incremental_content_parity_6129_test.go compares full-vs-incremental and
// would stay green if BOTH paths broke identically, so each mechanism gets its
// own assertion against an absolute invariant here.
//
// The invariant both tests assert is the same one buildDocument already
// enforces for the fresh pass: a graph.Document must never hold two rows that
// share one entity / relationship ID. Every seam that appends into a Document
// needs that gate; before this change only buildDocument had one.
//
// Measured on the #6129 corpus BEFORE the fix (the state these tests pin):
//
//	relationship 831aad876ae63d6d  (cp_view_handler →IMPLEMENTS→ http:GET:/cpview)
//	  row 1  {framework, path, pattern_type=http_endpoint_synthesis_time_bridge, verb}   ← this run
//	  row 2  {framework, pattern_type=http_endpoint_synthesis_resolved}                  ← carried forward
//	entity       proc:4bfbff44bf238efd  (SCOPE.Process "http:GET:/cpview → cp_view_handler")
//	  row 1  carried forward by the merge (its SourceFile is an unchanged file)
//	  row 2  re-derived by Pass 7 over the merged graph

// cp6160Endpoint returns the unique /cpview endpoint entity, failing when the
// fixture no longer reaches it (a vacuous assertion is worse than a red one).
func cp6160Endpoint(t *testing.T, d *graph.Document) graph.Entity {
	t.Helper()
	var found []graph.Entity
	for _, e := range d.Entities {
		if e.Kind == "http_endpoint_definition" && e.Name == "http:GET:/cpview" {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 http:GET:/cpview endpoint entity, got %d — "+
			"the fixture no longer reaches the shape this test pins", len(found))
	}
	return found[0]
}

// TestPathB_6160_BridgeEdgeNotDuplicated pins CAUSE 1: the merge's relKey
// included the sorted property payload, so the carried-forward IMPLEMENTS
// bridge (post-resolve form) and this run's freshly synthesized bridge
// (synthesis-time form) — the same logical edge, the same relationship ID —
// hashed to different keys and BOTH persisted.
func TestPathB_6160_BridgeEdgeNotDuplicated(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	cpStatic(t, repo)
	cpDelta(t, repo, 0)
	dvFullRebuild(t, repo, stateDir)

	dvSeedManifest(t, repo, stateDir)
	cpDelta(t, repo, 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	ep := cp6160Endpoint(t, inc)

	var bridges []graph.Relationship
	for _, r := range inc.Relationships {
		if r.Kind == "IMPLEMENTS" && r.ToID == ep.ID {
			bridges = append(bridges, r)
		}
	}
	if len(bridges) == 0 {
		t.Fatalf("no IMPLEMENTS bridge into %s at all — the fixture no longer "+
			"reaches the shape this test pins", ep.ID)
	}
	if len(bridges) != 1 {
		for _, r := range bridges {
			t.Logf("  bridge id=%s %s→%s props=%v", r.ID, r.FromID, r.ToID, r.PropsSnapshot())
		}
		t.Fatalf("#6160: %d IMPLEMENTS bridges into the /cpview endpoint, want 1 — "+
			"the same logical edge persisted once per pipeline stage", len(bridges))
	}
}

// TestPathB_6160_FlowEntitiesNotDuplicated pins CAUSE 2: Pass 7 (process-flow)
// runs over the MERGED graph, and nothing removed the prior run's flow
// entities first, so a Process whose SourceFile was not re-extracted was both
// carried forward and re-derived — twice in the persisted graph, under one ID.
//
// Path A has had this strip since #5309 layer 3
// (engine.RunFlowsIncremental → stripFlows); Path B never did.
func TestPathB_6160_FlowEntitiesNotDuplicated(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	cpStatic(t, repo)
	cpDelta(t, repo, 0)
	dvFullRebuild(t, repo, stateDir)

	dvSeedManifest(t, repo, stateDir)
	cpDelta(t, repo, 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	counts := map[string]int{}
	total := 0
	for _, e := range inc.Entities {
		if e.Kind != "SCOPE.Process" && e.Kind != "SCOPE.EventFlow" {
			continue
		}
		total++
		counts[e.ID]++
	}
	if total == 0 {
		t.Fatal("no flow entities in the incremental graph at all — the fixture " +
			"no longer reaches the shape this test pins")
	}
	for id, n := range counts {
		if n != 1 {
			t.Errorf("#6160: flow entity %s appears %d times in the persisted graph, want 1 — "+
				"carried forward by the merge AND re-derived by Pass 7", id, n)
		}
	}
}

// ── unit-level cover for the merge's identity gate ──

func rel6160(id, from, to, kind string, props map[string]string) graph.Relationship {
	return graph.Relationship{ID: id, FromID: from, ToID: to, Kind: kind}.WithProperties(props)
}

// TestMergeIncrementalPrevSource_6160_ExplicitIDCollisionDeduped is the unit
// statement of cause 1: a prev edge carrying an EXPLICIT ID that the fresh doc
// already holds is the same edge, whatever its payload says, and must not be
// appended a second time.
func TestMergeIncrementalPrevSource_6160_ExplicitIDCollisionDeduped(t *testing.T) {
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "H", Kind: "SCOPE.Operation", Name: "handler", SourceFile: "handler.py"},
			{ID: "E", Kind: "http_endpoint_definition", Name: "http:GET:/x", SourceFile: "handler.py"},
		},
		Relationships: []graph.Relationship{
			rel6160("R1", "H", "E", "IMPLEMENTS", map[string]string{
				"framework": "flask", "pattern_type": "http_endpoint_synthesis_resolved",
			}),
		},
	}
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			rel6160("R1", "H", "E", "IMPLEMENTS", map[string]string{
				"framework": "flask", "path": "/x",
				"pattern_type": "http_endpoint_synthesis_time_bridge", "verb": "GET",
			}),
		},
	}

	mergeIncrementalPrevDoc(doc, prev, map[string]bool{"reg.py": true})

	n := 0
	for _, r := range doc.Relationships {
		if r.ID == "R1" {
			n++
		}
	}
	if n != 1 {
		for _, r := range doc.Relationships {
			t.Logf("  id=%s %s→%s %s props=%v", r.ID, r.FromID, r.ToID, r.Kind, r.PropsSnapshot())
		}
		t.Fatalf("#6160: relationship R1 present %d times after the merge, want 1", n)
	}
}

// TestMergeIncrementalPrevSource_6160_LegacySiblingsSharingOneIDSurvive is the
// guard on what the property-sensitive key was PROTECTING, and the reason the
// gate above is scoped to the IDs THIS RUN emitted rather than applied among
// the prev rows themselves.
//
// A graph.fb written before #6085 persisted no relationship identity, and
// LoadGraphFromDir normalizes an absent ID to the DERIVED one. So the
// deliberately salted siblings that share one (from, to, kind) triple — process
// / event steps salted per step index, migration ops salted per operation, see
// graph.RelationshipIDProperty — arrive at the merge all carrying ONE shared,
// explicit ID, separable only by their payload. An identity gate applied among
// them would collapse three graph rows into one on the upgrade path.
//
// TestMergeIncrementalPrevSource_DedupeKeepsNearIdenticalEdges covers the same
// invariant from the dedupe's side; this one states it in #6160's terms.
func TestMergeIncrementalPrevSource_6160_LegacySiblingsSharingOneIDSurvive(t *testing.T) {
	derived := graph.RelationshipID("P", "O", "STEP_IN_PROCESS")
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "P", Kind: "SCOPE.Process", Name: "p", SourceFile: "a.py"},
			{ID: "O", Kind: "SCOPE.Operation", Name: "o", SourceFile: "a.py"},
		},
		Relationships: []graph.Relationship{
			rel6160(derived, "P", "O", "STEP_IN_PROCESS", map[string]string{"step_index": "0"}),
			rel6160(derived, "P", "O", "STEP_IN_PROCESS", map[string]string{"step_index": "1"}),
			rel6160(derived, "P", "O", "STEP_IN_PROCESS", map[string]string{"step_index": "2"}),
		},
	}
	doc := &graph.Document{}

	mergeIncrementalPrevDoc(doc, prev, map[string]bool{"reg.py": true})

	seen := map[string]int{}
	for _, r := range doc.Relationships {
		if r.Kind != "STEP_IN_PROCESS" {
			continue
		}
		seen[r.PropsSnapshot()["step_index"]]++
	}
	for _, idx := range []string{"0", "1", "2"} {
		if seen[idx] != 1 {
			t.Errorf("step_index=%s present %d time(s) after the merge, want 1 "+
				"(pre-#6085 salted siblings share one id and are separable only by payload)",
				idx, seen[idx])
		}
	}
	if len(seen) != 3 {
		t.Errorf("got %d distinct salted siblings after the merge, want 3: %v", len(seen), seen)
	}
}
