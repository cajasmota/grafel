package quality

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #6490 arm A — an expected.json that OMITS `expected_relationships` is today
// indistinguishable from one that asserts zero: `json.Unmarshal` leaves the
// slice nil in both cases, so the grader scores "I declined to assert" exactly
// as it scores "I assert nothing exists". Seven of the twenty-six golden
// fixtures were in the first shape.
//
// The distinction is the whole point of this arm, so these tests are written
// as a PAIR against the same loader: absence must be rejected, and an explicit
// empty array must be accepted. A test that only checked one half would pass
// for both shapes and pin nothing.
//
// Everything here uses a temp-dir expected.json rather than a real fixture, so
// the assertions do not move when a golden fixture's rows are written (arm B).

// writeExpectedJSON writes a minimal-but-valid expected.json into a fresh temp
// dir and returns the dir. body is spliced in as the object's fields.
func writeExpectedJSON(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	raw := "{\n" + body + "\n}\n"
	if !json.Valid([]byte(raw)) {
		t.Fatalf("test bug: composed invalid JSON:\n%s", raw)
	}
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestAbsentExpectedRelationshipsIsHardError_6490 pins the load-bearing half:
// a fixture that never mentions the key at all must not load.
func TestAbsentExpectedRelationshipsIsHardError_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "absent-key-mini",
  "language": "erlang",
  "description": "declines to assert anything about relationships",
  "expected_entities": [{"name": "Sup", "kind": "SCOPE.Component", "must_exist": true}]`)

	fix, err := LoadFixture(dir)
	if err == nil {
		t.Fatalf("LoadFixture accepted an expected.json with no expected_relationships "+
			"key (parsed %d rows) — an omitted key must be a hard error, not a silent "+
			"zero-assertion pass", len(fix.ExpectedRelationships))
	}
	if !strings.Contains(err.Error(), "expected_relationships") {
		t.Fatalf("error does not name the missing key, so it does not tell the fixture "+
			"author what to add: %v", err)
	}
}

// TestEmptyExpectedRelationshipsIsAccepted_6490 pins the other half. Without
// this the hard error could be implemented as "reject any fixture with no
// rows", which would make the explicit empty array — the deliberate, visible
// "we assert nothing here" marker this arm introduces — unusable, and would
// make absence and emptiness indistinguishable again, just in the other
// direction.
func TestEmptyExpectedRelationshipsIsAccepted_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "empty-array-mini",
  "language": "php",
  "description": "explicitly asserts nothing",
  "expected_entities": [{"name": "App", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": []`)

	fix, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("LoadFixture rejected an explicit empty expected_relationships array: %v\n"+
			"an empty array is a legitimate declaration; only an absent key is an error", err)
	}
	if len(fix.ExpectedRelationships) != 0 {
		t.Fatalf("expected_relationships: got %d rows, want 0", len(fix.ExpectedRelationships))
	}
}

// TestNullExpectedRelationshipsIsHardError_6490 closes the obvious hole in a
// key-presence check: `"expected_relationships": null` mentions the key while
// still unmarshalling to a nil slice, i.e. it is the absent shape wearing the
// present shape's clothes.
func TestNullExpectedRelationshipsIsHardError_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "null-value-mini",
  "language": "java",
  "description": "key present, value null",
  "expected_entities": [{"name": "Job", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": null`)

	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted expected_relationships: null — a null value is the " +
			"absent shape and must be rejected the same way")
	}
}

// TestPopulatedExpectedRelationshipsIsAccepted_6490 is the positive control:
// the new validation must not reject a fixture that actually has rows.
func TestPopulatedExpectedRelationshipsIsAccepted_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "populated-mini",
  "language": "go",
  "description": "asserts one edge",
  "expected_entities": [{"name": "Handler", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": [{"from_name": "Handler", "to_name": "Store", "kind": "CALLS", "must_exist": true}]`)

	fix, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("LoadFixture rejected a populated fixture: %v", err)
	}
	if len(fix.ExpectedRelationships) != 1 {
		t.Fatalf("expected_relationships: got %d rows, want 1", len(fix.ExpectedRelationships))
	}
}

// TestEveryGoldenFixtureDeclaresExpectedRelationships_6490 is the corpus-wide
// half, in the shape of TestBaseline: it walks the real golden set and asserts
// that every expected.json now DECLARES the key, reading the raw JSON rather
// than the decoded struct so that absence and emptiness stay distinguishable
// at the point of assertion.
//
// The fixture count is pinned so that a fixture added without the key cannot
// slip past by simply not being walked.
func TestEveryGoldenFixtureDeclaresExpectedRelationships_6490(t *testing.T) {
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	var declaredEmpty []string
	fixtures := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(goldenDir, e.Name(), "expected.json")
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fixtures++

		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatalf("%s: parse: %v", p, err)
		}
		v, ok := keys["expected_relationships"]
		if !ok || string(v) == "null" {
			missing = append(missing, e.Name())
			continue
		}
		var rows []ExpectedRelationship
		if err := json.Unmarshal(v, &rows); err != nil {
			t.Fatalf("%s: expected_relationships: %v", p, err)
		}
		if len(rows) == 0 {
			declaredEmpty = append(declaredEmpty, e.Name())
		}

		// And the loader must agree with the raw read.
		if _, err := LoadFixture(filepath.Join(goldenDir, e.Name())); err != nil {
			t.Fatalf("%s: LoadFixture: %v", e.Name(), err)
		}
	}
	if fixtures != 26 {
		t.Fatalf("walked %d golden fixtures, want 26 — the corpus size changed, so this "+
			"test's coverage claim needs re-deriving rather than silently shrinking", fixtures)
	}
	if len(missing) != 0 {
		t.Fatalf("golden fixtures with no expected_relationships key: %v\n"+
			"add an explicit \"expected_relationships\": [] (a visible \"we assert nothing "+
			"here\" marker) or, better, real must-have rows", missing)
	}
	// Not a failure — the count is recorded so that #6490 arm B, which writes
	// real rows, has a number to move and cannot claim progress without it.
	t.Logf("fixtures declaring an EMPTY expected_relationships array (arm B owes these "+
		"real rows): %d %v", len(declaredEmpty), declaredEmpty)
}
