// Package extractors — incremental_entity_dedupe_6161_test.go
//
// #6161 unit gates for the ENTITY half of the record→graph seam on the
// incremental path. convertExtractedRecords appended every record's entity
// unconditionally, so two records deriving the same graph.EntityID became two
// rows in doc.Entities — while the relationship half three lines below already
// carried the #6094 seenRel guard.
//
// WHY DUPLICATE IDs ARE NOT A HYPOTHETICAL. graph.EntityID is
// sha256(repo, kind, name, source_file) and deliberately EXCLUDES StartLine, so
// every construct that declares one name twice in one file collides by
// construction: Java method overloads (see custom_dispatch.go's span-union
// comment, which exists for exactly this), C#/VB partial classes and partial
// methods, C++/TypeScript overload declarations, Python @overload /
// @singledispatch / `def` under `if TYPE_CHECKING`, Ruby reopened classes.
//
// WHY THE FIX IS A FOLD, NOT A GATE. Because the collision is expected rather
// than erroneous, a bare `if seen { continue }` would DISCARD the second
// overload's relationships — strictly worse than the duplication it removes,
// which is at least visible in a row count. buildDocument
// (cmd/grafel/index.go) already resolved this: it gates the entity, gap-fills
// base-only fields from the duplicate onto the survivor, and lets the
// relationship loop run for EVERY record so the dropped record's edges anchor
// to the survivor's id. This file pins that same three-part contract on the
// incremental path.
//
// THE THREE BEHAVIOURS ARE PINNED INDEPENDENTLY, on purpose:
//
//	_FoldsToOneRow      dies if the gate is removed (the #6161 regression).
//	_OverloadEdges…     dies if the gate DISCARDS instead of folding, and is
//	                    deliberately blind to the entity count so that it
//	                    cannot pass or fail for the gate's reasons.
//	_GapFillsSurvivor   dies if the gap-fill is removed.
package extractors

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// dupRec builds an EntityRecord at an explicit line span. Two dupRecs sharing
// (kind, name, file) derive the SAME graph.EntityID however far apart their
// lines are — that is the whole premise of this file.
func dupRec(kind, name, file string, startLine, endLine int, rels ...types.RelationshipRecord) types.EntityRecord {
	return types.EntityRecord{
		Kind:          kind,
		Name:          name,
		QualifiedName: name,
		SourceFile:    file,
		Language:      "java",
		StartLine:     startLine,
		EndLine:       endLine,
		Relationships: rels,
	}
}

// relTargets renders the edges as sorted "to:kind" strings.
func relTargets(rels []graph.Relationship) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.ToID+":"+r.Kind)
	}
	sort.Strings(out)
	return out
}

// TestConvertExtractedRecords_SameEntityIDFoldsToOneRow is the direct #6161
// regression gate.
//
// Two records with the same (Kind, Name, SourceFile) at different lines — the
// Java-overload shape — derive one graph.EntityID and must therefore produce
// ONE row. Two rows sharing an ID breaks the invariant stated verbatim at
// internal/graph/emission_order.go:32 ("Entity IDs are unique, so ID alone is a
// total order — no secondary keys"), on which SortDocumentForEmission and the
// FlatBuffers binary search behind LookupEntityByID both depend: one row is
// returned arbitrarily and the other is permanently unreachable while still
// occupying a slot and a count.
//
// This test asserts on the entity count ONLY. It says nothing about the
// duplicate's edges or fields, so it cannot stand in for the two tests below.
func TestConvertExtractedRecords_SameEntityIDFoldsToOneRow(t *testing.T) {
	const repo = "r"

	recs := []types.EntityRecord{
		dupRec("SCOPE.Operation", "go", "Over.java", 10, 12),
		dupRec("SCOPE.Operation", "go", "Over.java", 14, 16),
	}
	ents, _ := convertExtractedRecords(recs, repo, map[string]bool{})

	if len(ents) != 1 {
		ids := make([]string, 0, len(ents))
		for _, e := range ents {
			ids = append(ids, e.ID)
		}
		t.Fatalf("two records deriving one graph.EntityID produced %d entity rows, want 1 — "+
			"duplicate IDs make one row permanently unreachable through LookupEntityByID (#6161).\nids: %v",
			len(ents), ids)
	}
	if want := graph.EntityID(repo, "SCOPE.Operation", "go", "Over.java"); ents[0].ID != want {
		t.Fatalf("survivor ID = %q, want the derived %q", ents[0].ID, want)
	}
	// The survivor is the FIRST record, matching buildDocument's first-wins rule.
	// A last-wins fold would make the two paths disagree on line coordinates.
	if ents[0].StartLine != 10 {
		t.Errorf("survivor StartLine = %d, want 10 — the FIRST record must win, as buildDocument's fold does",
			ents[0].StartLine)
	}
}

// TestConvertExtractedRecords_OverloadEdgesAnchorToSurvivor is the test that
// stops the #6161 fix from being a deletion.
//
// Both overloads' relationships must reach the graph, anchored to the surviving
// entity's id. A gate that `continue`s past the duplicate before the
// relationship loop silently loses the second overload's entire edge set —
// invisible to any row-count assertion, and a worse defect than the duplication
// it removes.
//
// DELIBERATELY BLIND TO THE ENTITY COUNT. This test passes both before the fix
// (two rows, both edges) and after (one row, both edges), so it fails for
// exactly one reason: edge loss. Do not add a len(ents) assertion here — that
// would make it die alongside the regression test above and destroy the
// independence that makes the mutant matrix meaningful.
func TestConvertExtractedRecords_OverloadEdgesAnchorToSurvivor(t *testing.T) {
	const repo = "r"

	// The real shape: `void go(int)` calls helperInt, `void go(String)` calls
	// helperStr. Both carry an omitted FromID, i.e. "owned by my record".
	recs := []types.EntityRecord{
		dupRec("SCOPE.Operation", "go", "Over.java", 10, 12,
			ceRel("", "method:Over.helperInt", "CALLS", nil)),
		dupRec("SCOPE.Operation", "go", "Over.java", 14, 16,
			ceRel("", "method:Over.helperStr", "CALLS", nil)),
	}
	_, rels := convertExtractedRecords(recs, repo, map[string]bool{})

	want := []string{"method:Over.helperInt:CALLS", "method:Over.helperStr:CALLS"}
	got := relTargets(rels)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("both overloads' edges must survive the entity fold — a gate that discards the "+
			"duplicate record loses the second overload's call graph entirely (#6161).\ngot:  %v\nwant: %v",
			got, want)
	}

	survivor := graph.EntityID(repo, "SCOPE.Operation", "go", "Over.java")
	for _, r := range rels {
		if r.FromID != survivor {
			t.Errorf("edge →%s:%s is anchored to %q, want the surviving entity %q — an edge anchored "+
				"to a dropped row dangles", r.ToID, r.Kind, r.FromID, survivor)
		}
		if wantID := graph.RelationshipID(r.FromID, r.ToID, r.Kind); r.ID != wantID {
			t.Errorf("edge %s→%s:%s has ID %q, want %q", r.FromID, r.ToID, r.Kind, r.ID, wantID)
		}
	}
}

// TestConvertExtractedRecords_GapFillsSurvivor pins the third part of the
// contract, ported from buildDocument's cmd/grafel/index.go dedup branch
// (issue #4406): the dropped duplicate is frequently the carrier of base-only
// state the survivor lacks — most critically the module-qualified
// QualifiedName that drives byQualifiedName resolution and cross-repo joins.
//
// Semantics are gap-fill, NOT override: a field the survivor already provided
// is never overwritten. Both directions are asserted, because a fold that
// overrides is as wrong as one that does not fill.
func TestConvertExtractedRecords_GapFillsSurvivor(t *testing.T) {
	const repo = "r"

	survivorRec := types.EntityRecord{
		Kind: "SCOPE.Operation", Name: "go", SourceFile: "Over.java",
		StartLine: 10, EndLine: 12,
		// QualifiedName and Signature deliberately absent.
		Language: "java",
		Subtype:  "method",
	}
	duplicateRec := types.EntityRecord{
		Kind: "SCOPE.Operation", Name: "go", SourceFile: "Over.java",
		StartLine: 14, EndLine: 16,
		QualifiedName: "com.example.Over.go",
		Signature:     "void go(String)",
		Language:      "java",
		// A value the survivor ALREADY has — must not win.
		Subtype: "constructor",
		Tags:    []string{"overload"},
	}

	ents, _ := convertExtractedRecords(
		[]types.EntityRecord{survivorRec, duplicateRec}, repo, map[string]bool{})

	if len(ents) == 0 {
		t.Fatal("no entities produced")
	}
	surv := ents[0]
	if surv.QualifiedName != "com.example.Over.go" {
		t.Errorf("QualifiedName = %q, want %q gap-filled from the dropped duplicate — losing it "+
			"is the live-graph half of #4402/#4406", surv.QualifiedName, "com.example.Over.go")
	}
	if surv.Signature != "void go(String)" {
		t.Errorf("Signature = %q, want %q gap-filled from the dropped duplicate",
			surv.Signature, "void go(String)")
	}
	if surv.Subtype != "method" {
		t.Errorf("Subtype = %q, want %q — gap-fill must never OVERRIDE a value the survivor provided",
			surv.Subtype, "method")
	}
	if surv.StartLine != 10 || surv.EndLine != 12 {
		t.Errorf("line span = %d-%d, want 10-12 — the survivor's own non-zero span must stand. "+
			"A blind span union invents a third span covering neither overload (custom_dispatch.go:507).",
			surv.StartLine, surv.EndLine)
	}
	var hasTag bool
	for _, tag := range surv.Tags {
		if tag == "overload" {
			hasTag = true
		}
	}
	if !hasTag {
		t.Errorf("Tags = %v, want the duplicate's %q unioned in, as buildDocument's fold does",
			surv.Tags, "overload")
	}
}

// TestConvertExtractedRecords_GapFillNeverOverrides is the other half of the
// gap-fill contract, and it is a SEPARATE test from _GapFillsSurvivor because
// the two failure modes are opposites: one fold does too little, the other does
// too much, and a single test that mixed them could not tell you which.
//
// Every field is its own SUBTEST on purpose. Each `if surv.X == "" ` clause in
// foldDuplicateEntity is an independent mutation site — flipping any one of them
// to an unconditional assignment is a separate defect — so each gets a named
// case that dies alone. A single flat test here would report the same name for
// every one of those mutants and tell you nothing about which clause broke.
//
// QualifiedName is the one that matters most, and it was the one previously
// unpinned: foldDuplicateEntity's own comment calls it load-bearing for
// byQualifiedName resolution and cross-repo joins (#4402/#4406). Overriding a
// survivor's module-qualified name with a duplicate's is precisely the join-key
// corruption that fix existed to prevent.
func TestConvertExtractedRecords_GapFillNeverOverrides(t *testing.T) {
	const repo = "r"

	// fold runs one survivor/duplicate pair through the seam and returns the
	// surviving row. Both records derive the same EntityID by construction.
	fold := func(t *testing.T, survivor, duplicate types.EntityRecord) graph.Entity {
		t.Helper()
		survivor.Kind, survivor.Name, survivor.SourceFile = "SCOPE.Operation", "go", "Over.java"
		duplicate.Kind, duplicate.Name, duplicate.SourceFile = "SCOPE.Operation", "go", "Over.java"
		ents, _ := convertExtractedRecords(
			[]types.EntityRecord{survivor, duplicate}, repo, map[string]bool{})
		if len(ents) != 1 {
			t.Fatalf("expected the pair to fold to one row, got %d", len(ents))
		}
		return ents[0]
	}

	t.Run("qualified_name", func(t *testing.T) {
		surv := fold(t,
			types.EntityRecord{QualifiedName: "com.example.Over.go"},
			types.EntityRecord{QualifiedName: "wrong.Over.go"})
		if surv.QualifiedName != "com.example.Over.go" {
			t.Fatalf("QualifiedName = %q, want the SURVIVOR's %q. Gap-fill must never override: "+
				"QualifiedName is the byQualifiedName join key, and overwriting it with a "+
				"duplicate's is the cross-repo-join corruption #4402/#4406 exists to prevent.",
				surv.QualifiedName, "com.example.Over.go")
		}
	})

	t.Run("signature", func(t *testing.T) {
		surv := fold(t,
			types.EntityRecord{Signature: "void go(int)"},
			types.EntityRecord{Signature: "void go(String)"})
		if surv.Signature != "void go(int)" {
			t.Fatalf("Signature = %q, want the SURVIVOR's %q — the other overload's signature "+
				"describes a different declaration, not a better version of this one",
				surv.Signature, "void go(int)")
		}
	})

	t.Run("language", func(t *testing.T) {
		surv := fold(t,
			types.EntityRecord{Language: "java"},
			types.EntityRecord{Language: "kotlin"})
		if surv.Language != "java" {
			t.Fatalf("Language = %q, want the SURVIVOR's %q", surv.Language, "java")
		}
	})

	t.Run("line_span", func(t *testing.T) {
		surv := fold(t,
			types.EntityRecord{StartLine: 10, EndLine: 12},
			types.EntityRecord{StartLine: 14, EndLine: 16})
		if surv.StartLine != 10 || surv.EndLine != 12 {
			t.Fatalf("line span = %d-%d, want the SURVIVOR's 10-12. Only a ZERO span may be filled: "+
				"taking the duplicate's, or unioning the two, is the invented third span "+
				"custom_dispatch.go:507 warns about.", surv.StartLine, surv.EndLine)
		}
	})

	t.Run("properties", func(t *testing.T) {
		// Two keys, one shared and one duplicate-only, so this case pins BOTH
		// directions of the property merge in one place: the existence check
		// must block the shared key and must not block the missing one.
		surv := fold(t,
			types.EntityRecord{Properties: map[string]string{"module": "core", "shared": "survivor"}},
			types.EntityRecord{Properties: map[string]string{"shared": "duplicate", "extra": "filled"}})
		if got := surv.PropGet("shared"); got != "survivor" {
			t.Errorf("Properties[shared] = %q, want the SURVIVOR's %q — dropping the PropLookup "+
				"existence check makes every duplicate silently rewrite the survivor's properties, "+
				"including the \"module\" key the whole module layer is built from", got, "survivor")
		}
		if got := surv.PropGet("module"); got != "core" {
			t.Errorf("Properties[module] = %q, want %q", got, "core")
		}
		if got := surv.PropGet("extra"); got != "filled" {
			t.Errorf("Properties[extra] = %q, want %q gap-filled from the duplicate — the existence "+
				"check must block only keys the survivor ALREADY has", got, "filled")
		}
	})
}

// TestConvertExtractedRecords_DistinctIDsAllSurvive is the over-collapse guard.
// It fails if the fold is keyed on anything COARSER than the full EntityID
// tuple — e.g. on (Name) alone, which would merge same-name methods living on
// different receivers or in different files.
func TestConvertExtractedRecords_DistinctIDsAllSurvive(t *testing.T) {
	const repo = "r"

	recs := []types.EntityRecord{
		dupRec("SCOPE.Operation", "go", "Over.java", 10, 12),   // baseline
		dupRec("SCOPE.Operation", "go", "Other.java", 10, 12),  // differs in SourceFile
		dupRec("SCOPE.Class", "go", "Over.java", 10, 12),       // differs in Kind
		dupRec("SCOPE.Operation", "stop", "Over.java", 10, 12), // differs in Name
	}
	ents, _ := convertExtractedRecords(recs, repo, map[string]bool{})

	if len(ents) != 4 {
		t.Fatalf("four records with four distinct EntityIDs produced %d rows, want 4 — the fold key "+
			"must be the full (repo, kind, name, source_file) tuple, never a prefix of it", len(ents))
	}
	seen := map[string]bool{}
	for _, e := range ents {
		if seen[e.ID] {
			t.Fatalf("duplicate ID %q among supposedly distinct records", e.ID)
		}
		seen[e.ID] = true
	}
}
