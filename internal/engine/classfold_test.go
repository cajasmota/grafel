package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// cfRows renders a record set as sorted "kind|name|subtype|file" content
// tuples. Assertions below compare these sets WHOLE, in both directions: a
// "the typed row is present" check passes while the generic row also survives
// (two nodes for one class), and a "the generic row is gone" check passes on an
// empty set.
func cfRows(recs []types.EntityRecord) []string {
	out := make([]string, 0, len(recs))
	for i := range recs {
		r := &recs[i]
		out = append(out, r.Kind+"|"+r.Name+"|"+r.Subtype+"|"+r.SourceFile)
	}
	sort.Strings(out)
	return out
}

func cfAssertRows(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("record rows:\n  want %v\n  got  %v", want, got)
	}
}

func cfSource(name, file string) types.EntityRecord {
	return types.EntityRecord{
		Kind: "SCOPE.Component", Subtype: "class", Name: name, SourceFile: file,
		QualifiedName: "m." + name, Language: "python", StartLine: 3, EndLine: 9,
		Properties: map[string]string{"role": "class", "provenance": "INFERRED_FROM_CLASS_HIERARCHY"},
	}
}

func cfFramework(kind, name, file string) types.EntityRecord {
	return types.EntityRecord{
		Kind: kind, Name: name, SourceFile: file,
		QualifiedName: "m." + name, Language: "python", StartLine: 3,
		Properties: map[string]string{"framework": "python", "pattern_type": "yaml_driven"},
	}
}

// TestFoldFrameworkClassKinds_TypedSurvivorReplacesGenericNode is the #6148
// core: the generic class record becomes the framework-typed record.
func TestFoldFrameworkClassKinds_TypedSurvivorReplacesGenericNode(t *testing.T) {
	recs := []types.EntityRecord{
		cfSource("Probe", "a.py"),
		{Kind: "SCOPE.Operation", Subtype: "method", Name: "Probe.m", SourceFile: "a.py"},
	}
	fw := []types.EntityRecord{cfFramework("Controller", "Probe", "a.py")}

	out, n := FoldFrameworkClassKinds(recs, fw)
	if n != 1 {
		t.Errorf("folded %d records, want 1", n)
	}
	cfAssertRows(t, cfRows(out), []string{
		"Controller|Probe||a.py",
		"SCOPE.Operation|Probe.m|method|a.py",
	})

	// The survivor is the framework record — including its EMPTY subtype, which
	// the content-keyed parity comparator compares — carrying the properties the
	// source had and it lacked, minus the source-describing ones.
	var sv types.EntityRecord
	for _, r := range out {
		if r.Kind == "Controller" {
			sv = r
		}
	}
	if sv.Properties["framework"] != "python" || sv.Properties["pattern_type"] != "yaml_driven" {
		t.Errorf("survivor lost its own properties: %v", sv.Properties)
	}
	if got := sv.Properties["role"]; got != "class" {
		t.Errorf("survivor should inherit the source property it lacks (role): %v", sv.Properties)
	}
	if _, ok := sv.Properties["provenance"]; ok {
		t.Errorf("provenance describes the folded-away source and must not travel: %v", sv.Properties)
	}
}

// TestFoldFrameworkClassKinds_ExtractorEmittedSurvivorAbsorbsGenericNode is the
// #1700 case: the SURVIVOR comes from the LANGUAGE EXTRACTOR, not from Detect.
// kotlin emits SCOPE.Service for a Spring-stereotyped class and a generic
// SCOPE.Component for the same symbol; proto, razor, cobol, fsharp and elm have
// the same shape. A fold that indexed candidates only from Detect's output left
// BOTH nodes standing — one class, two nodes, the exact invariant the fold
// exists to enforce.
func TestFoldFrameworkClassKinds_ExtractorEmittedSurvivorAbsorbsGenericNode(t *testing.T) {
	svc := types.EntityRecord{
		Kind: "SCOPE.Service", Name: "BillingService", SourceFile: "Billing.kt",
		QualifiedName: "Billing.kt::BillingService", Language: "kotlin", StartLine: 6, EndLine: 9,
		Properties: map[string]string{"provenance": "@Service", "source_type": "class"},
	}
	recs := []types.EntityRecord{cfSource("BillingService", "Billing.kt"), svc}

	// No Detect output at all — the survivor must still be found.
	out, n := FoldFrameworkClassKinds(recs, nil)
	if n != 1 {
		t.Errorf("folded %d records, want 1", n)
	}
	cfAssertRows(t, cfRows(out), []string{"SCOPE.Service|BillingService||Billing.kt"})
}

// TestFoldFrameworkClassKinds_NoCandidateLeavesGenericNode pins the other half:
// a class no rule matches must stay generic. Without this, a fix that typed
// every class would pass the test above and corrupt every unmatched class.
func TestFoldFrameworkClassKinds_NoCandidateLeavesGenericNode(t *testing.T) {
	recs := []types.EntityRecord{cfSource("Probe", "a.py")}
	before := cfRows(recs)

	// Framework records that must NOT match the class: right name wrong file,
	// right file wrong name, and a kind that is not class-declaration-strength.
	// All three still have to be EMITTED — they are Detect's non-class output,
	// which the full rebuild carries and this path must too.
	fw := []types.EntityRecord{
		cfFramework("Controller", "Probe", "other.py"),
		cfFramework("Controller", "Other", "a.py"),
		cfFramework("Route", "Probe", "a.py"),
	}
	out, n := FoldFrameworkClassKinds(recs, fw)
	if n != 0 {
		t.Errorf("folded %d records, want 0", n)
	}
	cfAssertRows(t, cfRows(out), append(before,
		"Controller|Other||a.py", "Controller|Probe||other.py", "Route|Probe||a.py"))
}

// TestFoldFrameworkClassKinds_NonClassFrameworkRecordsAreEmitted is #6150's
// half: Detect output that pairs with NO extractor record (a Route from a
// responder method, a Service from an app object, a Config from its
// constructor) is what a full rebuild carries and an incremental run dropped.
func TestFoldFrameworkClassKinds_NonClassFrameworkRecordsAreEmitted(t *testing.T) {
	recs := []types.EntityRecord{{Kind: "SCOPE.Operation", Name: "on_get", SourceFile: "a.py"}}
	fw := []types.EntityRecord{
		cfFramework("Route", "on_get", "a.py"),
		cfFramework("Service", "app", "a.py"),
		cfFramework("Config", "App()", "a.py"),
	}
	out, n := FoldFrameworkClassKinds(recs, fw)
	if n != 0 {
		t.Errorf("folded %d records, want 0", n)
	}
	cfAssertRows(t, cfRows(out), []string{
		"Config|App()||a.py", "Route|on_get||a.py",
		"SCOPE.Operation|on_get||a.py", "Service|app||a.py",
	})
}

// TestFoldFrameworkClassKinds_NonClassRecordsUntouched: an operation, a file
// component and an import placeholder are not class representations and must
// survive verbatim even when a framework record shares their name.
func TestFoldFrameworkClassKinds_NonClassRecordsUntouched(t *testing.T) {
	recs := []types.EntityRecord{
		{Kind: "SCOPE.Operation", Name: "Probe", SourceFile: "a.py"},
		{Kind: "SCOPE.Component", Subtype: "file", Name: "Probe", SourceFile: "a.py"},
		{Kind: "SCOPE.Component", Subtype: "import", Name: "Probe", SourceFile: "a.py"},
	}
	before := cfRows(recs)
	out, n := FoldFrameworkClassKinds(recs, []types.EntityRecord{cfFramework("Controller", "Probe", "a.py")})
	if n != 0 {
		t.Errorf("folded %d records, want 0", n)
	}
	cfAssertRows(t, cfRows(out), append(before, "Controller|Probe||a.py"))
}

// TestFoldFrameworkClassKinds_CanonRankBreaksEqualPriority reproduces the
// #3172/#3195 double-emission: one class surfacing as BOTH Model and
// Controller, equal priority. The structural-declaration kind must win, and
// exactly ONE node must remain for the class — the loser is folded away, not
// left standing beside the winner.
func TestFoldFrameworkClassKinds_CanonRankBreaksEqualPriority(t *testing.T) {
	for _, order := range [][]types.EntityRecord{
		{cfFramework("Controller", "Probe", "a.py"), cfFramework("Model", "Probe", "a.py")},
		{cfFramework("Model", "Probe", "a.py"), cfFramework("Controller", "Probe", "a.py")},
	} {
		out, _ := FoldFrameworkClassKinds([]types.EntityRecord{cfSource("Probe", "a.py")}, order)
		cfAssertRows(t, cfRows(out), []string{"Model|Probe||a.py"})
	}
}

// TestFoldFrameworkClassKinds_PriorityBeatsRank guards the ordering of the two
// tiebreakers: priority is primary, canonical rank only breaks an exact tie.
// "TestClass" (priority 90, rank 0) must lose to "Controller" (priority 100,
// rank 0) — and "Task" (priority 70) to "Schema" (priority 80, rank 3).
func TestFoldFrameworkClassKinds_PriorityBeatsRank(t *testing.T) {
	out, _ := FoldFrameworkClassKinds([]types.EntityRecord{cfSource("Probe", "a.py")},
		[]types.EntityRecord{
			cfFramework("TestClass", "Probe", "a.py"),
			cfFramework("Controller", "Probe", "a.py"),
		})
	cfAssertRows(t, cfRows(out), []string{"Controller|Probe||a.py"})

	out, _ = FoldFrameworkClassKinds([]types.EntityRecord{cfSource("Probe", "a.py")},
		[]types.EntityRecord{
			cfFramework("Task", "Probe", "a.py"),
			cfFramework("Schema", "Probe", "a.py"),
		})
	cfAssertRows(t, cfRows(out), []string{"Schema|Probe||a.py"})
}

// TestFoldFrameworkClassKinds_StartLineBreaksEqualKind exercises the third
// tiebreak, which nothing else reaches: candidates of the SAME kind (so
// priority and rank tie) where one carries no line at all. A line-less record is
// the "no coordinates" case the #1613 fold was written for — it must lose to the
// one that points at real source, whichever order they arrive in.
func TestFoldFrameworkClassKinds_StartLineBreaksEqualKind(t *testing.T) {
	mk := func(qn string, line int) types.EntityRecord {
		r := cfFramework("Controller", "Probe", "a.py")
		r.QualifiedName, r.StartLine = qn, line
		return r
	}
	for _, order := range [][]types.EntityRecord{
		{mk("lineless", 0), mk("real", 12), mk("later", 40)},
		{mk("later", 40), mk("real", 12), mk("lineless", 0)},
		{mk("real", 12), mk("later", 40), mk("lineless", 0)},
	} {
		out, _ := FoldFrameworkClassKinds(nil, order)
		if len(out) != 1 {
			t.Fatalf("want exactly one survivor, got %v", cfRows(out))
		}
		if out[0].QualifiedName != "real" || out[0].StartLine != 12 {
			t.Errorf("survivor should be the smallest REAL start line: got qn=%q line=%d",
				out[0].QualifiedName, out[0].StartLine)
		}
	}
}

// TestFoldFrameworkClassKinds_EdgesTheFoldedRecordOwnsMoveToSurvivor: the fold
// must never drop an edge. Owned edges keep an empty FromID, which the
// record→graph seam resolves to the OWNING record's id — so carrying them onto
// the survivor is what re-homes them.
func TestFoldFrameworkClassKinds_EdgesTheFoldedRecordOwnsMoveToSurvivor(t *testing.T) {
	src := cfSource("Probe", "a.py")
	src.Relationships = []types.RelationshipRecord{{ToID: "Probe.m", Kind: "CONTAINS"}}
	fwRec := cfFramework("Controller", "Probe", "a.py")
	fwRec.Relationships = []types.RelationshipRecord{{ToID: "Other", Kind: "MOUNTS"}}
	// A losing sibling's edges must survive the sibling fold too.
	loser := cfFramework("TestClass", "Probe", "a.py")
	loser.Relationships = []types.RelationshipRecord{{ToID: "Suite", Kind: "TESTS"}}

	out, _ := FoldFrameworkClassKinds([]types.EntityRecord{src}, []types.EntityRecord{fwRec, loser})
	if len(out) != 1 {
		t.Fatalf("want one surviving record, got %v", cfRows(out))
	}
	var kinds []string
	for _, r := range out[0].Relationships {
		if r.FromID != "" {
			t.Errorf("owned edge gained an explicit FromID %q — it would no longer bind to the survivor", r.FromID)
		}
		kinds = append(kinds, r.Kind+"→"+r.ToID)
	}
	sort.Strings(kinds)
	cfAssertRows(t, kinds, []string{"CONTAINS→Probe.m", "MOUNTS→Other", "TESTS→Suite"})
}

// TestFoldFrameworkClassKinds_BlankKeyFieldsNeverPair pins the guard the
// docstring claims rather than leaving it asserted-but-unenforced: a record with
// no SourceFile (or no Name) is not keyable. Two unrelated records both keyed
// {"",""} would otherwise collapse into one.
func TestFoldFrameworkClassKinds_BlankKeyFieldsNeverPair(t *testing.T) {
	fw := []types.EntityRecord{
		{Kind: "Controller", Name: "Probe", SourceFile: ""},
		{Kind: "Controller", Name: "", SourceFile: ""},
	}
	out, n := FoldFrameworkClassKinds(
		[]types.EntityRecord{cfSource("Probe", ""), cfSource("", "")}, fw)
	if n != 0 {
		t.Errorf("folded %d records with blank key fields, want 0", n)
	}
	cfAssertRows(t, cfRows(out), []string{
		"Controller|Probe||", "Controller|||",
		"SCOPE.Component|Probe|class|", "SCOPE.Component||class|",
	})
}

// TestFoldFrameworkClassKinds_NoInputsIsANoOp covers the guard clauses: no
// records at all must not panic, and either side alone must pass through.
func TestFoldFrameworkClassKinds_NoInputsIsANoOp(t *testing.T) {
	out, n := FoldFrameworkClassKinds(nil, nil)
	if n != 0 || len(out) != 0 {
		t.Errorf("empty input produced folded=%d out=%v", n, cfRows(out))
	}
	out, n = FoldFrameworkClassKinds([]types.EntityRecord{cfSource("Probe", "a.py")}, nil)
	if n != 0 {
		t.Errorf("folded %d with no framework records", n)
	}
	cfAssertRows(t, cfRows(out), []string{"SCOPE.Component|Probe|class|a.py"})
}

// TestFoldFrameworkClassKinds_CandidateThatIsAlsoAFoldSourceKeepsItself covers
// the self-fold guard, which is reachable in exactly one shape: a
// class-hierarchy SHADOW carrying an eligible framework kind is BOTH a survivor
// candidate and a fold source. Without the guard it would be looked up, find
// itself, and donate its own edges to itself — duplicating them.
func TestFoldFrameworkClassKinds_CandidateThatIsAlsoAFoldSourceKeepsItself(t *testing.T) {
	self := types.EntityRecord{
		Kind: "Service", Name: "Probe", SourceFile: "a.py", StartLine: 4,
		Properties:    map[string]string{"provenance": "INFERRED_FROM_CLASS_HIERARCHY"},
		Relationships: []types.RelationshipRecord{{ToID: "Probe.m", Kind: "CONTAINS"}},
	}
	out, n := FoldFrameworkClassKinds([]types.EntityRecord{self}, nil)
	if n != 0 {
		t.Errorf("a record folded into itself: folded=%d", n)
	}
	cfAssertRows(t, cfRows(out), []string{"Service|Probe||a.py"})
	if len(out) != 1 || len(out[0].Relationships) != 1 {
		t.Errorf("owned edges duplicated by a self-fold: %+v", out)
	}
}
