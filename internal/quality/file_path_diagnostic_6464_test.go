package quality

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6464 follow-up — strict file narrowing created a NEW misdiagnosis.
//
// Before strict narrowing, a row naming a wrong path was silently rescued by
// the unnarrowed lookup, so it never reached report.go's diagnostic switch.
// Now it resolves to nothing, which makes FromResolved/ToResolved false, and
// the switch's `!rr.ToResolved` arm prints
//
//	(root cause: to-entity not extracted)
//
// for an entity that WAS extracted — just under a different path. That is the
// "blame the extractor for a fixture-row bug" class #6476 was written to
// remove, reappearing on the file axis, and it fires on the single likeliest
// authoring mistake: a mistyped path.
//
// These tests drive Evaluate -> WriteHuman, not the predicate, because the
// defect is what the report SAYS.

// pathDiag returns the diagnostic line for the single missing relationship.
func pathDiag(t *testing.T, er ExpectedRelationship) string {
	t.Helper()
	er.MustExist = true
	fix := &Fixture{Name: "erlang-shaped", ExpectedRelationships: []ExpectedRelationship{er}}
	out := humanReport(t, fix, twoFileDoc())
	if !strings.Contains(out, "missing relationships:") {
		t.Fatalf("row unexpectedly matched; report:\n%s", out)
	}
	return out
}

// The Finding-A scenario verbatim: `init` IS extracted in cache_sup.erl, and
// the row says `src/cache_sup.erl`.
func TestWrongToFileIsBlamedOnTheRowNotTheExtractor_6464(t *testing.T) {
	out := pathDiag(t, ExpectedRelationship{
		FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "cache_sup.erl",
		Kind:   "CONTAINS",
		ToName: "init", ToKind: "SCOPE.Operation", ToFile: "src/cache_sup.erl",
	})
	if strings.Contains(out, "to-entity not extracted") {
		t.Fatalf("the to-entity IS extracted (cache_sup.erl); only the path is wrong:\n%s", out)
	}
	if !strings.Contains(out, `to_file "src/cache_sup.erl"`) {
		t.Fatalf("diagnostic does not name the offending to_file:\n%s", out)
	}
	if !strings.Contains(out, "FIXTURE ROW") {
		t.Fatalf("diagnostic does not attribute the miss to the fixture row:\n%s", out)
	}
}

func TestWrongFromFileIsBlamedOnTheRowNotTheExtractor_6464(t *testing.T) {
	out := pathDiag(t, ExpectedRelationship{
		FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "src/cache_sup.erl",
		Kind:   "CONTAINS",
		ToName: "init", ToKind: "SCOPE.Operation", ToFile: "cache_sup.erl",
	})
	if strings.Contains(out, "from-entity not extracted") {
		t.Fatalf("the from-entity IS extracted (cache_sup.erl); only the path is wrong:\n%s", out)
	}
	if !strings.Contains(out, `from_file "src/cache_sup.erl"`) {
		t.Fatalf("diagnostic does not name the offending from_file:\n%s", out)
	}
}

// PLACEMENT. The path arm must sit ahead of EVERY missing-endpoint arm, not
// merely ahead of the default. A wrong path makes its endpoint unresolved, so
// the row also satisfies `!FromResolved && !ToResolved`, `!FromResolved` and
// `!ToResolved`; moving the arm behind any of them restores the exact
// misdiagnosis it exists to remove. This case has BOTH paths wrong, so it
// pins the arm ahead of the NEITHER arm specifically — the two single-sided
// tests above pin it ahead of the other two.
func TestFileNarrowingArmMustOutrankTheNotExtractedArms_6464(t *testing.T) {
	out := pathDiag(t, ExpectedRelationship{
		FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "src/cache_sup.erl",
		Kind:   "CONTAINS",
		ToName: "init", ToKind: "SCOPE.Operation", ToFile: "src/cache_sup.erl",
	})
	if strings.Contains(out, "NEITHER endpoint extracted") {
		t.Fatalf("both endpoints ARE extracted; both paths are wrong:\n%s", out)
	}
	if !strings.Contains(out, `from_file "src/cache_sup.erl" and to_file "src/cache_sup.erl"`) {
		t.Fatalf("diagnostic does not name both offending paths:\n%s", out)
	}
}

// NEGATIVE. A row naming a file for an entity that genuinely does not exist
// anywhere in the graph must KEEP the honest extractor diagnostic. Without
// this, a flag keyed on "the row named a file" rather than on "the name exists
// unnarrowed" would blame the fixture for every real extractor miss.
func TestGenuinelyAbsentEntityKeepsTheExtractorDiagnostic_6464(t *testing.T) {
	out := pathDiag(t, ExpectedRelationship{
		FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "cache_sup.erl",
		Kind:   "CONTAINS",
		ToName: "terminate", ToKind: "SCOPE.Operation", ToFile: "cache_sup.erl",
	})
	if !strings.Contains(out, "to-entity not extracted") {
		t.Fatalf("`terminate` is in no file; the extractor diagnostic is the true one:\n%s", out)
	}
	if strings.Contains(out, "FIXTURE ROW") {
		t.Fatalf("a genuinely absent entity must not be blamed on the row:\n%s", out)
	}
}

// A row whose paths are RIGHT and which matched carries neither flag.
func TestMatchedRowCarriesNoPathFlag_6464(t *testing.T) {
	fix := &Fixture{Name: "erlang-shaped", ExpectedRelationships: []ExpectedRelationship{
		{FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "cache_sup.erl",
			Kind: "CONTAINS", ToName: "init", ToKind: "SCOPE.Operation",
			ToFile: "cache_sup.erl", MustExist: true},
	}}
	rr := Evaluate(fix, twoFileDoc()).RelResults[0]
	if !rr.Found {
		t.Fatalf("row should have matched: %+v", rr)
	}
	if rr.FromFileMatchedNothing || rr.ToFileMatchedNothing {
		t.Fatalf("a matched row must carry no path flag: %+v", rr)
	}
}

// The flags reach machine consumers, for the same reason #6476 serialised
// to_bare_name_is_entity: from_resolved:false alone reads as an extractor miss.
func TestPathFlagsAreSerialisedAndOtherwiseOmitted_6464(t *testing.T) {
	flagged := &Fixture{Name: "erlang-shaped", ExpectedRelationships: []ExpectedRelationship{
		{FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "cache_sup.erl",
			Kind: "CONTAINS", ToName: "init", ToKind: "SCOPE.Operation",
			ToFile: "src/cache_sup.erl", MustExist: true},
	}}
	b, err := json.Marshal(Evaluate(flagged, twoFileDoc()).ToJSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"to_file_matched_nothing":true`) {
		t.Fatalf("flag missing from JSON:\n%s", b)
	}

	// An ordinary extractor miss must not gain either key — omitempty keeps
	// every pre-existing report byte-identical.
	plain := &Fixture{Name: "erlang-shaped", ExpectedRelationships: []ExpectedRelationship{
		{FromName: "cache_sup", FromKind: "SCOPE.Component",
			Kind: "CONTAINS", ToName: "flush", ToKind: "SCOPE.Operation", MustExist: true},
	}}
	b2, err := json.Marshal(Evaluate(plain, twoFileDoc()).ToJSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b2), "matched_nothing") {
		t.Fatalf("unflagged row gained a key:\n%s", b2)
	}
}

// ---------------------------------------------------------------------------
// Gaps in the #6464 suite proven live by mutation (M2 / M3 / M7).
// ---------------------------------------------------------------------------

// blankKindDoc has an entity whose Kind is blank, so the unnarrowed
// (kind="", name) bucket is reachable. Without such an entity the M3 mutant is
// equivalent: a blank-kind row falls into an empty bucket either way.
func blankKindDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "blank", Name: "init", Kind: "", SourceFile: "cache_server.erl"},
		},
	}
}

// M2 — the entity file gate must NOT exempt nice_to_have rows. 3 of 423 entity
// rows in the golden set are nice_to_have AND carry a source_file, so an
// exemption is a live widening, not a theoretical one.
func TestFileGateAppliesToNiceToHaveEntityRows_6464(t *testing.T) {
	fix := &Fixture{Name: "erlang-shaped", ExpectedEntities: []ExpectedEntity{
		{Name: "init", Kind: "SCOPE.Operation", SourceFile: "src/cache_sup.erl",
			NiceToHave: true},
	}}
	rep := Evaluate(fix, twoFileDoc())
	if rep.NiceEntityTotal != 1 {
		t.Fatalf("NiceEntityTotal=%d want 1", rep.NiceEntityTotal)
	}
	if rep.NiceEntityFound != 0 {
		t.Fatalf("NiceEntityFound=%d want 0 — a nice_to_have row that names a file and "+
			"misses must not be answered by another file's entity", rep.NiceEntityFound)
	}
}

// M3 — the entity file gate must NOT exempt blank-kind rows either. Same axis
// as the nice_to_have exemption, other field.
func TestFileGateAppliesToBlankKindEntityRows_6464(t *testing.T) {
	fix := &Fixture{Name: "blank-kind", ExpectedEntities: []ExpectedEntity{
		{Name: "init", Kind: "", SourceFile: "src/cache_server.erl", MustExist: true},
	}}
	rep := Evaluate(fix, blankKindDoc())
	if rep.EntityFound != 0 {
		t.Fatalf("EntityFound=%d want 0 — a blank-kind row that names a file and misses "+
			"must not fall back to the unnarrowed blank-kind bucket", rep.EntityFound)
	}
}

// M7 — the FROM-side kind-blank scan is retained deliberately for rows that
// name no file. TestKindBlankScanStillWorksWhenNoFileIsNamed exercises the TO
// side only, leaving half the kept affordance unpinned.
func TestFromSideKindBlankScanStillWorksWhenNoFileIsNamed_6464(t *testing.T) {
	fix := &Fixture{Name: "erlang-shaped", ExpectedRelationships: []ExpectedRelationship{
		{FromName: "cache_sup", FromKind: "",
			Kind: "CONTAINS", ToName: "init", ToKind: "SCOPE.Operation", MustExist: true},
	}}
	rep := Evaluate(fix, twoFileDoc())
	if rep.RelFound != 1 {
		t.Fatalf("RelFound=%d want 1 — the FROM-side kind-blank scan is retained for rows "+
			"that name no file", rep.RelFound)
	}
}

// ---------------------------------------------------------------------------
// Two-intents rows are rejected at LOAD, not diagnosed after the fact.
// ---------------------------------------------------------------------------

// loadTmpFixture writes expected.json into a temp fixture dir and loads it.
func loadTmpFixture(t *testing.T, body string) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadFixture(dir)
	return err
}

func TestQualifiedNameRowWithSourceFileIsRejectedAtLoad_6464(t *testing.T) {
	err := loadTmpFixture(t, `{"fixture_name":"x","expected_entities":[
		{"name":"init","kind":"SCOPE.Operation","qualified_name":"m.init",
		 "match_by":"qualified_name","source_file":"cache_sup.erl","must_exist":true}]}`)
	if err == nil {
		t.Fatal("match_by=qualified_name + source_file states two intents and honours " +
			"only one; it must not load silently")
	}
	for _, want := range []string{"qualified_name", "source_file", "cache_sup.erl", "init"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not name %q: %v", want, err)
		}
	}
}

// Each half alone is coherent and must still load — the rejection is scoped to
// the combination, not to either field.
func TestQualifiedNameOrSourceFileAloneStillLoads_6464(t *testing.T) {
	for name, body := range map[string]string{
		"qualified_name only": `{"fixture_name":"x","expected_entities":[
			{"name":"init","kind":"K","qualified_name":"m.init","match_by":"qualified_name",
			 "must_exist":true}]}`,
		"source_file only": `{"fixture_name":"x","expected_entities":[
			{"name":"init","kind":"K","source_file":"cache_sup.erl","must_exist":true}]}`,
		"match_by source_file with source_file": `{"fixture_name":"x","expected_entities":[
			{"name":"init","kind":"K","match_by":"source_file","source_file":"cache_sup.erl",
			 "must_exist":true}]}`,
	} {
		if err := loadTmpFixture(t, body); err != nil {
			t.Fatalf("%s: unexpected load error: %v", name, err)
		}
	}
}
