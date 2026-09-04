package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #6490 arm A, second half. The first half made an ABSENT
// `expected_relationships` key a hard error, and deliberately left the explicit
// empty array legal as a "visible marker". That marker turned out to be a
// marker of nothing: the grader still cannot fail a fixture that asserts no
// must-have edge, so nine golden fixtures score `relationships: 0/0 — pass`
// while grading their language's edge extraction with nothing at all.
//
// The owner's decision on the issue makes the rule: a zero-must-have
// relationship assertion is a HARD grader failure unless the fixture opts in
// through an explicit, named, positively-valued field. The opt-in may never be
// expressible as an empty or absent value — absence reading as consent is the
// entire defect.
//
// These tests use temp-dir expected.json files so they do not move when arm B
// writes real rows into the nine.

// TestEmptyExpectedRelationshipsIsHardError_6490 supersedes the first half's
// TestEmptyExpectedRelationshipsIsAccepted_6490: an empty array is a
// declaration that asserts nothing, and asserting nothing is now an error
// unless the fixture says so out loud.
func TestEmptyExpectedRelationshipsIsHardError_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "empty-array-mini",
  "language": "php",
  "description": "explicitly asserts nothing, without opting in",
  "expected_entities": [{"name": "App", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": []`)

	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("LoadFixture accepted an empty expected_relationships array with no " +
			"opt-in — a fixture asserting no must-have edge is a gate that cannot " +
			"fail, and must be rejected")
	}
	if !strings.Contains(err.Error(), "asserts_no_relationships") {
		t.Fatalf("error does not name the opt-in field, so it does not tell the fixture "+
			"author how to state the exemption: %v", err)
	}
}

// TestAllNiceToHaveRelationshipsIsHardError_6490 is the haskell-warp-mini
// shape: rows are present, so a check written against `len(rows) == 0` sees a
// populated fixture and waves it through. Every row is `nice_to_have`, i.e.
// nothing in it can ever fail. The count that matters is must-have rows.
func TestAllNiceToHaveRelationshipsIsHardError_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "all-nice-to-have-mini",
  "language": "haskell",
  "description": "three rows, none of which can fail",
  "expected_entities": [{"name": "App", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": [
    {"from_name": "main", "to_name": "run", "kind": "CALLS", "must_exist": false, "nice_to_have": true},
    {"from_name": "main", "to_name": "app", "kind": "CALLS", "must_exist": false, "nice_to_have": true},
    {"from_name": "app", "to_name": "route", "kind": "CALLS", "must_exist": false, "nice_to_have": true}
  ]`)

	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted three rows that are all nice_to_have — none of " +
			"them can fail, so the fixture asserts nothing about relationships and " +
			"must be rejected the same way an empty array is")
	}
}

// TestForbiddenRelationshipsDoNotSatisfyTheFloor_6490 closes the nearest
// escape hatch: forbidden rows assert that something is ABSENT, which never
// goes red when the extractor stops emitting edges — the regression this arm
// exists to catch. A fixture carrying forbidden rows and no must-have row is
// still a gate that cannot fail on recall.
func TestForbiddenRelationshipsDoNotSatisfyTheFloor_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "forbidden-only-mini",
  "language": "solidity",
  "description": "asserts only that an edge is absent",
  "expected_entities": [{"name": "Token", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": [],
  "forbidden_relationships": [{"from_name": "Token", "to_name": "Token", "kind": "IMPLEMENTS", "must_exist": false}]`)

	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted a fixture whose only relationship assertions are " +
			"forbidden rows — those go red when an edge APPEARS, never when edges " +
			"stop being emitted, so they are not a recall floor")
	}
}

// TestAssertsNoRelationshipsOptInIsAccepted_6490 pins the escape valve. Without
// it the rule would be unimplementable for a fixture whose language genuinely
// emits no edges, and the rule would then be relaxed under pressure rather
// than opted out of visibly.
func TestAssertsNoRelationshipsOptInIsAccepted_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "opted-out-mini",
  "language": "proto",
  "description": "deliberately relationship-free",
  "expected_entities": [{"name": "Msg", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": [],
  "asserts_no_relationships": true`)

	fix, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("LoadFixture rejected a fixture that explicitly opted out via "+
			"asserts_no_relationships: true: %v", err)
	}
	if !fix.AssertsNoRelationships {
		t.Fatal("asserts_no_relationships decoded as false — the opt-in must be readable " +
			"by the grader, not only by the loader's validation")
	}
}

// TestAssertsNoRelationshipsFalseIsNotAnOptIn_6490 is the load-bearing half of
// the opt-in design: the field must only exempt a fixture when it is
// positively true. `false`, and by extension the absent field, must read as
// "no exemption claimed" — a field whose default value grants the exemption
// would rebuild the exact defect (absence reading as consent) under a new name.
func TestAssertsNoRelationshipsFalseIsNotAnOptIn_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "opt-in-declined-mini",
  "language": "csharp",
  "description": "names the field but does not claim the exemption",
  "expected_entities": [{"name": "Ctl", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": [],
  "asserts_no_relationships": false`)

	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture treated asserts_no_relationships: false as an opt-in — " +
			"only an explicit true may exempt a fixture from the must-have floor")
	}
}

// TestOneMustHaveRowSatisfiesTheFloor_6490 is the positive control: the new
// validation must not reject fixtures that do assert something, and one
// must-have row among nice-to-haves is enough.
func TestOneMustHaveRowSatisfiesTheFloor_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "one-must-have-mini",
  "language": "go",
  "description": "one real assertion plus a nice-to-have",
  "expected_entities": [{"name": "Handler", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": [
    {"from_name": "Handler", "to_name": "Store", "kind": "CALLS", "must_exist": false, "nice_to_have": true},
    {"from_name": "Handler", "to_name": "Logger", "kind": "CALLS", "must_exist": true}
  ]`)

	if _, err := LoadFixture(dir); err != nil {
		t.Fatalf("LoadFixture rejected a fixture with a must-have row: %v", err)
	}
}

// TestNoGoldenFixtureClaimsTheOptIn_6490 keeps the escape valve from becoming
// the answer. The opt-in exists so the rule is expressible, not so the nine
// can be waved through: arm B owes them real rows, and stamping
// `asserts_no_relationships: true` onto a fixture would satisfy the loader
// while leaving its language graded by nothing — the re-blessing-at-zero the
// issue names as the thing to prevent.
//
// If a fixture ever earns the exemption, this test is the place that decision
// gets recorded and argued, which is the point.
func TestNoGoldenFixtureClaimsTheOptIn_6490(t *testing.T) {
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var optedOut []string
	inspected := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(goldenDir, e.Name(), "expected.json"))
		if err != nil {
			continue
		}
		inspected++
		if strings.Contains(string(raw), "\"asserts_no_relationships\"") {
			optedOut = append(optedOut, e.Name())
		}
	}
	if inspected != 29 {
		t.Fatalf("inspected %d golden fixtures, want 29 — the corpus size changed, so "+
			"this test's coverage claim needs re-deriving", inspected)
	}
	if len(optedOut) != 0 {
		t.Fatalf("golden fixtures claiming asserts_no_relationships: %v\n"+
			"the opt-in is not a way to close #6490 arm B — a fixture that needs it must "+
			"argue for it here, by name", optedOut)
	}
}
