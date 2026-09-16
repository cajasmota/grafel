package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #7071 — a must_exist row naming a dotted target is satisfied by an edge
// the extractor emitted with NO type information at all, as long as that
// target happens to be the only same-named thing in the caller's file: the
// resolver's locality tier launders the untyped guess into exactly the
// target the row is asserting. `to_must_be_typed` is the narrowing that
// makes such a row grade what it claims to grade.
//
// Every test below is built on the SAME pair of documents — one where the
// edge carries the guess marker, one where it does not — because that pair
// IS the property. Two documents that differ in the marker and in nothing
// else are the only thing that can show the field is load-bearing rather
// than decorative.

// touchDoc models the #7068 reproduction reduced to two entities: a caller
// and the method it calls. guessed says whether the resolver reached the
// target through a lexical guess tier or through the receiver type.
//
// Note there is NO competitor — no second `*.Touch` — which is precisely
// the shape the grounding measured on 146 of the golden corpus's 507
// must_exist edge rows (29%). Adding one is the workaround this field
// exists to replace.
func touchDoc(guessed bool) *graph.Document {
	rel := graph.Relationship{
		ID: "rel-touch", FromID: "sha-replay", ToID: "sha-touch", Kind: "CALLS",
	}
	if guessed {
		rel.PropSet(types.PropBindTier, "file-leaf-name")
	}
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-replay", Name: "AuditLog.Replay", Kind: "SCOPE.Operation", SourceFile: "Services/UserService.cs"},
			{ID: "sha-touch", Name: "AuditEntry.Touch", Kind: "SCOPE.Operation", SourceFile: "Services/UserService.cs"},
		},
		Relationships: []graph.Relationship{rel},
	}
}

func touchRow(typed bool) ExpectedRelationship {
	return ExpectedRelationship{
		FromName: "AuditLog.Replay", FromKind: "SCOPE.Operation", FromFile: "Services/UserService.cs",
		Kind:   "CALLS",
		ToName: "AuditEntry.Touch", ToKind: "SCOPE.Operation", ToFile: "Services/UserService.cs",
		ToMustBeTyped: typed,
		MustExist:     true,
	}
}

// TestToMustBeTypedRejectsAGuessedEdge_7071 is the whole point: the
// narrowed row goes RED on the graph the un-narrowed row scores green.
//
// VARIED across the four sub-cases: whether the edge was guessed, and
// whether the row demands a typed bind.
// HELD CONSTANT: the entities, the edge's endpoints and kind, and the row's
// from/to/kind/file axes — so the ONLY thing that can move the verdict is
// the pair (marker present, field set). Three of the four cells are green;
// the one red cell is the behaviour that did not exist before, and its
// three green neighbours are what stop the field from being a blanket
// tightening.
func TestToMustBeTypedRejectsAGuessedEdge_7071(t *testing.T) {
	cases := []struct {
		name      string
		guessed   bool
		typed     bool
		wantFound bool
	}{
		{"evidence-bound edge, row does not care", false, false, true},
		{"evidence-bound edge, row demands typed", false, true, true},
		{"guessed edge, row does not care — the pre-#7071 blind spot", true, false, true},
		{"guessed edge, row demands typed — the new red", true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := Evaluate(oneRow(touchRow(c.typed)), touchDoc(c.guessed))
			if rep.RelExpected != 1 {
				t.Fatalf("row not scored as must_exist: expected=%d", rep.RelExpected)
			}
			got := rep.RelFound == 1
			if got != c.wantFound {
				t.Fatalf("found=%v, want %v (guessed=%v typed=%v)", got, c.wantFound, c.guessed, c.typed)
			}
			if c.wantFound && rep.RelResults[0].MatchedRelID != "rel-touch" {
				t.Fatalf("matched the wrong edge: %q", rep.RelResults[0].MatchedRelID)
			}
		})
	}
}

// TestToMustBeTypedFallsThroughToAnEvidenceBoundEdge_7071 pins that the
// narrowing SKIPS a guessed candidate rather than stopping at it. A row
// that a later, evidence-bound edge satisfies must still be found — the
// question the field asks is "does the graph contain an evidence-bound
// edge like this", not "is the first candidate evidence-bound".
//
// Without the fall-through this test goes red while the graph plainly
// contains what the row asks for, which would be a false regression report
// on any fixture whose caller makes the same call twice.
func TestToMustBeTypedFallsThroughToAnEvidenceBoundEdge_7071(t *testing.T) {
	guessed := graph.Relationship{ID: "rel-guess", FromID: "sha-replay", ToID: "sha-touch", Kind: "CALLS"}
	guessed.PropSet(types.PropBindTier, "file-leaf-name")
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-replay", Name: "AuditLog.Replay", Kind: "SCOPE.Operation", SourceFile: "Services/UserService.cs"},
			{ID: "sha-touch", Name: "AuditEntry.Touch", Kind: "SCOPE.Operation", SourceFile: "Services/UserService.cs"},
			// A second caller by the same (kind, name, file) axes, so both
			// edges are candidates for the one row.
			{ID: "sha-replay2", Name: "AuditLog.Replay", Kind: "SCOPE.Operation", SourceFile: "Services/UserService.cs"},
		},
		Relationships: []graph.Relationship{
			guessed,
			{ID: "rel-typed", FromID: "sha-replay2", ToID: "sha-touch", Kind: "CALLS"},
		},
	}
	rep := Evaluate(oneRow(touchRow(true)), doc)
	if rep.RelFound != 1 {
		t.Fatalf("row demanding a typed bind must match the evidence-bound edge: found=%d", rep.RelFound)
	}
	if got := rep.RelResults[0].MatchedRelID; got != "rel-typed" {
		t.Fatalf("matched %q, want rel-typed — the guessed edge must be skipped, not returned", got)
	}
}

// TestToMustBeTypedIsRejectedOnAForbiddenRow_7071 and its sibling pin the
// LoadFixture guards. Without them the field is another ungraded axis: a
// fixture author can write it somewhere it silently does nothing, or —
// worse — somewhere it inverts.
//
// VARIED: which invalid placement is attempted.
// HELD CONSTANT: the fixture is otherwise valid and would load.
func TestToMustBeTypedPlacementGuards_7071(t *testing.T) {
	valid := `{"fixture_name":"x","expected_relationships":[` +
		`{"from_name":"A","kind":"CALLS","to_name":"B","must_exist":true}]`
	cases := []struct {
		name     string
		json     string
		wantFrag string
	}{
		{
			// On a forbidden row the field WIDENS: the edge becomes
			// allowed whenever the resolver guessed its target.
			name: "forbidden row",
			json: valid + `,"forbidden_relationships":[` +
				`{"from_name":"A","kind":"CALLS","to_name":"C","to_must_be_typed":true}]}`,
			wantFrag: "WIDENS",
		},
		{
			// Neither must_exist nor nice_to_have: nothing scores the row,
			// so narrowing it narrows nothing.
			name: "unscored row",
			json: `{"fixture_name":"x","expected_relationships":[` +
				`{"from_name":"A","kind":"CALLS","to_name":"B","must_exist":true},` +
				`{"from_name":"A","kind":"CALLS","to_name":"D","to_must_be_typed":true}]}`,
			wantFrag: "grades nothing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(c.json), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFixture(dir)
			if err == nil {
				t.Fatalf("LoadFixture accepted to_must_be_typed on a %s", c.name)
			}
			if !strings.Contains(err.Error(), c.wantFrag) {
				t.Fatalf("error %q does not explain the problem (want a mention of %q)", err, c.wantFrag)
			}
		})
	}
}

// TestToMustBeTypedAbsentIsUnchanged_7071 is the compatibility control.
// Every one of the corpus's pre-#7071 rows omits the field, and omitting it
// must mean exactly what it meant before — otherwise this change tightens
// 507 rows at once under cover of adding one.
func TestToMustBeTypedAbsentIsUnchanged_7071(t *testing.T) {
	rep := Evaluate(oneRow(touchRow(false)), touchDoc(true))
	if rep.RelFound != 1 {
		t.Fatalf("a row without to_must_be_typed must still match a guessed edge: found=%d", rep.RelFound)
	}
}
