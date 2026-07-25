package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #4406 — the production dedup-by-ID path in buildDocument. When two
// EntityRecords collapse to the same graph.EntityID (same kind/name/source-file),
// the first wins and later duplicates are dropped. Before this fix the dropped
// duplicate's QualifiedName was lost (breaking byQualifiedName resolution +
// cross-repo joins) even when the survivor's was empty. The fix mirrors #4405's
// supersedeBase gap-fill: the survivor inherits the duplicate's QualifiedName
// (and other base-only fields) where it left them empty, and every edge of the
// dropped duplicate is unioned onto the survivor with no dangling reference.

func dedup4406Indexer() *Indexer {
	return &Indexer{repoTag: "test_repo"}
}

// findEntity returns the (single) entity with the given graph ID, failing if
// it is absent or duplicated.
func findEntity(t *testing.T, ents []graph.Entity, id string) graph.Entity {
	t.Helper()
	var found *graph.Entity
	count := 0
	for k := range ents {
		if ents[k].ID == id {
			found = &ents[k]
			count++
		}
	}
	if count == 0 {
		t.Fatalf("entity %s not found in document", id)
	}
	if count > 1 {
		t.Fatalf("entity %s present %d times — dedup did not collapse it", id, count)
	}
	return *found
}

// TestDedup4406_QNameAndEdgesSurviveDedup is the core production-shape case:
// two records dedupe to one survivor. The base/dropped record carries a
// QualifiedName plus incoming + outgoing edges. After buildDocument the survivor
// must keep a non-empty QualifiedName and ALL edges (deduped) must be present,
// with no edge dangling to a node that is not in the document.
func TestDedup4406_QNameAndEdgesSurviveDedup(t *testing.T) {
	const (
		srcFile = "core/models/contract.py"
		name    = "Contract"
	)

	id := graph.EntityID("test_repo", "Class", name, srcFile)
	fieldID := graph.EntityID("test_repo", "Field", "status", srcFile)
	callerID := graph.EntityID("test_repo", "Function", "save_contract", "core/services.py")

	// Survivor (first-seen) — the framework/custom node: bare Name, NO
	// QualifiedName, and one outgoing edge.
	survivor := types.EntityRecord{
		Kind:       "Class",
		Name:       name,
		SourceFile: srcFile,
		Subtype:    "model",
		StartLine:  10,
		Properties: map[string]string{"framework": "django"},
		Relationships: []types.RelationshipRecord{
			// outgoing CONTAINS class → field (anchored to the survivor)
			{FromID: "", ToID: fieldID, Kind: "CONTAINS"},
		},
	}

	// Duplicate (second-seen) — the base tree-sitter node: carries the
	// module-qualified QualifiedName and BOTH an incoming edge (caller →
	// class) and a distinct outgoing edge that the survivor lacks.
	duplicate := types.EntityRecord{
		Kind:          "Class",
		Name:          name,
		SourceFile:    srcFile,
		QualifiedName: "core.models.contract.Contract",
		StartLine:     10,
		EndLine:       42,
		Language:      "python",
		Tags:          []string{"persisted"},
		Relationships: []types.RelationshipRecord{
			// incoming REFERENCES caller → class (explicit FromID)
			{FromID: callerID, ToID: id, Kind: "REFERENCES"},
			// extra outgoing CONTAINS (different target) anchored to the dup
			{FromID: id, ToID: fieldID, Kind: "DEFINES"},
		},
	}

	// Two referenced endpoints so no edge dangles.
	field := types.EntityRecord{Kind: "Field", Name: "status", SourceFile: srcFile, StartLine: 12}
	caller := types.EntityRecord{Kind: "Function", Name: "save_contract", SourceFile: "core/services.py", StartLine: 3}

	idx := dedup4406Indexer()
	pass1 := []types.EntityRecord{survivor, duplicate, field, caller}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, id)

	// (1) QualifiedName preserved from the dropped duplicate.
	if got.QualifiedName != "core.models.contract.Contract" {
		t.Fatalf("survivor QualifiedName = %q; want it inherited from the dropped duplicate (core.models.contract.Contract)", got.QualifiedName)
	}
	// gap-filled base-only fields.
	if got.EndLine != 42 {
		t.Errorf("survivor EndLine = %d; want 42 inherited from duplicate", got.EndLine)
	}
	if got.Language != "python" {
		t.Errorf("survivor Language = %q; want python inherited from duplicate", got.Language)
	}
	if got.Subtype != "model" {
		t.Errorf("survivor Subtype = %q; want survivor's own 'model' preserved", got.Subtype)
	}
	var hasTag bool
	for _, tg := range got.Tags {
		if tg == "persisted" {
			hasTag = true
		}
	}
	if !hasTag {
		t.Errorf("survivor Tags = %v; want 'persisted' unioned from duplicate", got.Tags)
	}

	// (2) Edge union — all three distinct edges present, none dangling.
	entIDs := make(map[string]bool, len(doc.Entities))
	for _, e := range doc.Entities {
		entIDs[e.ID] = true
	}
	want := map[[3]string]bool{
		{id, fieldID, "CONTAINS"}:    false, // survivor's own (empty FromID anchored to id)
		{callerID, id, "REFERENCES"}: false, // incoming from duplicate
		{id, fieldID, "DEFINES"}:     false, // outgoing from duplicate
	}
	for _, r := range doc.Relationships {
		k := [3]string{r.FromID, r.ToID, r.Kind}
		if _, ok := want[k]; ok {
			want[k] = true
		}
		// no edge may reference an entity absent from the document
		if r.FromID != "" && !entIDs[r.FromID] {
			t.Errorf("edge %v has dangling FromID %s (not in document)", k, r.FromID)
		}
		if r.ToID != "" && !entIDs[r.ToID] {
			t.Errorf("edge %v has dangling ToID %s (not in document)", k, r.ToID)
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("expected edge %v missing after dedup (orphaned by the merge)", k)
		}
	}
}

// TestDedup4406_SurvivorQNameNotOverridden — when the survivor already has a
// QualifiedName, a duplicate's different value must NOT override it (gap-fill
// only, never clobber a value the survivor provided). Mirrors #4405's
// "non-empty custom QName is never overridden".
func TestDedup4406_SurvivorQNameNotOverridden(t *testing.T) {
	const srcFile = "app/user.go"
	id := graph.EntityID("test_repo", "Class", "User", srcFile)

	survivor := types.EntityRecord{
		Kind: "Class", Name: "User", SourceFile: srcFile,
		QualifiedName: "app.User",
	}
	duplicate := types.EntityRecord{
		Kind: "Class", Name: "User", SourceFile: srcFile,
		QualifiedName: "WRONG.User",
	}

	idx := dedup4406Indexer()
	pass1 := []types.EntityRecord{survivor, duplicate}
	doc := idx.buildDocument(&pass1, nil, nil, nil)
	got := findEntity(t, doc.Entities, id)
	if got.QualifiedName != "app.User" {
		t.Fatalf("survivor QualifiedName = %q; want survivor's own app.User preserved (not overridden by duplicate)", got.QualifiedName)
	}
}

// TestDedup4406_NonDuplicateUnchanged — regression: a non-duplicate entity's
// QualifiedName and edges pass through buildDocument unchanged.
func TestDedup4406_NonDuplicateUnchanged(t *testing.T) {
	const srcFile = "lib/order.rb"
	id := graph.EntityID("test_repo", "Class", "Order", srcFile)
	otherID := graph.EntityID("test_repo", "Class", "Line", srcFile)

	ent := types.EntityRecord{
		Kind: "Class", Name: "Order", SourceFile: srcFile,
		QualifiedName: "Lib::Order",
		StartLine:     1, EndLine: 9, Language: "ruby",
		Relationships: []types.RelationshipRecord{
			{FromID: "", ToID: otherID, Kind: "CONTAINS"},
		},
	}
	other := types.EntityRecord{Kind: "Class", Name: "Line", SourceFile: srcFile, StartLine: 20}

	idx := dedup4406Indexer()
	pass1 := []types.EntityRecord{ent, other}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, id)
	if got.QualifiedName != "Lib::Order" {
		t.Errorf("QualifiedName = %q; want unchanged Lib::Order", got.QualifiedName)
	}
	if got.EndLine != 9 || got.Language != "ruby" {
		t.Errorf("entity fields mutated: EndLine=%d Language=%q; want 9/ruby", got.EndLine, got.Language)
	}
	var found bool
	for _, r := range doc.Relationships {
		if r.FromID == id && r.ToID == otherID && r.Kind == "CONTAINS" {
			found = true
		}
	}
	if !found {
		t.Errorf("CONTAINS edge Order → Line missing for non-duplicate entity")
	}
}
