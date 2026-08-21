package main

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/quality"
)

// #6488 arm A, end to end: proto-mini's `Role.ROLE_ADMIN` row now states
// `subtype: "enum_value"`, and this test grades that row against the graph the
// production indexer actually produces.
//
// The unit tests in internal/quality/subtype_assertion_6488_test.go pin the
// grader's behaviour on a hand-built document. They cannot see whether the
// proto extractor still stamps the subtype the fixture names — that is the
// property #6488's killing mutant moves (buildEnumValue's Subtype
// "enum_value" → "field"), and it is only observable through a real index.
//
// Non-vacuity is asserted explicitly, because the trap here is exactly the one
// #6481 records from the F# fix: an assertion can go green by the scenario
// evaporating. Two checks below make that impossible — the enum value and a
// message field must share (Kind, SourceFile) and differ ONLY in Subtype (so
// no other axis could be doing the work), and the same row with the WRONG
// subtype must miss against the same graph.
func TestProtoEnumValueSubtypeIsGraded_6488(t *testing.T) {
	goldenDir, err := filepath.Abs(filepath.Join("..", "..", "internal", "quality", "golden"))
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}
	fixtureDir := filepath.Join(goldenDir, "proto-mini")
	fix, err := quality.LoadFixture(fixtureDir)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	if err := Index(quality.SourceDir(fixtureDir), graphPath, fix.Name,
		nil /*skip*/, false /*pretty*/, false, /*jsonStats*/
		qualityIndexOptions()...); err != nil {
		t.Fatalf("index fixture: %v", err)
	}
	doc, err := loadDocument(graphPath)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	// --- 1. the fixture states the assertion ------------------------------
	var row *quality.ExpectedEntity
	for i := range fix.ExpectedEntities {
		if fix.ExpectedEntities[i].Name == "Role.ROLE_ADMIN" {
			row = &fix.ExpectedEntities[i]
		}
	}
	if row == nil {
		t.Fatal("proto-mini has no Role.ROLE_ADMIN entity row")
	}
	if row.Subtype != "enum_value" {
		t.Fatalf("Role.ROLE_ADMIN row subtype=%q want %q — this row is the "+
			"corpus's only subtype assertion; dropping it re-opens #6488 arm A "+
			"with no other row to notice", row.Subtype, "enum_value")
	}

	// --- 2. NON-VACUITY: subtype is the only discriminating axis ----------
	//
	// If proto ever emitted enum values under a distinct Kind or into a
	// distinct file, the row would pass on (kind, name, file) alone and this
	// arm would be graded by an assertion that is no longer load-bearing.
	var enumVal, field *struct{ kind, file, subtype string }
	for _, e := range doc.Entities {
		switch e.Name {
		case "Role.ROLE_ADMIN":
			enumVal = &struct{ kind, file, subtype string }{e.Kind, e.SourceFile, e.Subtype}
		case "User.email":
			field = &struct{ kind, file, subtype string }{e.Kind, e.SourceFile, e.Subtype}
		}
	}
	if enumVal == nil || field == nil {
		t.Fatalf("expected both Role.ROLE_ADMIN and User.email in the graph; got %v / %v", enumVal, field)
	}
	if enumVal.kind != field.kind || enumVal.file != field.file {
		t.Fatalf("the subtype assertion is no longer load-bearing: the enum "+
			"value (%s in %s) and the message field (%s in %s) no longer share "+
			"kind+file, so (kind, name, source_file) already tells them apart",
			enumVal.kind, enumVal.file, field.kind, field.file)
	}
	// Errorf, not Fatalf: when the extractor stops distinguishing them this
	// test must ALSO report the recall drop below, so the failure reads as
	// "the graph lost a distinction and the fixture noticed" rather than only
	// as a broken precondition.
	if enumVal.subtype == field.subtype {
		t.Errorf("enum value and message field both carry subtype %q — nothing "+
			"to discriminate", enumVal.subtype)
	}

	// --- 3. the row grades, and the fixture still scores 100% -------------
	//
	// Absolute counts, not "the ratchet is green": scripts/quality/ratchet.py
	// can re-record its own floor, so a number recorded there does not survive
	// a revert. 18/18 and 16/16 are the figures #6488 measured while the
	// enum-value subtype flip was live and undetected.
	rep := quality.Evaluate(fix, doc)
	for _, r := range rep.EntityResults {
		if r.Expected.Name != "Role.ROLE_ADMIN" {
			continue
		}
		if !r.Found {
			t.Errorf("Role.ROLE_ADMIN did not grade: SubtypeMismatch=%v "+
				"GotSubtype=%q. The proto extractor must stamp Subtype "+
				"%q on enum values (buildEnumValue).",
				r.SubtypeMismatch, r.GotSubtype, "enum_value")
		}
	}
	// 19 since #6518 added the per-file SCOPE.Component carrier, and 17/17
	// relationships since the file -> message CONTAINS row it made gradeable
	// (see proto-mini/NOTICE.md, which asked for exactly that row).
	if rep.EntityExpected != 19 || rep.EntityFound != 19 {
		var missing []string
		for _, r := range rep.EntityResults {
			if r.Expected.MustExist && !r.Found {
				missing = append(missing, r.Expected.Kind+" "+r.Expected.Name)
			}
		}
		t.Errorf("proto-mini entities %d/%d want 19/19; missing: %v",
			rep.EntityFound, rep.EntityExpected, missing)
	}
	if rep.RelExpected != 17 || rep.RelFound != 17 {
		t.Errorf("proto-mini relationships %d/%d want 17/17", rep.RelFound, rep.RelExpected)
	}
	if n := len(rep.ForbiddenHits); n != 0 {
		t.Errorf("proto-mini forbidden hits = %d, want 0", n)
	}

	// --- 4. NON-VACUITY: the same row with the WRONG subtype must miss ----
	//
	// This is the mutant of the FIXTURE rather than of the extractor, and it
	// is what proves the row is being graded at all rather than passing on the
	// other three axes.
	wrong := *fix
	wrong.ExpectedEntities = append([]quality.ExpectedEntity(nil), fix.ExpectedEntities...)
	for i := range wrong.ExpectedEntities {
		if wrong.ExpectedEntities[i].Name == "Role.ROLE_ADMIN" {
			wrong.ExpectedEntities[i].Subtype = "field"
		}
	}
	wrongRep := quality.Evaluate(&wrong, doc)
	if wrongRep.EntityFound != rep.EntityFound-1 {
		t.Errorf("a Role.ROLE_ADMIN row demanding subtype %q graded %d/%d, "+
			"i.e. it still matched: the subtype is not being compared against "+
			"the real graph", "field", wrongRep.EntityFound, wrongRep.EntityExpected)
	}
	for _, r := range wrongRep.EntityResults {
		if r.Expected.Name != "Role.ROLE_ADMIN" {
			continue
		}
		if !r.SubtypeMismatch || r.GotSubtype != "enum_value" {
			t.Errorf("mismatched row diagnosis: SubtypeMismatch=%v GotSubtype=%q "+
				"want true/%q — the entity WAS extracted and the report must not "+
				"read as an absence", r.SubtypeMismatch, r.GotSubtype, "enum_value")
		}
	}
}
