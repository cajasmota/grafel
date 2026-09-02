package main

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/quality"
)

// #6488 arm B, end to end: proto-mini now carries `forbidden_entities`, and
// this test grades those rows against the graph the production indexer
// actually produces.
//
// The unit tests in internal/quality/forbidden_entities_6488_test.go pin the
// grader on a hand-built document. They cannot see whether the two rows still
// describe anything real — a forbidden row is silent by design, so a row that
// has quietly become impossible looks exactly like a row that is holding, and
// that is the vacuity this arm exists not to reproduce.
//
// So the shape is: the rows are silent on the real graph (no over-fire), AND
// each is ONE production change away from firing, demonstrated by re-grading
// the same real graph with the row perturbed along the single axis its
// motivating defect moves.
func TestProtoMiniForbiddenEntitiesAreGradedAndFalsifiable_6488(t *testing.T) {
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

	// --- 1. the fixture states the assertions -----------------------------
	if len(fix.ForbiddenEntities) < 2 {
		t.Fatalf("proto-mini declares %d forbidden_entities rows, want at least 2 "+
			"(the subtype-bearing and subtype-free forms)", len(fix.ForbiddenEntities))
	}

	// --- 2. they do not over-fire on the real graph -----------------------
	rep := quality.Evaluate(fix, doc)
	if n := len(rep.ForbiddenEntityHits); n != 0 {
		t.Fatalf("forbidden entity hits=%d on a clean tree, want 0: %+v",
			n, rep.ForbiddenEntityHits)
	}
	// Guard the guard: a report that is green because the grader never ran
	// would satisfy the line above. proto-mini's must-have rows are the
	// positive control that the same Evaluate call is doing real work.
	if rep.EntityFound != rep.EntityExpected || rep.EntityExpected == 0 {
		t.Fatalf("DEGENERATE: entity recall %d/%d — the graph under test is not the "+
			"clean one this assertion assumes", rep.EntityFound, rep.EntityExpected)
	}

	find := func(name, kind, file string) *graph.Entity {
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if e.Name == name && e.Kind == kind && e.SourceFile == file {
				return e
			}
		}
		return nil
	}

	// --- 3. FE1 is one subtype stamp away from firing ---------------------
	//
	// The row forbids Role/SCOPE.Schema/user.proto wearing subtype "message".
	// The enum IS extracted at that exact (kind, name, file) and differs only
	// in Subtype, so the row's silence is a live fact about the extractor's
	// stamp and not an accident of address.
	role := find("Role", "SCOPE.Schema", "user.proto")
	if role == nil {
		t.Fatal("no Role/SCOPE.Schema entity in user.proto — FE1 forbids a shape " +
			"nothing in this fixture could ever occupy")
	}
	if role.Subtype != "enum" {
		t.Fatalf("Role subtype=%q want %q", role.Subtype, "enum")
	}
	// Non-vacuity, positively: state the row the way a defective extractor
	// would make true, and it fires against THIS graph.
	probe := &quality.Fixture{
		Name: fix.Name,
		ForbiddenEntities: []quality.ExpectedEntity{
			{Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto", Subtype: "enum"},
		},
	}
	if n := len(quality.Evaluate(probe, doc).ForbiddenEntityHits); n != 1 {
		t.Fatalf("the FE1 row with the subtype the extractor actually stamps hit %d "+
			"times, want 1 — FE1 is unfalsifiable as written", n)
	}

	// --- 4. FE2 is one regex widening away from firing --------------------
	//
	// The row forbids proto_message:Role. The near-duplicate family it bounds
	// is real: internal/custom/cpp/protobuf.go mints proto_message:<msg> for
	// every message and proto_enum:<enum> for every enum in the SAME file the
	// tree-sitter extractor already covered. Both halves must be present, or
	// the row bounds a family that does not exist.
	if e := find("proto_message:User", "SCOPE.Schema", "user.proto"); e == nil {
		t.Error("no proto_message:User — the message family FE2 bounds is not being " +
			"emitted at all, so the row cannot be describing a live risk")
	}
	if e := find("proto_enum:Role", "SCOPE.Schema", "user.proto"); e == nil {
		t.Error("no proto_enum:Role — the enum Role does not enter the detector's " +
			"families, so a widened message regex would have nothing to swallow")
	}
	probe = &quality.Fixture{
		Name: fix.Name,
		ForbiddenEntities: []quality.ExpectedEntity{
			{Name: "proto_enum:Role", Kind: "SCOPE.Schema", SourceFile: "user.proto"},
		},
	}
	if n := len(quality.Evaluate(probe, doc).ForbiddenEntityHits); n != 1 {
		t.Fatalf("the FE2 row pointed at the family member that DOES exist hit %d "+
			"times, want 1 — FE2's address space is wrong and the row could never "+
			"fire however the detector broke", n)
	}
}
