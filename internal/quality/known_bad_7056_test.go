package quality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #7056. `forbidden_relationships` is the only mechanism in this repo that can
// say "this edge is wrong", and a hit is unconditionally fatal. So a precision
// defect that has been found, measured, and cannot be fixed today can only be
// fixed now or not written down; golden/solidity-mini/NOTICE.md carries two
// such findings as markdown prose, invisible to every instrument, for exactly
// that reason.
//
// `known_bad` on a forbidden row demotes the hit from fatal to RECORDED. The
// row still fires, and it is still graded — in BOTH directions. The second
// direction is the one that makes this a mechanism rather than a mute button:
// a known_bad row that STOPS firing fails, because a defect that was fixed and
// a producer that quietly died look identical from here.

// knownBadDoc is the graph every case below grades against: it contains the
// offending edge (Vault CALLS helper) and the offending entity (Ghost), so a
// row naming either FIRES. A case that needs the silent direction removes the
// offender rather than weakening the row, which is what actually happens when
// a defect is fixed or a producer dies.
func knownBadDoc(withOffenders bool) *graph.Document {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "e1", Name: "Vault", Kind: "SCOPE.Component", SourceFile: "Vault.sol"},
		},
	}
	if !withOffenders {
		return doc
	}
	doc.Entities = append(doc.Entities,
		graph.Entity{ID: "e2", Name: "helper", Kind: "SCOPE.Operation", SourceFile: "Vault.sol"},
		graph.Entity{ID: "e3", Name: "Ghost", Kind: "SCOPE.Schema", SourceFile: "Vault.sol"})
	doc.Relationships = []graph.Relationship{
		{ID: "r1", FromID: "e1", ToID: "e2", Kind: "CALLS"},
	}
	return doc
}

func knownBadEdgeRow() ExpectedRelationship {
	return ExpectedRelationship{
		FromName: "Vault", FromKind: "SCOPE.Component",
		Kind:   "CALLS",
		ToName: "helper", ToKind: "SCOPE.Operation",
		KnownBad: true, Issue: "#6425",
		Note: "phantom yul operation minted from an assembly block label",
	}
}

func knownBadEntityRow() ExpectedEntity {
	return ExpectedEntity{
		Name: "Ghost", Kind: "SCOPE.Schema",
		KnownBad: true, Issue: "#6425",
		Note: "assembly label minted as a schema entity",
	}
}

func knownBadFixture() *Fixture {
	return &Fixture{
		Name:                   "kb",
		ForbiddenRelationships: []ExpectedRelationship{knownBadEdgeRow()},
		ForbiddenEntities:      []ExpectedEntity{knownBadEntityRow()},
	}
}

// --- direction 1: fired rows are recorded, not fatal ----------------------

func TestKnownBadHitIsNotCountedAsForbidden_7056(t *testing.T) {
	rep := Evaluate(knownBadFixture(), knownBadDoc(true))
	if got := len(rep.ForbiddenHits); got != 0 {
		t.Fatalf("ForbiddenHits=%d want 0 — a known_bad edge row is still landing in "+
			"the always-fatal counter, so the channel changes nothing", got)
	}
	if got := len(rep.ForbiddenEntityHits); got != 0 {
		t.Fatalf("ForbiddenEntityHits=%d want 0 — the entity arm still routes a "+
			"known_bad row into the fatal counter", got)
	}
	if got := len(rep.KnownBad); got != 2 {
		t.Fatalf("KnownBad=%d want 2 (one edge row, one entity row)", got)
	}
	for _, kb := range rep.KnownBad {
		if !kb.Fired {
			t.Fatalf("known_bad %s row %q did not fire against a graph that contains "+
				"its offender — the positive control below proves it is matchable",
				kb.Class, kb.Label)
		}
		if kb.MatchedID == "" {
			t.Fatalf("known_bad row %q fired but names no offender; the reader is sent "+
				"back to the graph to work out which one", kb.Label)
		}
	}
}

// TestPositiveControl_TheSameRowsWithoutKnownBadAreFatal_7056 is what makes
// every case above evidence. Without it, a change that simply stopped
// evaluating forbidden rows at all would score identically: both counters
// would be 0 and every assertion would hold.
func TestPositiveControl_TheSameRowsWithoutKnownBadAreFatal_7056(t *testing.T) {
	edge, ent := knownBadEdgeRow(), knownBadEntityRow()
	edge.KnownBad, edge.Issue = false, ""
	ent.KnownBad, ent.Issue = false, ""
	fix := &Fixture{
		Name:                   "kb",
		ForbiddenRelationships: []ExpectedRelationship{edge},
		ForbiddenEntities:      []ExpectedEntity{ent},
	}
	rep := Evaluate(fix, knownBadDoc(true))
	if len(rep.ForbiddenHits) != 1 || len(rep.ForbiddenEntityHits) != 1 {
		t.Fatalf("the same two rows without known_bad did not fire as ordinary "+
			"forbidden hits (edges=%d entities=%d) — the graph cannot express the "+
			"defect and nothing above grades anything",
			len(rep.ForbiddenHits), len(rep.ForbiddenEntityHits))
	}
	if len(rep.KnownBad) != 0 {
		t.Fatalf("KnownBad=%d want 0 — a row that never set the flag is being "+
			"recorded as known-bad", len(rep.KnownBad))
	}
}

// --- direction 2: a row that stops firing is the failure ------------------

func TestKnownBadRowThatStopsFiringIsReportedSilent_7056(t *testing.T) {
	rep := Evaluate(knownBadFixture(), knownBadDoc(false))
	if got := len(rep.KnownBad); got != 2 {
		t.Fatalf("KnownBad=%d want 2 — a row that did not fire vanished from the "+
			"report entirely, so nothing downstream can tell it went quiet", got)
	}
	for _, kb := range rep.KnownBad {
		if kb.Fired {
			t.Fatalf("known_bad row %q reported as firing against a graph with no "+
				"offender in it", kb.Label)
		}
	}
	var jr JSONReport
	roundTripJSON(t, rep, &jr)
	if jr.KnownBadHits != 0 {
		t.Fatalf("known_bad_hits=%d want 0", jr.KnownBadHits)
	}
	if jr.KnownBadDeclared != 2 {
		t.Fatalf("known_bad_declared=%d want 2 — the gate detects a silent row by "+
			"comparing the declaration with what fired, so the declared count is "+
			"load-bearing", jr.KnownBadDeclared)
	}
	if len(jr.KnownBadSilent) != 2 {
		t.Fatalf("known_bad_silent has %d row(s), want 2", len(jr.KnownBadSilent))
	}
	for _, row := range jr.KnownBadSilent {
		if row.Issue == "" || row.Note == "" {
			t.Fatalf("a silent known_bad row is serialised without its issue/note "+
				"(%+v) — the gate prints this text and the reader acts on it", row)
		}
	}
}

// --- what the machine report carries --------------------------------------

func roundTripJSON(t *testing.T, rep *Report, into *JSONReport) {
	t.Helper()
	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if err := json.Unmarshal(buf.Bytes(), into); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, buf.String())
	}
}

// TestKnownBadJSONCarriesTheIssueAndNote_7056 pins the artefact CI reads.
// scripts/quality/ratchet.py consumes this JSON and nothing in CI reads the
// human summary, so a diagnostic that exists only in WriteHuman is one a
// developer running the command by hand would see and the gate never would.
func TestKnownBadJSONCarriesTheIssueAndNote_7056(t *testing.T) {
	rep := Evaluate(knownBadFixture(), knownBadDoc(true))
	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	raw := buf.String()
	for _, want := range []string{`"known_bad_declared": 2`, `"known_bad_hits": 2`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("the JSON report does not carry %q — the gate reads this file "+
				"and nothing else:\n%s", want, raw)
		}
	}
	// The rows are asserted after DECODING rather than as raw bytes:
	// encoding/json escapes `>` to \u003e, so a substring search for the edge
	// label fails on a report that carries it perfectly well. ratchet.py reads
	// this file with json.loads and sees the unescaped string, so the decoded
	// value is what the gate actually prints.
	var jr JSONReport
	if err := json.Unmarshal(buf.Bytes(), &jr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := map[string]knownBadRow{}
	for _, row := range jr.KnownBad {
		got[row.Label] = row
	}
	// Class is asserted alongside the label, and the two expected values are
	// DIFFERENT, so a report that stamped one class on both rows fails here.
	// Class is the only thing separating the two kinds of finding once they
	// share one array: the gate prints it ("known_bad entity Ghost ..."), and
	// swapping the two literals was ALIVE until this assertion existed.
	for want, wantClass := range map[string]string{
		"Vault --[CALLS]--> helper": "relationship",
		"Ghost (SCOPE.Schema)":      "entity",
	} {
		row, ok := got[want]
		if !ok {
			t.Fatalf("the JSON report names no known_bad row %q; it carries %v",
				want, jr.KnownBad)
		}
		if row.Class != wantClass {
			t.Fatalf("known_bad row %q is serialised with class %q, want %q — the "+
				"gate prints this word, and the two classes go to different "+
				"diagnoses", want, row.Class, wantClass)
		}
		if row.Issue != "#6425" || row.Note == "" {
			t.Fatalf("known_bad row %q carries no issue/note (%+v) — the gate prints "+
				"this text and the reader acts on it", want, row)
		}
	}
	// The counts are NOT omitempty: a consumer must be able to tell a fixture
	// that declares nothing from a report written before the channel existed,
	// and ratchet.py fails the gate on exactly that distinction.
	clean := Evaluate(&Fixture{Name: "kb"}, knownBadDoc(true))
	var cleanBuf bytes.Buffer
	if err := clean.WriteJSON(&cleanBuf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.Contains(cleanBuf.String(), `"known_bad_declared": 0`) {
		t.Fatalf("a fixture with no known_bad rows omits known_bad_declared — "+
			"absent then means BOTH \"no rows\" and \"old binary\", and the gate "+
			"cannot tell a stale report from a clean one:\n%s", cleanBuf.String())
	}
}

// TestKnownBadIsPrintedOnAGreenHumanReport_7056. A recorded defect that is
// only visible when the gate is red is invisible exactly when someone is in a
// position to act on it — the reasoning ratchet.py already applies to
// known_regressions, which it prints on passing runs.
func TestKnownBadIsPrintedOnAGreenHumanReport_7056(t *testing.T) {
	rep := Evaluate(knownBadFixture(), knownBadDoc(true))
	if len(rep.ForbiddenHits)+len(rep.ForbiddenEntityHits) != 0 {
		t.Fatal("premise broken: this report is not the green one")
	}
	var buf bytes.Buffer
	rep.WriteHuman(&buf)
	out := buf.String()
	for _, want := range []string{"KNOWN-BAD", "#6425", "Vault --[CALLS]--> helper"} {
		if !strings.Contains(out, want) {
			t.Fatalf("a green human report does not mention %q:\n%s", want, out)
		}
	}
	silent := Evaluate(knownBadFixture(), knownBadDoc(false))
	var sbuf bytes.Buffer
	silent.WriteHuman(&sbuf)
	if !strings.Contains(sbuf.String(), "STOPPED FIRING") {
		t.Fatalf("the human report does not distinguish a row that went quiet from "+
			"one that is still firing:\n%s", sbuf.String())
	}
}

// --- load-time validation -------------------------------------------------

func writeKnownBadFixture_7056(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLoadFixture_KnownBadDemandsAnIssueAndANote_7056 mirrors
// TestKnownRegressionsAgreeWithRecordedFloor on the recall side: an untracked
// known-bad is indistinguishable from an accepted one, and a known-bad with no
// note keeps the delta and loses the mechanism.
//
// BOTH FORBIDDEN ARRAYS. The validator is called from four sites — two recall
// arrays and two forbidden ones — and deleting the `forbidden_entities` call
// site was ALIVE against a table that only ever built a forbidden_relationships
// row. A known_bad ENTITY row with no issue and no note was therefore accepted
// at load, which is the decorative-row state this whole validator exists to
// reject.
func TestLoadFixture_KnownBadDemandsAnIssueAndANote_7056(t *testing.T) {
	// One template per forbidden array. They are different KEYS carrying
	// different row shapes, which is exactly why one cannot stand in for the
	// other.
	arrays := map[string]string{
		"forbidden_relationships": `{
		  "fixture_name": "t",
		  "expected_entities": [],
		  "expected_relationships": [],
		  "asserts_no_relationships": true,
		  "forbidden_relationships": [
		    { "from_name": "Vault", "kind": "CALLS", "to_name": "helper", "known_bad": true%s }
		  ]
		}`,
		"forbidden_entities": `{
		  "fixture_name": "t",
		  "expected_entities": [],
		  "expected_relationships": [],
		  "asserts_no_relationships": true,
		  "forbidden_entities": [
		    { "name": "Ghost", "kind": "SCOPE.Schema", "known_bad": true%s }
		  ]
		}`,
	}
	cases := []struct {
		name, extra, want string
	}{
		{"no issue and no note", "", "no issue"},
		{"issue but no note", `, "issue": "#6425"`, "no note"},
		{"note but no issue", `, "note": "why"`, "no issue"},
	}
	for key, tmpl := range arrays {
		for _, c := range cases {
			t.Run(key+"/"+c.name, func(t *testing.T) {
				_, err := LoadFixture(writeKnownBadFixture_7056(t, sprintf(tmpl, c.extra)))
				if err == nil {
					t.Fatalf("LoadFixture accepted a %s known_bad row with %s", key, c.name)
				}
				if !strings.Contains(err.Error(), c.want) {
					t.Fatalf("error does not name the missing field (%q): %v", c.want, err)
				}
				if !strings.Contains(err.Error(), key) {
					t.Fatalf("error does not name the array it came from (%q): %v", key, err)
				}
			})
		}
		// The control, per array: with BOTH present the same row loads.
		// Without it, a validator that rejected every known_bad row would pass
		// all three cases above.
		t.Run(key+"/complete row loads", func(t *testing.T) {
			ok := sprintf(tmpl, `, "issue": "#6425", "note": "why"`)
			fix, err := LoadFixture(writeKnownBadFixture_7056(t, ok))
			if err != nil {
				t.Fatalf("LoadFixture rejected a complete %s known_bad row: %v", key, err)
			}
			var knownBad bool
			var issue string
			if key == "forbidden_relationships" {
				knownBad, issue = fix.ForbiddenRelationships[0].KnownBad, fix.ForbiddenRelationships[0].Issue
			} else {
				knownBad, issue = fix.ForbiddenEntities[0].KnownBad, fix.ForbiddenEntities[0].Issue
			}
			if !knownBad || issue != "#6425" {
				t.Fatalf("known_bad/issue were accepted but not read back on %s", key)
			}
		})
	}
}

// TestLoadFixture_KnownBadIsRejectedOnARecallRow_7056. Evaluate consults the
// flag only on the two forbidden loops, so on a recall row it is honoured by
// nothing: the row would read as a recorded finding and be scored as an
// ordinary must-have. That is the decorative-row failure #6488 arm B
// enumerated, one concept over.
func TestLoadFixture_KnownBadIsRejectedOnARecallRow_7056(t *testing.T) {
	cases := map[string]string{
		"expected_relationships": `{
		  "fixture_name": "t",
		  "expected_entities": [],
		  "expected_relationships": [
		    { "from_name": "A", "kind": "CALLS", "to_name": "B", "must_exist": true,
		      "known_bad": true, "issue": "#1", "note": "n" }
		  ]
		}`,
		"expected_entities": `{
		  "fixture_name": "t",
		  "expected_entities": [
		    { "name": "A", "kind": "SCOPE.Component", "must_exist": true,
		      "known_bad": true, "issue": "#1", "note": "n" }
		  ],
		  "expected_relationships": [],
		  "asserts_no_relationships": true
		}`,
	}
	for key, body := range cases {
		t.Run(key, func(t *testing.T) {
			_, err := LoadFixture(writeKnownBadFixture_7056(t, body))
			if err == nil {
				t.Fatalf("LoadFixture accepted known_bad on a %s row, where it is "+
					"read by nothing", key)
			}
			if !strings.Contains(err.Error(), "known_bad") {
				t.Fatalf("error does not name the offending key: %v", err)
			}
			// The advice must point at the RIGHT forbidden array. Swapping the
			// two branches of forbiddenTwin was ALIVE: the diagnostic told an
			// author to move an entity row to forbidden_relationships, which
			// is the "points the reader at the wrong thing" defect #6476 spent
			// its budget removing, in a message a confused author follows.
			wantTwin := "forbidden_relationships"
			if key == "expected_entities" {
				wantTwin = "forbidden_entities"
			}
			if !strings.Contains(err.Error(), wantTwin) {
				t.Fatalf("a %s row is told to move to the wrong array — the error "+
					"does not mention %q: %v", key, wantTwin, err)
			}
		})
	}
}

// TestLoadFixture_IssueWithoutKnownBadIsRejected_7056. `issue` is read only on
// a known_bad row, so setting it elsewhere states a tracking claim the grader
// does not honour. Rejected rather than ignored, for the same reason
// to_must_be_typed is rejected on a forbidden row.
func TestLoadFixture_IssueWithoutKnownBadIsRejected_7056(t *testing.T) {
	bodies := map[string]string{
		"forbidden_relationships": `{
		  "fixture_name": "t",
		  "expected_entities": [],
		  "expected_relationships": [],
		  "asserts_no_relationships": true,
		  "forbidden_relationships": [
		    { "from_name": "Vault", "kind": "CALLS", "to_name": "helper", "issue": "#6425" }
		  ]
		}`,
		// The twin. Both forbidden arrays reach the same validator through
		// their own call site, and a call site that is not exercised is a
		// call site that can be deleted.
		"forbidden_entities": `{
		  "fixture_name": "t",
		  "expected_entities": [],
		  "expected_relationships": [],
		  "asserts_no_relationships": true,
		  "forbidden_entities": [
		    { "name": "Ghost", "kind": "SCOPE.Schema", "issue": "#6425" }
		  ]
		}`,
	}
	for key, body := range bodies {
		t.Run(key, func(t *testing.T) {
			if _, err := LoadFixture(writeKnownBadFixture_7056(t, body)); err == nil {
				t.Fatalf("LoadFixture accepted issue on a %s row that is not known_bad", key)
			}
		})
	}
}

// TestNoGoldenFixtureDeclaresKnownBadYet_7056 is the scope pin for this PR.
// The mechanism lands here; converting solidity-mini's two parked findings and
// vbnet-mini's tradeoff row is a separate change with its own measurement, and
// mixing them means a red gate cannot be attributed. When the first row is
// converted, this test is the thing that has to be deliberately deleted.
func TestNoGoldenFixtureDeclaresKnownBadYet_7056(t *testing.T) {
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("read %s: %v", goldenDir, err)
	}
	scanned := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(goldenDir, e.Name(), "expected.json")
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		scanned++
		if bytes.Contains(raw, []byte("known_bad")) {
			t.Errorf("%s declares a known_bad row — the mechanism PR deliberately "+
				"converts none; if this is the follow-up, delete this test in the "+
				"same commit", p)
		}
	}
	// A walk that read nothing reports clean, and a walk that read TEN of
	// thirty-eight reports clean for the other twenty-eight. `!= 38` is the
	// convention three sibling tests in this package already use over this same
	// directory (TestEveryGoldenFixtureDeclaresExpectedRelationships_6490 among
	// them), and its comment gives the reason: a coverage claim must be
	// re-derived when the corpus changes rather than silently shrinking. A `<`
	// floor cannot do that — it accepts every number above it, including the
	// one a truncated walk produces.
	if scanned != 38 {
		t.Fatalf("scanned %d expected.json file(s) under %s, want 38 — either the "+
			"corpus size changed (re-derive this claim) or the walk is truncated, "+
			"in which case every unscanned fixture is ungraded by this test",
			scanned, goldenDir)
	}
}

// sprintf is a local alias so the table above reads as data rather than as a
// wall of fmt.Sprintf calls.
func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
