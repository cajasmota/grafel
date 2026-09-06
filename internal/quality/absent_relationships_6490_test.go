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

// TestAbsentExpectedRelationshipsIsHardErrorWithNoEntitiesEither_6490 closes
// the degenerate corner, which is the extreme form of the very shape #6490 is
// about: a fixture that asserts NOTHING — no entities, no relationships.
//
// It exists because a check written as `len(f.ExpectedEntities) > 0 && <key
// absent>` passes the whole suite otherwise: every other case here carries one
// entity row, so an entity-count guard is invisible to them. Nothing in
// LoadFixture requires expected_entities to be non-empty, so this input is
// reachable, and it is exactly the fixture the gate must not wave through.
func TestAbsentExpectedRelationshipsIsHardErrorWithNoEntitiesEither_6490(t *testing.T) {
	dir := writeExpectedJSON(t, `  "fixture_name": "asserts-literally-nothing"`)

	if _, err := LoadFixture(dir); err == nil {
		t.Fatal("LoadFixture accepted a fixture asserting nothing at all — no " +
			"expected_entities and no expected_relationships key. The relationship " +
			"declaration must be required unconditionally, not only for fixtures that " +
			"happen to assert an entity")
	}
}

// TestEmptyExpectedRelationshipsIsDeclaredButNotSufficient_6490 pins the
// other half of the absent-vs-empty distinction, which still matters: an
// empty array is a DECLARATION, and must be rejected for a different reason
// than an absent key — it fails the must-have floor, not the key check.
//
// Without this the absent-key error could be implemented as "reject any
// fixture with no rows", which would make absence and emptiness
// indistinguishable again, just in the other direction. The two errors are
// therefore asserted to be different errors, by their text.
func TestEmptyExpectedRelationshipsIsDeclaredButNotSufficient_6490(t *testing.T) {
	empty := writeExpectedJSON(t, `  "fixture_name": "empty-array-mini",
  "language": "php",
  "description": "explicitly asserts nothing",
  "expected_entities": [{"name": "App", "kind": "SCOPE.Component", "must_exist": true}],
  "expected_relationships": []`)
	absent := writeExpectedJSON(t, `  "fixture_name": "absent-key-mini",
  "language": "php",
  "description": "declines to assert",
  "expected_entities": [{"name": "App", "kind": "SCOPE.Component", "must_exist": true}]`)

	emptyErr, absentErr := mustLoadError(t, empty), mustLoadError(t, absent)
	if !strings.Contains(emptyErr, "must-have") {
		t.Fatalf("an explicit empty array must be rejected for asserting nothing, not "+
			"for omitting the key: %s", emptyErr)
	}
	if !strings.Contains(absentErr, "is required") {
		t.Fatalf("an absent key must still be rejected as a missing declaration: %s", absentErr)
	}
	if emptyErr == absentErr {
		t.Fatalf("empty and absent produce the identical error %q — the loader has "+
			"collapsed the two shapes it exists to distinguish", emptyErr)
	}
}

// mustLoadError fails the test unless LoadFixture rejects dir, and returns the
// error text.
func mustLoadError(t *testing.T, dir string) string {
	t.Helper()
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatalf("LoadFixture accepted %s, want an error", dir)
	}
	return err.Error()
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
// that every expected.json now DECLARES the key. It reads the raw JSON rather
// than the decoded struct so the assertion is made against the bytes on disk —
// i.e. it observes the declaration itself rather than a decoded consequence of
// it, and so cannot be satisfied by a loader that stopped checking.
//
// The fixture count is pinned so that a fixture added without the key cannot
// slip past by simply not being walked. `fixtures++` deliberately runs AFTER
// the key check rather than after the file is opened: a census that counts
// files OPENED lets a filter inserted mid-loop shrink coverage while the
// denominator stays 27. Counting files INSPECTED makes the pin defend itself.
func TestEveryGoldenFixtureDeclaresExpectedRelationships_6490(t *testing.T) {
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	var assertsNoMustHave []string
	var loadErrs []string
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

		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatalf("%s: parse: %v", p, err)
		}
		v, ok := keys["expected_relationships"]
		if !ok || string(v) == "null" {
			missing = append(missing, e.Name())
			continue
		}
		fixtures++

		var rows []ExpectedRelationship
		if err := json.Unmarshal(v, &rows); err != nil {
			t.Fatalf("%s: expected_relationships: %v", p, err)
		}
		mustHave := 0
		for _, r := range rows {
			if r.MustExist {
				mustHave++
			}
		}
		if mustHave == 0 {
			assertsNoMustHave = append(assertsNoMustHave, e.Name())
		}

		// And the loader must agree with the raw read. Collected rather than
		// fatal so the report names EVERY offending fixture in one run —
		// arm B is scoped from this list, and a first-failure abort would
		// hand it a list of one.
		if _, err := LoadFixture(filepath.Join(goldenDir, e.Name())); err != nil {
			loadErrs = append(loadErrs, e.Name()+": "+err.Error())
		}
	}
	if len(missing) != 0 {
		t.Fatalf("golden fixtures with no expected_relationships key: %v\n"+
			"add an explicit \"expected_relationships\": [] (a visible \"we assert nothing "+
			"here\" marker) or, better, real must-have rows", missing)
	}
	// Checked only after `missing` is reported, because a fixture that failed
	// the key check never reached the counter — this is the number of fixtures
	// actually INSPECTED, not the number of files opened.
	if fixtures != 34 {
		t.Fatalf("inspected %d golden fixtures, want 34 — the corpus size changed, so this "+
			"test's coverage claim needs re-deriving rather than silently shrinking", fixtures)
	}
	// A FAILURE, not a t.Logf. It was a log while arm A only required the key
	// to be declared; the owner's decision on #6490 makes a zero-must-have
	// assertion a hard grader failure, and a count that is only logged is the
	// same unfailable gate one level up.
	//
	// The debt is counted in MUST-HAVE rows, not in rows: haskell-warp-mini
	// carries three rows that are all `must_exist:false`, so counting rows
	// would report 8 and hand arm B a number two short of its own debt — the
	// nine fixtures baseline.json records at `relationship_expected: 0`.
	//
	// These fixtures are EXPECTED to be red until arm B writes their rows. Do
	// not close this by relaxing the loader or by stamping the opt-in on them.
	if len(assertsNoMustHave) != 0 {
		t.Errorf("golden fixtures asserting NO must-have relationship: %d %v\n"+
			"each of these grades its language's edge extraction with nothing — #6490 "+
			"arm B owes them real must_exist rows", len(assertsNoMustHave), assertsNoMustHave)
	}
	if len(loadErrs) != 0 {
		t.Errorf("golden fixtures LoadFixture now rejects:\n  %s", strings.Join(loadErrs, "\n  "))
	}
}
