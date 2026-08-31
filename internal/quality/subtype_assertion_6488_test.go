package quality

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6488 arm A — `subtype` on ExpectedEntity.
//
// graph.EntityID = sha256(repo, kind, name, sourceFile). Subtype is NOT
// hashed, so changing it moves no id and breaks no edge; and until this arm
// ExpectedEntity carried no subtype field, so no fixture row could see it
// either. That combination made Subtype the one entity property that decides
// consumer behaviour (internal/mcp/denoise.go filters on it, dashboard views
// group by it, resolver paths branch on it) while being invisible to the whole
// measurement chain. #6488 measured the consequence directly: flipping
// proto-mini's enum values from Subtype "enum_value" to "field" left the
// fixture at 18/18 entities, 16/16 relationships, exit 0.
//
// These tests are written against LoadFixture + Evaluate — i.e. against the
// JSON as an author writes it — rather than against ExpectedEntity struct
// literals, deliberately: the whole defect is that a row an author writes
// cannot express this, so the assertion has to start at the JSON.

// loadFixtureJSON writes one expected.json into a temp dir and loads it
// through the production loader, so every test here exercises the same parse
// path a real fixture takes (including LoadFixture's match_by validation).
func loadFixtureJSON(t *testing.T, body string) *Fixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fix, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	return fix
}

// subtypeDoc mirrors the shape that makes proto's enum values unassertable
// without this arm: `Role.ROLE_ADMIN` and `User.email` are the SAME Kind
// (SCOPE.Schema) in the SAME file, and differ ONLY in Subtype. Name, kind and
// source_file — every axis the harness had — cannot tell an enum value from a
// message field.
func subtypeDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "s1", Name: "Role.ROLE_ADMIN", Kind: "SCOPE.Schema", SourceFile: "user.proto", Subtype: "enum_value"},
			{ID: "s2", Name: "User.email", Kind: "SCOPE.Schema", SourceFile: "user.proto", Subtype: "field"},
			{ID: "s3", Name: "Role", Kind: "SCOPE.Schema", SourceFile: "user.proto", Subtype: "enum",
				QualifiedName: "proto.Role"},
		},
	}
}

// A row that STATES a subtype must reject a candidate carrying a different
// one. This is the assertion #6488 exists for: without it the enum-value flip
// is invisible.
func TestSubtypeRowRejectsAMismatchedCandidate_6488(t *testing.T) {
	fix := loadFixtureJSON(t, `{
	  "fixture_name": "subtype-mini",
	  "expected_entities": [
	    { "name": "Role.ROLE_ADMIN", "kind": "SCOPE.Schema", "source_file": "user.proto",
	      "subtype": "field", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true
	}`)
	rep := Evaluate(fix, subtypeDoc())
	if rep.EntityExpected != 1 {
		t.Fatalf("EntityExpected=%d want 1", rep.EntityExpected)
	}
	if rep.EntityFound != 0 {
		t.Fatalf("EntityFound=%d want 0: the extracted Role.ROLE_ADMIN carries "+
			"Subtype %q, the row demands %q, and a row that states a subtype and "+
			"grades green against a different one is decorative",
			rep.EntityFound, "enum_value", "field")
	}
}

// The mirror: the row that states the RIGHT subtype still matches. Without
// this, "reject everything with a subtype" would pass the test above.
func TestSubtypeRowAcceptsAMatchingCandidate_6488(t *testing.T) {
	fix := loadFixtureJSON(t, `{
	  "fixture_name": "subtype-mini",
	  "expected_entities": [
	    { "name": "Role.ROLE_ADMIN", "kind": "SCOPE.Schema", "source_file": "user.proto",
	      "subtype": "enum_value", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true
	}`)
	rep := Evaluate(fix, subtypeDoc())
	if rep.EntityFound != 1 {
		t.Fatalf("EntityFound=%d want 1: the row names the extracted subtype exactly", rep.EntityFound)
	}
	if rep.EntityResults[0].MatchedID != "s1" {
		t.Fatalf("MatchedID=%q want s1", rep.EntityResults[0].MatchedID)
	}
}

// NEGATIVE CONTROL, and the compatibility contract for the other 26 fixtures:
// an ABSENT subtype means "don't care", never "must be blank". All 423 rows in
// the golden corpus omit it, and they must go on matching entities that carry
// a subtype.
func TestBlankSubtypeRowMatchesAnySubtype_6488(t *testing.T) {
	fix := loadFixtureJSON(t, `{
	  "fixture_name": "subtype-mini",
	  "expected_entities": [
	    { "name": "Role.ROLE_ADMIN", "kind": "SCOPE.Schema", "source_file": "user.proto", "must_exist": true },
	    { "name": "User.email", "kind": "SCOPE.Schema", "source_file": "user.proto", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true
	}`)
	rep := Evaluate(fix, subtypeDoc())
	if rep.EntityFound != 2 || rep.EntityExpected != 2 {
		t.Fatalf("EntityFound=%d/%d want 2/2: a row with no subtype must not "+
			"start rejecting entities that have one", rep.EntityFound, rep.EntityExpected)
	}
}

// An entity carrying NO subtype must not satisfy a row that demands one.
//
// This is #6481's exact shape — ~26 extractors emit their per-import
// placeholder with no recognisable Subtype, which is why #6427's precedence
// never reaches them — and it is the case a "compare only when both sides are
// non-empty" grader would wave through. A blank subtype is a value, and the
// row said which value it wants.
func TestSubtypeRowRejectsAnEntityCarryingNoSubtype_6488(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "b1", Name: "Widget", Kind: "SCOPE.Component", SourceFile: "a.ts"},
	}}
	fix := loadFixtureJSON(t, `{
	  "fixture_name": "subtype-mini",
	  "expected_entities": [
	    { "name": "Widget", "kind": "SCOPE.Component", "source_file": "a.ts",
	      "subtype": "import", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true
	}`)
	rep := Evaluate(fix, doc)
	if rep.EntityFound != 0 {
		t.Fatalf("EntityFound=%d want 0: the extracted entity carries NO "+
			"subtype and the row demands %q. Accepting a blank subtype makes "+
			"the field unable to observe #6481, whose whole content is "+
			"placeholders emitted with no recognisable Subtype", rep.EntityFound, "import")
	}
	if r := rep.EntityResults[0]; !r.SubtypeMismatch || r.GotSubtype != "" {
		t.Errorf("SubtypeMismatch=%v GotSubtype=%q want true/\"\" — the entity "+
			"was extracted; what it lacks is the subtype", r.SubtypeMismatch, r.GotSubtype)
	}
}

// A subtype row must discriminate BETWEEN two candidates that share the whole
// (kind, name, file) key — the collision case a fixture cannot otherwise
// express, and the one that makes the field load-bearing rather than merely
// additive.
func TestSubtypeRowPicksTheRightOneOfTwoCollidingCandidates_6488(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "c1", Name: "Thing", Kind: "SCOPE.Schema", SourceFile: "a.proto", Subtype: "field"},
		{ID: "c2", Name: "Thing", Kind: "SCOPE.Schema", SourceFile: "a.proto", Subtype: "enum_value"},
	}}
	fix := loadFixtureJSON(t, `{
	  "fixture_name": "subtype-mini",
	  "expected_entities": [
	    { "name": "Thing", "kind": "SCOPE.Schema", "source_file": "a.proto",
	      "subtype": "enum_value", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true
	}`)
	rep := Evaluate(fix, doc)
	if rep.EntityFound != 1 {
		t.Fatalf("EntityFound=%d want 1", rep.EntityFound)
	}
	if got := rep.EntityResults[0].MatchedID; got != "c2" {
		t.Fatalf("MatchedID=%q want c2: firstExtracted takes the EARLIEST "+
			"candidate, so a subtype row must filter the bucket rather than "+
			"filter after picking", got)
	}
}

// The qualified_name path returns before the (kind, name, file) lookups, so it
// needs its own subtype gate — otherwise `match_by: qualified_name` is a hole
// straight through the assertion.
func TestSubtypeAppliesOnTheQualifiedNamePath_6488(t *testing.T) {
	base := `{
	  "fixture_name": "subtype-mini",
	  "expected_entities": [
	    { "name": "Role", "qualified_name": "proto.Role", "kind": "SCOPE.Schema",
	      "match_by": "qualified_name", "subtype": %q, "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true
	}`
	if rep := Evaluate(loadFixtureJSON(t, strings.Replace(base, "%q", `"enum"`, 1)), subtypeDoc()); rep.EntityFound != 1 {
		t.Fatalf("matching subtype on the qualified_name path: EntityFound=%d want 1", rep.EntityFound)
	}
	if rep := Evaluate(loadFixtureJSON(t, strings.Replace(base, "%q", `"message"`, 1)), subtypeDoc()); rep.EntityFound != 0 {
		t.Fatalf("mismatched subtype on the qualified_name path: EntityFound=%d "+
			"want 0 — match_by:qualified_name must not bypass the subtype gate", rep.EntityFound)
	}
}

// A subtype miss is a DIFFERENT failure from "the entity was never extracted",
// and the report must say which. Blaming the extractor for an entity it did
// extract is the exact fixture-row-blamed-on-the-extractor defect #6476 and
// #6464 each had to remove; this arm must not reintroduce it at the subtype
// axis.
func TestSubtypeMissIsDiagnosedAsAMismatchNotAnAbsence_6488(t *testing.T) {
	fix := loadFixtureJSON(t, `{
	  "fixture_name": "subtype-mini",
	  "expected_entities": [
	    { "name": "Role.ROLE_ADMIN", "kind": "SCOPE.Schema", "source_file": "user.proto",
	      "subtype": "field", "must_exist": true },
	    { "name": "Nowhere", "kind": "SCOPE.Schema", "source_file": "user.proto",
	      "subtype": "field", "must_exist": true }
	  ],
	  "expected_relationships": [],
	  "asserts_no_relationships": true
	}`)
	rep := Evaluate(fix, subtypeDoc())
	mismatch := rep.EntityResults[0]
	if !mismatch.SubtypeMismatch || mismatch.GotSubtype != "enum_value" {
		t.Fatalf("row 0: SubtypeMismatch=%v GotSubtype=%q want true/%q — the "+
			"entity WAS extracted, only its subtype differs",
			mismatch.SubtypeMismatch, mismatch.GotSubtype, "enum_value")
	}
	absent := rep.EntityResults[1]
	if absent.SubtypeMismatch || absent.GotSubtype != "" {
		t.Fatalf("row 1: SubtypeMismatch=%v GotSubtype=%q want false/\"\" — "+
			"nothing with that name was extracted, so there is no subtype to "+
			"report and the honest diagnostic is absence",
			absent.SubtypeMismatch, absent.GotSubtype)
	}

	// And it survives serialisation, so scripts/quality consumers see it.
	raw, err := json.Marshal(rep.ToJSON())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"subtype":"field"`, `"got_subtype":"enum_value"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("json report is missing %s: %s", want, raw)
		}
	}
	var buf strings.Builder
	rep.WriteHuman(&buf)
	if !strings.Contains(buf.String(), "enum_value") {
		t.Errorf("text report does not name the extracted subtype:\n%s", buf.String())
	}
}

// Corpus control: exactly the rows we intend carry a subtype, and every
// fixture still parses. If a later change bulk-adds subtypes, this fails and
// the author has to re-measure rather than inherit this arm's evidence.
func TestOnlyTheIntendedGoldenRowsAssertSubtype_6488(t *testing.T) {
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	fixtures := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(goldenDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "expected.json")); err != nil {
			continue
		}
		fix, err := LoadFixture(dir)
		if err != nil {
			t.Fatalf("%s: LoadFixture: %v", e.Name(), err)
		}
		fixtures++
		for _, ee := range fix.ExpectedEntities {
			if ee.Subtype != "" {
				got[e.Name()] = append(got[e.Name()], ee.Name+":"+ee.Subtype)
			}
		}
	}
	if fixtures != 26 {
		t.Fatalf("parsed %d fixtures, want 26 — every golden expected.json must "+
			"keep parsing across this additive schema change", fixtures)
	}
	want := map[string][]string{
		"proto-mini": {"Role.ROLE_ADMIN:enum_value"},
	}
	if len(got) != len(want) {
		t.Fatalf("fixtures asserting subtype: got %v want %v", got, want)
	}
	for fx, rows := range want {
		if strings.Join(got[fx], ",") != strings.Join(rows, ",") {
			t.Errorf("%s subtype rows: got %v want %v", fx, got[fx], rows)
		}
	}
}
