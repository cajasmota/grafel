package java

// #6912 arm E — in-package grading for javaInFileTypeTargets.
//
// These conjuncts cannot be reached from Java SOURCE. The allow-list refuses
// four record shapes and the ambiguity rule refuses one, and for three of them
// no .java file makes the extractor emit a competitor with the right Name: a
// file entity is always named after the file path, a Component with an empty
// Subtype is emitted by no producer today, and Java emits no SCOPE.Model at all.
//
// Testing them only through source would leave each one either UNGRADED or
// graded vacuously by an absence that some OTHER rule already caused — the
// mutually-masking-guards failure. So the target index is driven directly with
// synthetic records, one conjunct per test, each carrying a POSITIVE CONTROL in
// the same record set (an admitted Component of a different name) so a mutant
// that broke the index entirely fails every one of them rather than passing by
// returning nothing.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const javaFTUnitFile = "Model.java"

// javaFTComp is an admitted target: a SCOPE.Component the extractor emits for a
// real type declaration.
func javaFTComp(name, subtype string) types.EntityRecord {
	return types.EntityRecord{
		Name: name, Kind: "SCOPE.Component", Subtype: subtype,
		SourceFile: javaFTUnitFile, Language: "java",
	}
}

// javaFTAssertTargets asserts the EXACT admitted name set, so a rule that
// admits too much fails as loudly as one that admits too little.
func javaFTAssertTargets(t *testing.T, recs []types.EntityRecord, want ...string) {
	t.Helper()
	got := javaInFileTypeTargets(recs, javaFTUnitFile)
	if len(got) != len(want) {
		t.Fatalf("admitted %d targets %v, want %d %v", len(got), javaFTNames(got), len(want), want)
	}
	for _, w := range want {
		tgt, ok := got[w]
		if !ok {
			t.Fatalf("target %q missing; admitted %v", w, javaFTNames(got))
		}
		if exp := "scope:component:class:java:" + javaFTUnitFile + ":" + w; tgt.toID != exp {
			t.Errorf("toID for %q = %q, want %q", w, tgt.toID, exp)
		}
		if tgt.name != w {
			t.Errorf("name for %q = %q", w, tgt.name)
		}
	}
}

func javaFTNames(m map[string]javaFieldTypeTarget) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A SCOPE.Schema/field record sharing a type's name is not a target. Reachable
// in principle — a field emitted at module scope keeps a BARE name — and refused
// on the Kind check rather than on name shape, unlike Go's equivalent.
func TestJavaFieldTypeRefs_Unit_FieldIsNeverATarget(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		{Name: "Customer", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: javaFTUnitFile},
	}, "Order")
}

// The #577 file entity is a SCOPE.Component and would sail through a Kind-only
// guard. Its Name is normally a file path, so this constructs the name it could
// never have — which is precisely what makes the SUBTYPE half of the allow-list
// gradeable at all.
func TestJavaFieldTypeRefs_Unit_FileEntityIsNeverATarget(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		javaFTComp("Customer", "file"),
	}, "Order")
}

// A SCOPE.Component with an EMPTY Subtype is not a declared type. No producer
// emits one for Java today (synthComp and the Panache interfaces always set
// one), so this grades the default arm of the subtype switch against the
// producer that has not been written yet — the Java analogue of Go's import
// placeholder.
func TestJavaFieldTypeRefs_Unit_UnsubtypedComponentIsNeverATarget(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		javaFTComp("Customer", ""),
	}, "Order")
}

// The ambiguity rule's REFUSE direction. Two DISTINCT kinds inside the component
// address family denote two graph nodes at one (file, name), so
// lookupLocationKind's uniqueMatchInFamily finds two IDs, returns no match, and
// the ref falls through to ambigLocation and dangles. Declining to emit is
// strictly better than emitting a stub that cannot bind.
func TestJavaFieldTypeRefs_Unit_TwoDistinctFamilyKindsAreRefused(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		javaFTComp("Customer", "class"),
		{Name: "Customer", Kind: "SCOPE.Model", Subtype: "model", SourceFile: javaFTUnitFile},
	}, "Order")
}

// The ambiguity rule's ADMIT direction, and the half that #7038 says arm C got
// wrong. Two RECORDS sharing Kind and Name in one file are ONE graph node —
// graph.EntityID hashes (repo, Kind, Name, SourceFile) with Subtype EXCLUDED —
// so a rule counting records would delete an edge that binds perfectly well.
// The two subtypes differ deliberately: it is the KIND that decides identity.
func TestJavaFieldTypeRefs_Unit_DuplicateComponentRecordsAreOneNode(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		javaFTComp("Customer", "class"),
		javaFTComp("Customer", "record"),
	}, "Order", "Customer")
}

// A SCOPE.Enum value-set sharing an enum's name does NOT make it ambiguous:
// SCOPE.Enum is outside the component address family, so it never enters the
// tier that resolves a scope:component ref. This is the unit-level statement of
// what TestJavaFieldTypeRefs_ResolvesToEntityIDs proves against the real
// resolver, and it is where arm D's wider rule would have refused every Java
// enum target.
func TestJavaFieldTypeRefs_Unit_EnumValueSetDoesNotShadowItsComponent(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		javaFTComp("Status", "enum"),
		{Name: "Status", Kind: "SCOPE.Enum", Subtype: "enum", SourceFile: javaFTUnitFile},
	}, "Order", "Status")
}

// A record in ANOTHER file is not a target however admissible its shape.
func TestJavaFieldTypeRefs_Unit_OtherFileRecordsAreNeverTargets(t *testing.T) {
	other := javaFTComp("Customer", "class")
	other.SourceFile = "Other.java"
	javaFTAssertTargets(t, []types.EntityRecord{javaFTComp("Order", "class"), other}, "Order")
}

// An unnamed record cannot be a target and must not create an empty-string key
// that a candidate could never match but a mutant might.
func TestJavaFieldTypeRefs_Unit_UnnamedRecordIsSkipped(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		javaFTComp("", "class"),
	}, "Order")
}

// The Kind half of the allow-list, graded APART from the Subtype half so the
// two do not mask each other. Java's own producers happen to make this one
// unreachable from source — every same-file record carrying one of the four
// admitted subtypes is either a SCOPE.Component or a SCOPE.Enum value-set that
// SHARES ITS NAME with one (buildJavaEnumValueSet and the `java_const_group`
// arm of buildJavaConstCollections both name the value-set after the enclosing
// type), so admitting it would compute the identical component-space toID and
// change no output.
//
// That is an accident of today's producers, not a property of the rule, and the
// distinguishing input is one line away: a non-Component record with an admitted
// subtype, a BARE name, and NO same-file Component of that name. Addressing it
// through BuildComponentStructuralRef would mint a ToID pointing at a component
// that does not exist — a dangling stub, classified bug-extractor. Constructed
// here rather than left as a reasoned-about equivalence.
func TestJavaFieldTypeRefs_Unit_NonComponentKindWithAnAdmittedSubtypeIsRefused(t *testing.T) {
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		{Name: "Rates", Kind: "SCOPE.Enum", Subtype: "enum", SourceFile: javaFTUnitFile},
		{Name: "Draft", Kind: "SCOPE.Schema", Subtype: "record", SourceFile: javaFTUnitFile},
	}, "Order")
}

// javaFTStashed builds a record carrying the field-type stash, so
// attachJavaFieldTypeRefs's own guard can be graded independently of the two
// emit sites (which only ever stash onto SCOPE.Schema/field records today).
func javaFTStashed(name, kind, subtype string, cands ...string) types.EntityRecord {
	return types.EntityRecord{
		Name: name, Kind: kind, Subtype: subtype, SourceFile: javaFTUnitFile, Language: "java",
		Metadata: map[string]interface{}{
			javaFieldTypeRefsMetaKey:  cands,
			javaFieldTypeRefsOwnerKey: "Host",
		},
	}
}

func javaFTEdgeCount(recs []types.EntityRecord) int {
	n := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == javaFieldTargetRefKind {
				n++
			}
		}
	}
	return n
}

// attachJavaFieldTypeRefs emits ONLY onto a SCOPE.Schema/field record, and the
// two conjuncts of that guard are graded SEPARATELY — a single test carrying a
// record that violates both would leave either conjunct free to be deleted.
// Both are unreachable from Java source today because the only two stash sites
// are on field records; the guard is what keeps that true if a third site is
// added. Each case carries a real field record as a positive control, so a
// mutant that stopped emitting altogether fails here too.
func TestJavaFieldTypeRefs_Unit_OnlyAFieldRecordCarriesTheEdge(t *testing.T) {
	target := javaFTComp("Customer", "class")

	for _, tc := range []struct {
		label         string
		kind, subtype string
	}{
		{"wrong kind, field subtype", "SCOPE.Component", "field"},
		{"schema kind, wrong subtype", "SCOPE.Schema", "schema"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			recs := []types.EntityRecord{
				target,
				javaFTStashed("Host.impostor", tc.kind, tc.subtype, "Customer"),
				javaFTStashed("Host.real", "SCOPE.Schema", "field", "Customer"),
			}
			attachJavaFieldTypeRefs(recs, javaFTUnitFile)
			if got := javaFTEdgeCount(recs); got != 1 {
				t.Fatalf("emitted %d edges, want exactly 1 (the field record only)", got)
			}
			if len(recs[1].Relationships) != 0 {
				t.Errorf("%s/%s record carries %d edges, want 0",
					tc.kind, tc.subtype, len(recs[1].Relationships))
			}
			if len(recs[2].Relationships) != 1 {
				t.Errorf("the SCOPE.Schema/field control carries %d edges, want 1",
					len(recs[2].Relationships))
			}
		})
	}
}

// The stash is DELETED after it is consumed, on every record it appears on —
// including the ones the guard refuses to emit for. A leaked key would ride into
// the graph as entity metadata.
func TestJavaFieldTypeRefs_Unit_StashIsAlwaysCleared(t *testing.T) {
	recs := []types.EntityRecord{
		javaFTComp("Customer", "class"),
		javaFTStashed("Host.impostor", "SCOPE.Operation", "method", "Customer"),
		javaFTStashed("Host.real", "SCOPE.Schema", "field", "Customer"),
	}
	attachJavaFieldTypeRefs(recs, javaFTUnitFile)
	for i := range recs {
		for _, k := range []string{javaFieldTypeRefsMetaKey, javaFieldTypeRefsOwnerKey} {
			if _, ok := recs[i].Metadata[k]; ok {
				t.Errorf("%s still carries metadata key %q", recs[i].Name, k)
			}
		}
	}
}

// The ambiguity count is scoped to THIS FILE on both of its passes, and the two
// scopings are graded separately because they fail differently. Pass 2's
// same-file filter decides which records may be TARGETS
// (TestJavaFieldTypeRefs_Unit_OtherFileRecordsAreNeverTargets); pass 1's decides
// which records may make a name AMBIGUOUS, and dropping it is the permissive
// mutation's opposite — it would REFUSE a perfectly bindable same-file target
// because some unrelated file happens to carry the name under another
// component-family kind.
//
// The resolver's index is keyed by (file, name), so a record in another file
// cannot make this file's name ambiguous; a rule that let it would silently
// delete edges as the graph grew, which is #7038's failure mode reached from the
// other direction.
func TestJavaFieldTypeRefs_Unit_AnotherFilesCollisionDoesNotShadowThisFile(t *testing.T) {
	rival := types.EntityRecord{
		Name: "Customer", Kind: "SCOPE.Model", Subtype: "model", SourceFile: "Other.java",
	}
	javaFTAssertTargets(t, []types.EntityRecord{
		javaFTComp("Order", "class"),
		javaFTComp("Customer", "class"),
		rival,
	}, "Order", "Customer")
}
