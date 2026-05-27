// Package main — incremental_merge_test.go
//
// Regression tests for #2719 Path B (CLI `archigraph rebuild --incremental`).
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

	"github.com/cajasmota/archigraph/internal/graph"
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
	// Both prev edges still point at live endpoints → both carried forward.
	if len(doc.Relationships) != 2 {
		t.Errorf("relationships=%d want 2 (ab,bc carried forward)", len(doc.Relationships))
	}
}
