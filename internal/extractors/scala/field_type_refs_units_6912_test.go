package scala

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs_units_6912_test.go — the grading that Extract cannot reach.
//
// Extract sees ONE file, so a cross-file record can never appear in the slice it
// hands the attach pass, and every SCOPE.* kind a Scala type declaration mints is
// SCOPE.Component, so a second in-family kind can never arise either. Both are
// nonetheless real: the pass takes `[]types.EntityRecord` and a path, and other
// producers (internal/patterns mints SCOPE.Model, which IS in
// componentKindFamily) write into the same slice downstream.
//
// These tests drive the CALL SITE — attachScalaFieldTypeRefs — with hand-built
// slices, not an extracted predicate. Extracting a predicate and unit-testing it
// does NOT pin behaviour; the call site stays free to ignore it (#6533).

// scStashed builds a field record carrying the capture the emit sites write.
func scStashed(name, file string, cands ...string) types.EntityRecord {
	rec := types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: file,
		Language:   "scala",
	}
	stashScalaFieldTypeRefs(&rec, cands, "Svc")
	return rec
}

func scComp(name, subtype, file string) types.EntityRecord {
	return types.EntityRecord{
		Name: name, Kind: "SCOPE.Component", Subtype: subtype,
		SourceFile: file, Language: "scala",
	}
}

// scTypeRefTargets lists "<field> -> <target_type>" after the pass.
func scTypeRefTargets(recs []types.EntityRecord) []string {
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				out = append(out, recs[i].Name+" -> "+r.Properties.Get("target_type"))
			}
		}
	}
	return out
}

func scWantTargets(t *testing.T, recs []types.EntityRecord, want ...string) {
	t.Helper()
	got := scTypeRefTargets(recs)
	if len(got) != len(want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("edges = %v, want %v", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// THE THREE SAME-FILE CONJUNCTS. Every table on this issue said two; php's
// author found three. Scala has three, each failing in a DIFFERENT direction,
// each graded on its own — because grading one is what makes its sibling look
// covered.
//
//	1. scalaInFileTypeTargets pass 1 (the family COUNT) — dropping it lets a
//	   cross-file record wrongly SUPPRESS a correct edge. No symptom (#7056).
//	2. scalaInFileTypeTargets pass 2 (the TARGET scan) — dropping it lets a
//	   cross-file record wrongly BECOME a target. A confident wrong binding.
//	3. attachScalaFieldTypeRefs's field loop (the ATTACH ANCHOR) — dropping it
//	   lets a field declared in ANOTHER file receive an edge into this one.
// ---------------------------------------------------------------------------

// TestScalaFieldTypeRefs_Unit_ForeignRecordNeverSuppressesAnEdge grades
// conjunct 1: a same-named, in-family record in a DIFFERENT file must not make
// this file's declaration look ambiguous.
func TestScalaFieldTypeRefs_Unit_ForeignRecordNeverSuppressesAnEdge(t *testing.T) {
	recs := []types.EntityRecord{
		scComp("Repo", "class", "A.scala"),
		// Same name, IN FAMILY, other file. Only the file check refuses it.
		{Name: "Repo", Kind: "SCOPE.Model", Subtype: "class", SourceFile: "B.scala"},
		scStashed("Svc.r", "A.scala", "Repo"),
	}
	attachScalaFieldTypeRefs(recs, "A.scala")
	scWantTargets(t, recs, "Svc.r -> Repo")
}

// TestScalaFieldTypeRefs_Unit_ForeignRecordIsNeverATarget grades conjunct 2.
func TestScalaFieldTypeRefs_Unit_ForeignRecordIsNeverATarget(t *testing.T) {
	recs := []types.EntityRecord{
		scComp("Repo", "class", "B.scala"), // declared ELSEWHERE
		scStashed("Svc.r", "A.scala", "Repo"),
	}
	attachScalaFieldTypeRefs(recs, "A.scala")
	scWantTargets(t, recs)
	// Non-vacuity: the identical slice with the declaration moved into A.scala
	// DOES emit, so the zero above is the file check and not a broken fixture.
	recs2 := []types.EntityRecord{
		scComp("Repo", "class", "A.scala"),
		scStashed("Svc.r", "A.scala", "Repo"),
	}
	attachScalaFieldTypeRefs(recs2, "A.scala")
	scWantTargets(t, recs2, "Svc.r -> Repo")
}

// TestScalaFieldTypeRefs_Unit_ForeignFieldIsNeverAnAnchor grades conjunct 3 —
// the one no arm before php counted.
func TestScalaFieldTypeRefs_Unit_ForeignFieldIsNeverAnAnchor(t *testing.T) {
	recs := []types.EntityRecord{
		scComp("Repo", "class", "A.scala"),
		scStashed("Other.r", "B.scala", "Repo"), // a field from ANOTHER file
	}
	attachScalaFieldTypeRefs(recs, "A.scala")
	scWantTargets(t, recs)
}

// ---------------------------------------------------------------------------
// The ambiguity guard and the allow-list, each graded where it is reachable.
// ---------------------------------------------------------------------------

// TestScalaFieldTypeRefs_Unit_InFamilyRivalRefusesTheEdge is the emission half
// of the in-family dangle proved in field_type_refs_scope_6912_test.go. It is
// unreachable from Scala source (every Scala type declaration is kinded
// SCOPE.Component), so it is graded here — and the row below holds the rival
// CONSTANT while varying only its Kind's family membership, so the two
// directions cannot mask each other.
func TestScalaFieldTypeRefs_Unit_InFamilyRivalRefusesTheEdge(t *testing.T) {
	for _, tc := range []struct {
		label     string
		rivalKind string
		want      []string
	}{
		{"no rival", "", []string{"Svc.r -> Repo"}},
		{"in-family: SCOPE.Model", "SCOPE.Model", nil},
		{"in-family: SCOPE.View", "SCOPE.View", nil},
		{"in-family via the TRIM ALIAS: Class", "Class", nil},
		{"in-family via the TRIM ALIAS: Component", "Component", nil},
		{"out-of-family: SCOPE.Enum", "SCOPE.Enum", []string{"Svc.r -> Repo"}},
		{"out-of-family: SCOPE.Operation", "SCOPE.Operation", []string{"Svc.r -> Repo"}},
		{"out-of-family: SCOPE.Service", "SCOPE.Service", []string{"Svc.r -> Repo"}},
		{"out-of-family: SCOPE.Schema", "SCOPE.Schema", []string{"Svc.r -> Repo"}},
		{"out-of-family: SCOPE.DataAccess", "SCOPE.DataAccess", []string{"Svc.r -> Repo"}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			recs := []types.EntityRecord{scComp("Repo", "class", "A.scala")}
			if tc.rivalKind != "" {
				recs = append(recs, types.EntityRecord{
					Name: "Repo", Kind: tc.rivalKind, Subtype: "rival",
					SourceFile: "A.scala", Language: "scala",
				})
			}
			recs = append(recs, scStashed("Svc.r", "A.scala", "Repo"))
			attachScalaFieldTypeRefs(recs, "A.scala")
			scWantTargets(t, recs, tc.want...)
		})
	}
}

// TestScalaFieldTypeRefs_Unit_SecondRecordOfTheSameKindIsOneNode is the
// record-vs-kind distinction (#7038) at the unit level: two records sharing a
// Kind are ONE graph node, so they are not ambiguous however many there are.
func TestScalaFieldTypeRefs_Unit_SecondRecordOfTheSameKindIsOneNode(t *testing.T) {
	recs := []types.EntityRecord{
		scComp("Order", "case_class", "A.scala"),
		scComp("Order", "object", "A.scala"),
		scComp("Order", "", "A.scala"), // an import placeholder too
		scStashed("Svc.o", "A.scala", "Order"),
	}
	attachScalaFieldTypeRefs(recs, "A.scala")
	scWantTargets(t, recs, "Svc.o -> Order")
}

// TestScalaFieldTypeRefs_Unit_TargetSubtypeAllowList enumerates the subtype
// space rather than sampling it. Each admitted subtype is a real Scala type
// declaration; each refused one is a carrier that is not.
func TestScalaFieldTypeRefs_Unit_TargetSubtypeAllowList(t *testing.T) {
	for _, tc := range []struct {
		subtype string
		want    bool
	}{
		{"class", true},
		{"case_class", true},
		{"trait", true},
		{"object", false},
		{"file", false},
		{"twirl", false},
		{"", false}, // the import placeholder
		{"unknown_future_subtype", false},
	} {
		t.Run("subtype="+tc.subtype, func(t *testing.T) {
			recs := []types.EntityRecord{
				scComp("Repo", tc.subtype, "A.scala"),
				scStashed("Svc.r", "A.scala", "Repo"),
			}
			attachScalaFieldTypeRefs(recs, "A.scala")
			if tc.want {
				scWantTargets(t, recs, "Svc.r -> Repo")
			} else {
				scWantTargets(t, recs)
			}
		})
	}
}

// TestScalaFieldTypeRefs_Unit_TargetKindMustBeComponent grades the Kind half of
// pass 2 SEPARATELY from the subtype half. The two only ever fire together on
// real Scala records, which is exactly what makes grading them as a unit
// worthless (#7042's send-back): the distinguishing input is a NON-Component
// kind carrying an ADMITTED subtype, so the allow-list cannot be what refuses
// it.
func TestScalaFieldTypeRefs_Unit_TargetKindMustBeComponent(t *testing.T) {
	for _, kind := range []string{
		"SCOPE.Model", "SCOPE.View", "Class", "Component",
		"SCOPE.Enum", "SCOPE.Service", "SCOPE.Schema", "SCOPE.Operation",
	} {
		t.Run(kind, func(t *testing.T) {
			recs := []types.EntityRecord{
				{Name: "Repo", Kind: kind, Subtype: "class", SourceFile: "A.scala"},
				scStashed("Svc.r", "A.scala", "Repo"),
			}
			attachScalaFieldTypeRefs(recs, "A.scala")
			scWantTargets(t, recs)
		})
	}
	// Mirror: the same subtype on a SCOPE.Component IS admitted, so the rows
	// above graded the Kind check and not a broken fixture.
	recs := []types.EntityRecord{
		scComp("Repo", "class", "A.scala"),
		scStashed("Svc.r", "A.scala", "Repo"),
	}
	attachScalaFieldTypeRefs(recs, "A.scala")
	scWantTargets(t, recs, "Svc.r -> Repo")
}

// TestScalaFieldTypeRefs_Unit_OnlySchemaFieldsAreAnchors grades the SOURCE
// endpoint: a record carrying the capture but not kinded as a field must not
// receive an edge. Unreachable through Extract (only the two field builders
// stash), and therefore graded here.
func TestScalaFieldTypeRefs_Unit_OnlySchemaFieldsAreAnchors(t *testing.T) {
	for _, tc := range []struct {
		kind, subtype string
	}{
		{"SCOPE.Component", "field"},
		{"SCOPE.Schema", "column"},
		{"SCOPE.Operation", "field"},
		{"SCOPE.Schema", ""},
	} {
		t.Run(tc.kind+"/"+tc.subtype, func(t *testing.T) {
			rec := types.EntityRecord{
				Name: "Svc.r", Kind: tc.kind, Subtype: tc.subtype, SourceFile: "A.scala",
			}
			stashScalaFieldTypeRefs(&rec, []string{"Repo"}, "Svc")
			recs := []types.EntityRecord{scComp("Repo", "class", "A.scala"), rec}
			attachScalaFieldTypeRefs(recs, "A.scala")
			scWantTargets(t, recs)
		})
	}
}

// TestScalaFieldTypeRefs_Unit_CaptureIsDeletedFromEveryAnchorShape pins that the
// scratch is cleared even on a record that gets NO edge — including the
// non-field shapes above, which return before the emit loop.
func TestScalaFieldTypeRefs_Unit_CaptureIsDeletedFromEveryAnchorShape(t *testing.T) {
	recs := []types.EntityRecord{
		scComp("Repo", "class", "A.scala"),
		scStashed("Svc.hit", "A.scala", "Repo"),
		scStashed("Svc.miss", "A.scala", "Nowhere"),
		scStashed("Other.foreign", "B.scala", "Repo"),
	}
	// Non-field anchor, which bails before the emit loop.
	nonField := types.EntityRecord{Name: "X", Kind: "SCOPE.Operation", SourceFile: "A.scala"}
	stashScalaFieldTypeRefs(&nonField, []string{"Repo"}, "")
	recs = append(recs, nonField)

	attachScalaFieldTypeRefs(recs, "A.scala")
	for i := range recs {
		if recs[i].Metadata != nil {
			t.Fatalf("%s: Metadata = %v, want nil", recs[i].Name, recs[i].Metadata)
		}
	}
	scWantTargets(t, recs, "Svc.hit -> Repo")
}

// TestScalaFieldTypeRefs_Unit_StashIsANoOpWithoutCandidates pins that a field
// naming nothing addressable is left byte-identical — no `metadata: {}` appears
// on records that carried no map before this pass existed.
func TestScalaFieldTypeRefs_Unit_StashIsANoOpWithoutCandidates(t *testing.T) {
	rec := types.EntityRecord{Name: "Svc.x", Kind: "SCOPE.Schema", Subtype: "field"}
	stashScalaFieldTypeRefs(&rec, nil, "Svc")
	if rec.Metadata != nil {
		t.Fatalf("Metadata = %v, want nil", rec.Metadata)
	}
}

// TestScalaFieldTypeRefs_Unit_ExistingRelationshipsSurvive pins the APPEND. A
// Scala field record carries no outbound edge today, so this is unreachable
// through Extract — and that is precisely why assigning instead of appending
// would be invisible until some later pass added one.
func TestScalaFieldTypeRefs_Unit_ExistingRelationshipsSurvive(t *testing.T) {
	f := scStashed("Svc.r", "A.scala", "Repo")
	f.Relationships = append(f.Relationships, types.RelationshipRecord{
		ToID: "pre-existing", Kind: "USES",
	})
	recs := []types.EntityRecord{scComp("Repo", "class", "A.scala"), f}
	attachScalaFieldTypeRefs(recs, "A.scala")
	got := recs[1].Relationships
	if len(got) != 2 || got[0].ToID != "pre-existing" || got[0].Kind != "USES" {
		t.Fatalf("relationships = %v, want the pre-existing edge kept first", got)
	}
}

// TestScalaFieldTypeRefs_Unit_EmptyNameRecordsAreIgnored pins the `Name == ""`
// half of both scan conjuncts: a nameless record must neither be a target nor
// contribute to the ambiguity count.
func TestScalaFieldTypeRefs_Unit_EmptyNameRecordsAreIgnored(t *testing.T) {
	recs := []types.EntityRecord{
		scComp("", "class", "A.scala"),
		scComp("Repo", "class", "A.scala"),
		scStashed("Svc.r", "A.scala", "Repo"),
	}
	attachScalaFieldTypeRefs(recs, "A.scala")
	scWantTargets(t, recs, "Svc.r -> Repo")
	// And a field whose candidate is the empty string binds to nothing.
	recs2 := []types.EntityRecord{
		scComp("", "class", "A.scala"),
		scStashed("Svc.r", "A.scala", ""),
	}
	attachScalaFieldTypeRefs(recs2, "A.scala")
	scWantTargets(t, recs2)
}

// TestScalaFieldTypeRefs_Unit_ComponentAddressFamilyIsTheTrimClosure pins the
// invariant the duplicated family map rests on: membership is
// `K ∈ componentKindFamily || trim(K) ∈ componentKindFamily`. Written as an
// explicit roster so a future widening of componentKindFamily upstream shows up
// as a diff here rather than as a dangling stub in production.
func TestScalaFieldTypeRefs_Unit_ComponentAddressFamilyIsTheTrimClosure(t *testing.T) {
	want := map[string]bool{
		"Component": true, "Class": true, "View": true, "Model": true,
		"SCOPE.Component": true, "SCOPE.Class": true,
		"SCOPE.View": true, "SCOPE.Model": true,
	}
	if len(scalaComponentAddressFamily) != len(want) {
		t.Fatalf("family has %d entries, want %d: %v",
			len(scalaComponentAddressFamily), len(want), scalaComponentAddressFamily)
	}
	for k := range want {
		if !scalaComponentAddressFamily[k] {
			t.Errorf("missing family member %q", k)
		}
	}
	for _, k := range []string{"SCOPE.Enum", "SCOPE.Service", "SCOPE.Schema", "Enum", "Service"} {
		if scalaComponentAddressFamily[k] {
			t.Errorf("%q must NOT be in the component address family", k)
		}
	}
}

// TestScalaFieldTypeRefs_Unit_RefKindLiteral pins the cross-arm vocabulary
// against an INDEPENDENT literal rather than against the package's own
// constant — arm D's CZ-2 established that this is what makes the eleven arms
// agree transitively without a shared constant.
func TestScalaFieldTypeRefs_Unit_RefKindLiteral(t *testing.T) {
	if scalaFieldTargetRefKind != "field_target_type" {
		t.Fatalf("ref_kind = %q, want field_target_type — arms A-J all spell it "+
			"this way and the discriminator is what makes the edge queryable",
			scalaFieldTargetRefKind)
	}
}
