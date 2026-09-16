package kotlin

// #6912 kotlin arm — in-package grading for the target index and the anchor
// scan.
//
// These conjuncts cannot all be reached from Kotlin SOURCE: the extractor emits
// no SCOPE.Model and no SCOPE.Class for a .kt file, and a cross-file record set
// never exists inside one Extract call. Testing them only through source would
// leave each either ungraded or graded vacuously by an absence some OTHER rule
// already caused — the mutually-masking-guards failure. So the pass is driven
// directly with synthetic records, ONE CONJUNCT PER TEST, each carrying a
// positive control in the same record set so a mutant that broke the index
// entirely fails every one of them rather than passing by emitting nothing.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func TestKotlinFieldTypeRefs_Unit_TargetsAreScopedToTheRequestedFile(t *testing.T) {
	// Conjunct A, driven directly: a Component in another file must never
	// become a target even when a field in THIS file names it.
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "Other.kt", Language: "kotlin"},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
			Metadata: map[string]interface{}{
				"field_type_refs":       []string{"Order"},
				"field_type_refs_owner": "Holder",
			}},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	if n := len(recs[1].Relationships); n != 0 {
		t.Fatalf("emitted %d edges to a record in another file, want 0", n)
	}
}

func TestKotlinFieldTypeRefs_Unit_AnotherFilesCollisionDoesNotShadowThisFile(t *testing.T) {
	// Conjunct B (pass 1, the ambiguity scan). The resolver's index is keyed
	// by (file, name), so a collision in ANOTHER file must not suppress an
	// edge here: the opposite failure direction from conjunct A, and grading
	// one is what makes the other look covered.
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Order", Kind: "SCOPE.Model", Subtype: "model", SourceFile: "Other.kt", Language: "kotlin"},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
			Metadata: map[string]interface{}{
				"field_type_refs":       []string{"Order"},
				"field_type_refs_owner": "Holder",
			}},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	if n := len(recs[2].Relationships); n != 1 {
		t.Fatalf("a collision in ANOTHER file suppressed this file's edge: got %d edges, want 1", n)
	}
}

func TestKotlinFieldTypeRefs_Unit_OnlySchemaFieldsAreAnchors(t *testing.T) {
	// Conjunct C (the anchor scan). A non-field record carrying the stash must
	// not grow an edge.
	stash := func() map[string]interface{} {
		return map[string]interface{}{
			"field_type_refs":       []string{"Order"},
			"field_type_refs_owner": "Holder",
		}
	}
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Op", Kind: "SCOPE.Operation", Subtype: "function", SourceFile: "H.kt", Language: "kotlin", Metadata: stash()},
		{Name: "Alias", Kind: "SCOPE.Schema", Subtype: "type_alias", SourceFile: "H.kt", Language: "kotlin", Metadata: stash()},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin", Metadata: stash()},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	for _, i := range []int{1, 2} {
		if n := len(recs[i].Relationships); n != 0 {
			t.Fatalf("%s/%s grew %d edges; only SCOPE.Schema/field is an anchor",
				recs[i].Kind, recs[i].Subtype, n)
		}
	}
	if n := len(recs[3].Relationships); n != 1 {
		t.Fatalf("the field anchor grew %d edges, want 1 — the refusals above "+
			"would otherwise pass vacuously", n)
	}
}

func TestKotlinFieldTypeRefs_Unit_TwoDistinctFamilyKindsAreRefused(t *testing.T) {
	// The collision the component-tier rule exists for: two DISTINCT kinds
	// inside the address family carrying one name.
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Order", Kind: "SCOPE.Model", Subtype: "model", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
			Metadata: map[string]interface{}{
				"field_type_refs":       []string{"Order"},
				"field_type_refs_owner": "Holder",
			}},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	if n := len(recs[2].Relationships); n != 0 {
		t.Fatalf("emitted %d edges into a two-family-kind collision, want 0", n)
	}
}

func TestKotlinFieldTypeRefs_Unit_ScopeClassParticipatesViaTheTrimAlias(t *testing.T) {
	// BuildIndex writes each entity under its raw Kind AND its SCOPE-trimmed
	// alias, and "Class" IS in componentKindFamily while "SCOPE.Class" is not.
	// So a SCOPE.Class sibling DOES make a component ref ambiguous.
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Order", Kind: "SCOPE.Class", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
			Metadata: map[string]interface{}{
				"field_type_refs":       []string{"Order"},
				"field_type_refs_owner": "Holder",
			}},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	if n := len(recs[2].Relationships); n != 0 {
		t.Fatalf("SCOPE.Class did not participate via the trim alias: %d edges, want 0", n)
	}
}

func TestKotlinFieldTypeRefs_Unit_EveryAddressFamilyEntryMakesARefAmbiguous(t *testing.T) {
	// kotlinComponentAddressFamily's own comment calls its eight entries
	// "exactly" the closure of componentKindFamily under the index's trim
	// alias. Three of them — the BARE `Component` / `View` / `Model`
	// spellings — were ungraded (review, MR-5): no Kotlin producer emits a
	// bare-kinded record, so no source fixture can reach them, and dropping
	// all three left the suite green.
	//
	// They are graded HERE, through the call site rather than by reading the
	// map, because that is what the map is FOR: a Kind in the set must be able
	// to blank a same-named Component target. A Kind the resolver weighs but
	// this map omits would let the pass emit a stub that dangles, which is the
	// invariant the comment states.
	//
	// The row list is deliberately the whole set MINUS the target's own kind,
	// not just the three ungraded ones: grading only those would leave
	// "exactly this set" resting on a partial enumeration again.
	//
	// `SCOPE.Component` is the one entry that cannot appear here, and its
	// absence is a consequence of the rule rather than a gap: a same-named
	// SCOPE.Component record is the target's OWN kind, so it is the SAME graph
	// node (EntityID excludes Subtype) and must NOT blank anything. That
	// direction is graded by Unit_DuplicateComponentRecordsAreOneNode below,
	// which asserts the edge SURVIVES.
	family := []string{
		"Component", "Class", "View", "Model",
		"SCOPE.Class", "SCOPE.View", "SCOPE.Model",
	}
	for _, kind := range family {
		t.Run(kind, func(t *testing.T) {
			recs := []types.EntityRecord{
				{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
				{Name: "Order", Kind: kind, Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
				{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
					Metadata: map[string]interface{}{
						"field_type_refs":       []string{"Order"},
						"field_type_refs_owner": "Holder",
					}},
			}
			attachKotlinFieldTypeRefs(recs, "H.kt")
			if n := len(recs[2].Relationships); n != 0 {
				t.Fatalf("rival kind %q did not blank the Component target: "+
					"%d edges, want 0 — it is in kotlinComponentAddressFamily, "+
					"so the resolver weighs it and the edge would dangle", kind, n)
			}
		})
	}
	// The positive control for the whole table: a rival kind OUTSIDE the
	// family must NOT blank the target. Without it every row above would pass
	// on a pass that emitted nothing at all.
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Order", Kind: "SCOPE.Enum", Subtype: "enum", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
			Metadata: map[string]interface{}{
				"field_type_refs":       []string{"Order"},
				"field_type_refs_owner": "Holder",
			}},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	if n := len(recs[2].Relationships); n != 1 {
		t.Fatalf("control: a NON-family rival blanked the target: %d edges, want 1", n)
	}
}

func TestKotlinFieldTypeRefs_Unit_DuplicateComponentRecordsAreOneNode(t *testing.T) {
	// graph.EntityID hashes (repo, Kind, Name, SourceFile) with Subtype
	// EXCLUDED, so two same-file records sharing a Kind are ONE graph node and
	// ONE ToID. Counting RECORDS instead of KINDS deletes a legitimate edge —
	// #7038, filed against arm C.
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "object", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
			Metadata: map[string]interface{}{
				"field_type_refs":       []string{"Order"},
				"field_type_refs_owner": "Holder",
			}},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	if n := len(recs[2].Relationships); n != 1 {
		t.Fatalf("two same-Kind records are one node: got %d edges, want 1", n)
	}
}

func TestKotlinFieldTypeRefs_Unit_AnExistingOutboundEdgeIsPreserved(t *testing.T) {
	// The pass APPENDS to Relationships rather than assigning. No Kotlin
	// producer gives a field record an outbound edge today, so a mutant that
	// assigns is invisible from source — the distinguishing input has to be
	// built. It costs four lines, and "too expensive to kill" is a claim, not
	// evidence.
	recs := []types.EntityRecord{
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "H.kt", Language: "kotlin"},
		{Name: "Holder.o", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "H.kt", Language: "kotlin",
			Relationships: []types.RelationshipRecord{{ToID: "pre-existing", Kind: "READS_FIELD"}},
			Metadata: map[string]interface{}{
				"field_type_refs":       []string{"Order"},
				"field_type_refs_owner": "Holder",
			}},
	}
	attachKotlinFieldTypeRefs(recs, "H.kt")
	if len(recs[1].Relationships) != 2 {
		t.Fatalf("got %d edges, want 2 (the pre-existing one plus the new one)",
			len(recs[1].Relationships))
	}
	if recs[1].Relationships[0].ToID != "pre-existing" {
		t.Fatalf("the pre-existing edge was clobbered: %+v", recs[1].Relationships)
	}
}
