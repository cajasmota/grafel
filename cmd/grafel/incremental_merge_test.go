// Package main — incremental_merge_test.go
//
// Regression tests for #2719 Path B (CLI `grafel rebuild --incremental`).
//
// Before #2719 the indexer's incremental branch ran the extraction pipeline
// against only the changed-file subset and then wrote the resulting document
// verbatim — every unchanged-file entity from the previous graph was silently
// dropped, leaving callers with a tiny fraction of the real graph on disk.
//
// `mergeIncrementalPrevDoc` is the helper that stitches the previous graph's
// unchanged-file portion back into the current run's document. These tests
// pin its behaviour: entity carry-forward, ID-collision precedence, dangling
// edge pruning, and synthetic-entity handling.
package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestMergeIncrementalPrevDoc_CarriesForwardUnchangedFileEntities(t *testing.T) {
	// Three files; only "b.go" was reindexed this run. Entities sourced
	// from a.go and c.go must be carried forward from the previous doc.
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "A", Name: "AlphaFn", Kind: "SCOPE.Operation", SourceFile: "a.go"},
			{ID: "B_old", Name: "BetaFn", Kind: "SCOPE.Operation", SourceFile: "b.go"},
			{ID: "C", Name: "GammaFn", Kind: "SCOPE.Operation", SourceFile: "c.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "A->B_old", FromID: "A", ToID: "B_old", Kind: "CALLS"},
			{ID: "C->A", FromID: "C", ToID: "A", Kind: "CALLS"},
		},
	}
	// Current run reindexed only b.go; the freshly-extracted Beta has the
	// same kind/name/source_file so its ID is identical (deterministic) —
	// for the test we simulate the ID staying stable as "B_old".
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "B_old", Name: "BetaFn", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
		Relationships: []graph.Relationship{},
	}
	changed := map[string]bool{"b.go": true}

	stats := mergeIncrementalPrevDoc(doc, prev, changed)

	if stats.entitiesAdded != 2 {
		t.Errorf("entitiesAdded=%d want 2 (A,C)", stats.entitiesAdded)
	}
	wantIDs := map[string]bool{"A": true, "B_old": true, "C": true}
	if len(doc.Entities) != 3 {
		t.Fatalf("doc.Entities count=%d want 3, got=%+v", len(doc.Entities), doc.Entities)
	}
	for _, e := range doc.Entities {
		if !wantIDs[e.ID] {
			t.Errorf("unexpected entity ID %s in merged doc", e.ID)
		}
	}
	// Both prev rels point at surviving endpoints → both carried forward.
	if stats.relsAdded != 2 {
		t.Errorf("relsAdded=%d want 2", stats.relsAdded)
	}
	if stats.relsDropped != 0 {
		t.Errorf("relsDropped=%d want 0", stats.relsDropped)
	}
	if doc.Stats.Entities != 3 {
		t.Errorf("doc.Stats.Entities=%d want 3", doc.Stats.Entities)
	}
	if doc.Stats.Relationships != 2 {
		t.Errorf("doc.Stats.Relationships=%d want 2", doc.Stats.Relationships)
	}
}

func TestMergeIncrementalPrevDoc_DropsRelsIntoChangedFileEntities(t *testing.T) {
	// Previous graph has an entity in a.go (unchanged) AND an entity in
	// b.go (changed) — the b.go entity must NOT come back from prev (the
	// fresh extraction is the canonical version). Prev edges incident to
	// the old b.go entity ID must be dropped if the fresh run renamed it.
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "A", Name: "AlphaFn", Kind: "SCOPE.Operation", SourceFile: "a.go"},
			{ID: "B_stale", Name: "BetaOld", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "A->B_stale", FromID: "A", ToID: "B_stale", Kind: "CALLS"},
		},
	}
	// Fresh run produced a different name (different ID) for b.go's entity.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "B_new", Name: "BetaRenamed", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
	}
	stats := mergeIncrementalPrevDoc(doc, prev, map[string]bool{"b.go": true})

	if stats.entitiesAdded != 1 {
		t.Errorf("entitiesAdded=%d want 1 (only A)", stats.entitiesAdded)
	}
	// B_stale must NOT be in merged doc.
	for _, e := range doc.Entities {
		if e.ID == "B_stale" {
			t.Error("stale prev entity from changed file leaked into merged doc")
		}
	}
	// Edge into stale B_stale must be dropped.
	if stats.relsDropped != 1 {
		t.Errorf("relsDropped=%d want 1 (A->B_stale)", stats.relsDropped)
	}
	if stats.relsAdded != 0 {
		t.Errorf("relsAdded=%d want 0", stats.relsAdded)
	}
}

func TestMergeIncrementalPrevDoc_SkipsSyntheticPrevEntities(t *testing.T) {
	// ext:* synthetic entities (no source_file) are regenerated downstream
	// by external.Synthesize; mergeIncrementalPrevDoc must NOT carry them
	// forward.
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "EXT", Name: "ext:fmt", Kind: "SCOPE.Operation", SourceFile: ""},
			{ID: "A", Name: "AlphaFn", Kind: "SCOPE.Operation", SourceFile: "a.go"},
		},
	}
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "B", Name: "BetaFn", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
	}
	stats := mergeIncrementalPrevDoc(doc, prev, map[string]bool{"b.go": true})

	if stats.entitiesAdded != 1 {
		t.Errorf("entitiesAdded=%d want 1 (just A, not EXT)", stats.entitiesAdded)
	}
	for _, e := range doc.Entities {
		if e.ID == "EXT" {
			t.Error("synthetic ext:* entity must not be carried forward by merge step")
		}
	}
}

func TestMergeIncrementalPrevDoc_DocEntityWinsOnIDCollision(t *testing.T) {
	// If an entity ID is present in BOTH prev and doc (deterministic ID
	// stayed stable across re-extraction), the doc version wins — it
	// reflects the current source code.
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "X", Name: "OldName", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
	}
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "X", Name: "FreshName", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
	}
	mergeIncrementalPrevDoc(doc, prev, map[string]bool{"b.go": true})

	if len(doc.Entities) != 1 {
		t.Fatalf("collision should not duplicate, got %d entities", len(doc.Entities))
	}
	if doc.Entities[0].Name != "FreshName" {
		t.Errorf("doc entity should win on collision, got name=%s", doc.Entities[0].Name)
	}
}

func TestMergeIncrementalPrevDoc_NilSafetyAndEmptyDocs(t *testing.T) {
	// Nil prev / doc must not panic.
	stats := mergeIncrementalPrevDoc(nil, nil, nil)
	if stats.entitiesAdded != 0 || stats.relsAdded != 0 {
		t.Errorf("nil inputs should yield zero stats, got %+v", stats)
	}
	doc := &graph.Document{}
	prev := &graph.Document{}
	stats = mergeIncrementalPrevDoc(doc, prev, map[string]bool{})
	if stats.entitiesAdded != 0 || stats.relsAdded != 0 {
		t.Errorf("empty inputs should yield zero stats, got %+v", stats)
	}
}

// TestMergeIncrementalPrevDoc_ThreeFileScenario is the headline regression
// scenario described in #2719: a 3-file repo where ONE file is modified in
// an incremental run; the merged graph MUST contain entities from ALL THREE
// files (not just the changed one).
func TestMergeIncrementalPrevDoc_ThreeFileScenario(t *testing.T) {
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "ID_a", Name: "A", Kind: "SCOPE.Operation", SourceFile: "a.go"},
			{ID: "ID_b", Name: "B", Kind: "SCOPE.Operation", SourceFile: "b.go"},
			{ID: "ID_c", Name: "C", Kind: "SCOPE.Operation", SourceFile: "c.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "ab", FromID: "ID_a", ToID: "ID_b", Kind: "CALLS"},
			{ID: "bc", FromID: "ID_b", ToID: "ID_c", Kind: "CALLS"},
		},
	}
	// Current run touched only b.go; the fresh extraction re-emitted B
	// with the same deterministic ID.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "ID_b", Name: "B", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
	}
	mergeIncrementalPrevDoc(doc, prev, map[string]bool{"b.go": true})

	// All three files' entities must survive the incremental merge.
	wantSourceFiles := map[string]bool{"a.go": true, "b.go": true, "c.go": true}
	gotSourceFiles := map[string]bool{}
	for _, e := range doc.Entities {
		gotSourceFiles[e.SourceFile] = true
	}
	for f := range wantSourceFiles {
		if !gotSourceFiles[f] {
			t.Errorf("merged doc missing entities from %s; #2719 regression", f)
		}
	}
	// Edge carry-forward, per the #6085 invariant:
	//   ab (a.go → b.go) is anchored in the UNCHANGED a.go: this run never
	//     re-emitted it, so it must survive. B kept its ID, so it is not a
	//     dangling pointer either.
	//   bc (b.go → c.go) is anchored in the RE-EXTRACTED b.go. The fresh pass
	//     emitted no outgoing edge for B, which means the call is gone from
	//     the source — the prev copy is superseded and must be dropped.
	// (Before #6085 this asserted 2: bc was retained purely because both of
	// its endpoint rows happened to still exist, so a deleted call lived on
	// in the graph indefinitely.)
	if len(doc.Relationships) != 1 {
		t.Errorf("relationships=%d want 1 (ab carried forward, bc superseded)", len(doc.Relationships))
	}
	for _, r := range doc.Relationships {
		if r.FromID == "ID_b" && r.ToID == "ID_c" {
			t.Error("edge out of the re-extracted b.go survived; the fresh pass did not re-emit it")
		}
	}
	if len(doc.Relationships) == 1 && doc.Relationships[0].FromID != "ID_a" {
		t.Errorf("surviving edge = %s→%s, want ID_a→ID_b",
			doc.Relationships[0].FromID, doc.Relationships[0].ToID)
	}
}

// ---------------------------------------------------------------------------
// #6085 — the incremental merge dropped ~35% of the graph on every run.
//
// The invariant these tests pin, in both directions:
//
//	a prev relationship is dropped IFF it is genuinely superseded by this
//	run's re-extraction — i.e. its FromID is an entity anchored in a file we
//	just re-extracted (that file re-emitted its outgoing edges), or its ToID
//	is an entity anchored in a re-extracted file that no longer exists.
//
// The pre-#6085 predicate was "both endpoints resolve to a live entity row",
// which is wrong in BOTH directions:
//
//   - too aggressive: it deleted every edge whose endpoint is not an entity
//     row — ext:* synthetics and unresolved bare-name targets — even though a
//     full reindex keeps them. On archigraph that was 347,946 edges after a
//     ONE-file change.
//   - too conservative: it KEPT an edge out of a changed file whenever the
//     re-extraction happened to mint the same entity ID, so a call deleted
//     from the source survived in the graph forever.
// ---------------------------------------------------------------------------

// TestMergeIncrementalPrevDoc_KeepsEdgesIntoNonEntityEndpoints pins the
// "too aggressive" direction: edges into synthetic ext:* nodes and into
// unresolved bare-name targets are NOT stale and must be carried forward.
// Fails on e43dbeab9 (both edges dropped, ext node lost).
func TestMergeIncrementalPrevDoc_KeepsEdgesIntoNonEntityEndpoints(t *testing.T) {
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "A", Name: "AlphaFn", Kind: "SCOPE.Operation", SourceFile: "a.go"},
			// Synthetic external: no source file. external.Synthesize minted
			// it on a previous run and persisted it into the graph.
			{ID: "ext:fmt", Name: "fmt", Kind: "SCOPE.External", SourceFile: ""},
			// A synthetic nothing points at: must NOT be resurrected.
			{ID: "ext:orphan", Name: "orphan", Kind: "SCOPE.External", SourceFile: ""},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "A", ToID: "ext:fmt", Kind: "IMPORTS"},
			// Unresolved bare-name target: never an entity row, in the full
			// reindex either. Still a real edge the graph ships.
			{ID: "r2", FromID: "A", ToID: "Join", Kind: "CALLS"},
		},
	}
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "B", Name: "BetaFn", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
	}

	stats := mergeIncrementalPrevDoc(doc, prev, map[string]bool{"b.go": true})

	if stats.relsDropped != 0 {
		t.Errorf("relsDropped=%d want 0: neither edge is superseded by re-extracting b.go", stats.relsDropped)
	}
	if stats.relsAdded != 2 {
		t.Errorf("relsAdded=%d want 2 (IMPORTS→ext:fmt, CALLS→bare name)", stats.relsAdded)
	}
	got := map[string]bool{}
	for _, r := range doc.Relationships {
		got[r.FromID+"->"+r.ToID] = true
	}
	for _, want := range []string{"A->ext:fmt", "A->Join"} {
		if !got[want] {
			t.Errorf("edge %s lost by the incremental merge (#6085)", want)
		}
	}
	// The ext node a surviving edge points at must come back so the carried
	// edge does not dangle; the unreferenced one must not.
	ents := map[string]bool{}
	for _, e := range doc.Entities {
		ents[e.ID] = true
	}
	if !ents["ext:fmt"] {
		t.Error("ext:fmt is referenced by a carried edge but was not carried forward — dangling edge")
	}
	if ents["ext:orphan"] {
		t.Error("unreferenced synthetic ext:orphan must not be carried forward")
	}
}

// TestMergeIncrementalPrevDoc_DropsSupersededEdgeOutOfChangedFile pins the
// "too conservative" direction: when a changed file's entity keeps its ID but
// the source no longer contains the call, the prev edge MUST go. A fix that
// simply drops fewer edges fails this test.
func TestMergeIncrementalPrevDoc_DropsSupersededEdgeOutOfChangedFile(t *testing.T) {
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "A", Name: "AlphaFn", Kind: "SCOPE.Operation", SourceFile: "a.go"},
			{ID: "B", Name: "BetaFn", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
		Relationships: []graph.Relationship{
			// b.go used to call a.go. The edit under test deleted that call.
			{ID: "rBA", FromID: "B", ToID: "A", Kind: "CALLS"},
			// a.go calls b.go — anchored in an UNCHANGED file, so this run
			// never re-emitted it and it must be preserved.
			{ID: "rAB", FromID: "A", ToID: "B", Kind: "CALLS"},
		},
	}
	// Fresh extraction of b.go: same deterministic ID for B, no outgoing call.
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "B", Name: "BetaFn", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
	}

	stats := mergeIncrementalPrevDoc(doc, prev, map[string]bool{"b.go": true})

	for _, r := range doc.Relationships {
		if r.FromID == "B" && r.ToID == "A" {
			t.Error("edge out of the re-extracted file survived the merge — the deleted call is still in the graph")
		}
	}
	if stats.relsDropped != 1 {
		t.Errorf("relsDropped=%d want 1 (only B→A is superseded)", stats.relsDropped)
	}
	found := false
	for _, r := range doc.Relationships {
		if r.FromID == "A" && r.ToID == "B" {
			found = true
		}
	}
	if !found {
		t.Error("edge from the UNCHANGED file into the changed file was dropped — it was never re-emitted, so it is lost")
	}
}

// TestMergeIncrementalPrevDoc_RestoresRelationshipIDAndDedupes pins the
// duplicate-accumulation half of #6085. graph.fb does not persist
// Relationship.ID, so prev edges arrive with an empty one; keying the dedupe
// on that empty ID meant every incremental run re-appended edges the fresh
// pass had already emitted, and the graph grew without bound.
func TestMergeIncrementalPrevDoc_RestoresRelationshipIDAndDedupes(t *testing.T) {
	prev := &graph.Document{
		Entities: []graph.Entity{
			{ID: "A", Name: "AlphaFn", Kind: "SCOPE.Operation", SourceFile: "a.go"},
			{ID: "C", Name: "GammaFn", Kind: "SCOPE.Operation", SourceFile: "c.go"},
		},
		Relationships: []graph.Relationship{
			// Both arrive ID-less, exactly as LoadGraphFromDir returned them
			// before the fbRelToGraphRel fix.
			{FromID: "A", ToID: "C", Kind: "CALLS"},
			{FromID: "C", ToID: "A", Kind: "CALLS"},
		},
	}
	// The fresh run already emitted A→C (with a proper ID).
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "B", Name: "BetaFn", Kind: "SCOPE.Operation", SourceFile: "b.go"},
		},
		Relationships: []graph.Relationship{
			{ID: graph.RelationshipID("A", "C", "CALLS"), FromID: "A", ToID: "C", Kind: "CALLS"},
		},
	}

	stats := mergeIncrementalPrevDoc(doc, prev, map[string]bool{"b.go": true})

	if stats.relsAdded != 1 {
		t.Errorf("relsAdded=%d want 1 (A→C already present, only C→A is new)", stats.relsAdded)
	}
	if len(doc.Relationships) != 2 {
		t.Fatalf("relationships=%d want 2 — the merge duplicated an edge the fresh pass had emitted", len(doc.Relationships))
	}
	for _, r := range doc.Relationships {
		if r.ID == "" {
			t.Errorf("carried edge %s→%s has no ID; consumers key on it", r.FromID, r.ToID)
		}
		if want := graph.RelationshipID(r.FromID, r.ToID, r.Kind); r.ID != want {
			t.Errorf("edge %s→%s ID=%q want %q", r.FromID, r.ToID, r.ID, want)
		}
	}
}
