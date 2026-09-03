package fbwriter

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// derived_kinds_6773_test.go — #6773, the DERIVED half of arm C's counter.
//
// COMMIT_COUPLED was 27,407 of the 27,645 edges this counter reported as
// absent from the vocabulary — 99.1% from a single emit site. #6773 declared
// it, in a separate derived class, so the counter must stop calling it
// unknown. What it must NOT do is make the population disappear: silencing a
// metric by declaring away its largest term is the defect shape #6534 and
// #6536 were both filed for. So the derived edges move to their OWN count,
// which these tests require to be non-zero and visible on the sidecar.

func TestDerivedKindsAreNotCountedAsAbsentFromTheVocabulary(t *testing.T) {
	commitCoupled := string(types.RelationshipKindCommitCoupled)

	// Positive controls: the fixture is only meaningful if these three
	// classifications really differ.
	if !types.IsDerivedRelationshipKind(commitCoupled) {
		t.Fatalf("fixture is inert: %q is expected to be DERIVED", commitCoupled)
	}
	if types.IsValidRelationshipKind(commitCoupled) {
		t.Fatalf("fixture is inert: %q must not be in the structural vocabulary", commitCoupled)
	}
	if !types.IsValidRelationshipKind("CALLS") {
		t.Fatal("fixture is inert: CALLS is expected to be structural")
	}
	if types.IsDeclaredRelationshipKind("OWNS") {
		t.Fatal("fixture is inert: OWNS is expected to be in NEITHER vocabulary")
	}

	doc := &graph.Document{
		Relationships: []graph.Relationship{
			relFixture("CALLS", "a", "b"),
			relFixture(commitCoupled, "a", "c"),
			relFixture(commitCoupled, "a", "d"),
			relFixture(commitCoupled, "a", "e"),
			relFixture("OWNS", "a", "f"),
		},
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}

	// The unknown population: OWNS only. Before #6773 this was 4.
	if rep.Edges != 1 || rep.DistinctKinds != 1 {
		t.Errorf("Edges/DistinctKinds = %d/%d, want 1/1 (OWNS alone); report = %+v",
			rep.Edges, rep.DistinctKinds, rep)
	}
	for _, name := range rep.KindNames() {
		if name == commitCoupled {
			t.Errorf("%q is still reported as absent from the vocabulary; #6773 declared it "+
				"(derived), and this counter must use the same definition of declared", name)
		}
	}

	// The derived population: visible, named, and counted — not silenced.
	if rep.DerivedEdges != 3 || rep.DerivedDistinctKinds != 1 {
		t.Errorf("DerivedEdges/DerivedDistinctKinds = %d/%d, want 3/1; the derived edges must stay "+
			"visible after being declared, or declaring them was just deleting the measurement",
			rep.DerivedEdges, rep.DerivedDistinctKinds)
	}
	got := map[string]int{}
	for _, k := range rep.DerivedKinds {
		got[k.Kind] = k.Edges
	}
	if got[commitCoupled] != 3 {
		t.Errorf("DerivedKinds[%q] = %d, want 3 (report: %+v)", commitCoupled, got[commitCoupled],
			rep.DerivedKinds)
	}
	// The over-firing controls, both directions: the derived tally must not
	// swallow the structural edges, and it must not swallow the unknown ones
	// either — otherwise the unknown count reads 0 for the wrong reason.
	for _, wrong := range []string{"CALLS", "OWNS"} {
		if _, bad := got[wrong]; bad {
			t.Errorf("%q was counted as DERIVED; the derived tally is counting kinds outside "+
				"AllDerivedRelationshipKinds()", wrong)
		}
	}
}

// TestDerivedEdgesDoNotMakeAGraphUnclean pins the classification's
// consequence: Clean() is about kinds the vocabulary does not know, and after
// #6773 a graph of nothing but COMMIT_COUPLED is a graph of declared edges.
func TestDerivedEdgesDoNotMakeAGraphUnclean(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			relFixture(string(types.RelationshipKindCommitCoupled), "a", "b"),
			relFixture("CALLS", "a", "c"),
		},
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}
	if !rep.Clean() {
		t.Errorf("Clean() = false for a graph whose only non-structural kind is declared derived "+
			"(report: %+v)", rep)
	}
	if rep.DerivedEdges != 1 {
		t.Errorf("DerivedEdges = %d, want 1 — clean must not mean uncounted", rep.DerivedEdges)
	}
	// A clean graph still has something to say, and it says it about the
	// derived population specifically.
	sum := rep.Summary()
	if !strings.Contains(sum, string(types.RelationshipKindCommitCoupled)) {
		t.Errorf("Summary() = %q, does not name the derived population at all", sum)
	}
	if strings.Contains(sum, "CALLS") {
		t.Errorf("Summary() = %q names the structural kind CALLS", sum)
	}
}

// TestSummaryReportsBothPopulationsSeparately scopes its assertions to the
// clause each population owns: a whole-string grep for "COMMIT_COUPLED" would
// survive the two counts being merged back into one number, which is the
// failure this issue exists to prevent.
func TestSummaryReportsBothPopulationsSeparately(t *testing.T) {
	// MULTI-KIND on both sides on purpose: with one kind per clause, a
	// separator equal to the intra-clause list separator (", ") still splits
	// the string in exactly the right place, and the whole point of having a
	// dedicated separator — that the two clauses stay unambiguously
	// separable — goes unobserved.
	rep := NonEnumKindReport{
		Scanned:              true,
		Edges:                238,
		DistinctKinds:        5,
		Kinds:                []NonEnumKind{{Kind: "STEP_IN_PROCESS", Edges: 175}, {Kind: "ENTRY_POINT_OF", Edges: 55}},
		DerivedEdges:         27416,
		DerivedDistinctKinds: 2,
		DerivedKinds:         []NonEnumKind{{Kind: "COMMIT_COUPLED", Edges: 27407}, {Kind: "CO_CHANGED", Edges: 9}},
	}
	sum := rep.Summary()

	// The CONSEQUENCE, asserted before anything splits on it: the separator
	// occurs exactly once in a summary whose clauses each list several kinds,
	// so "cut at the separator" yields the two clauses and not a fragment of
	// one. A separator of ", " renders a summary this check fails.
	if n := strings.Count(sum, DerivedSummarySeparator); n != 1 {
		t.Fatalf("the clause separator %q occurs %d times in a two-clause, four-kind summary; a "+
			"consumer cutting on it cannot recover the two populations. Summary() = %q",
			DerivedSummarySeparator, n, sum)
	}
	head, tail, split := strings.Cut(sum, DerivedSummarySeparator)
	if !split {
		t.Fatalf("Summary() = %q, want the unknown and derived populations in separate clauses "+
			"joined by %q", sum, DerivedSummarySeparator)
	}
	// And the split lands on a clause boundary: each half is a whole clause,
	// with its own edge total and every one of its own kind names.
	for _, want := range []string{"STEP_IN_PROCESS", "ENTRY_POINT_OF"} {
		if !strings.Contains(head, want) {
			t.Errorf("unknown clause = %q, lost kind %q — the cut landed inside the clause", head, want)
		}
	}
	for _, want := range []string{"COMMIT_COUPLED", "CO_CHANGED"} {
		if !strings.Contains(tail, want) {
			t.Errorf("derived clause = %q, lost kind %q — the cut landed inside the clause", tail, want)
		}
	}
	// The unknown clause: its own totals, and no derived kind in it.
	if !strings.Contains(head, "238") || !strings.Contains(head, "5") {
		t.Errorf("unknown clause = %q, want the 238/5 totals", head)
	}
	if strings.Contains(head, "COMMIT_COUPLED") || strings.Contains(head, "27416") {
		t.Errorf("unknown clause = %q, has absorbed the derived population", head)
	}
	// The derived clause: its own totals, named, and no unknown kind in it.
	if !strings.Contains(tail, "27416") {
		t.Errorf("derived clause = %q, want the derived edge total 27416", tail)
	}
	if !strings.Contains(tail, "COMMIT_COUPLED") {
		t.Errorf("derived clause = %q, does not name the derived kind", tail)
	}
	if strings.Contains(tail, "STEP_IN_PROCESS") {
		t.Errorf("derived clause = %q, has absorbed the unknown population", tail)
	}
}

// TestDerivedListIsCappedButItsCountsAreNot mirrors the asymmetry the
// non-enum list already holds: truncating the NAME list is a size guard,
// truncating the totals would cap the report in the dimension it exists to
// measure.
func TestDerivedListIsCappedButItsCountsAreNot(t *testing.T) {
	tally := &nonEnumKindTally{}
	const distinct = NonEnumKindListCap + 7
	for i := 0; i < distinct; i++ {
		tally.derivedEdges++
		if tally.derivedCounts == nil {
			tally.derivedCounts = map[string]int{}
		}
		tally.derivedCounts[fmt.Sprintf("DERIVED_%02d", i)] = i + 1
		tally.derivedEdges += i
	}
	rep := tally.report()
	if rep.DerivedDistinctKinds != distinct {
		t.Errorf("DerivedDistinctKinds = %d, want %d (uncapped)", rep.DerivedDistinctKinds, distinct)
	}
	if len(rep.DerivedKinds) != NonEnumKindListCap {
		t.Errorf("len(DerivedKinds) = %d, want the cap %d", len(rep.DerivedKinds), NonEnumKindListCap)
	}
	// Busiest first, so the truncation drops the least interesting names.
	if rep.DerivedKinds[0].Edges < rep.DerivedKinds[len(rep.DerivedKinds)-1].Edges {
		t.Errorf("DerivedKinds is not sorted busiest-first: %+v", rep.DerivedKinds)
	}
}

// TestApplyToSidecarCarriesTheDerivedPopulation is the durability half: the
// stderr line is invisible to MCP, the dashboard and `doctor`, which read the
// graph, so the derived counts have to reach graph-stats.json.
func TestApplyToSidecarCarriesTheDerivedPopulation(t *testing.T) {
	rep := NonEnumKindReport{
		Scanned:              true,
		Edges:                238,
		DistinctKinds:        5,
		Kinds:                []NonEnumKind{{Kind: "STEP_IN_PROCESS", Edges: 175}},
		DerivedEdges:         27407,
		DerivedDistinctKinds: 1,
		DerivedKinds:         []NonEnumKind{{Kind: "COMMIT_COUPLED", Edges: 27407}},
	}
	var side graph.GraphStatsSidecar
	rep.ApplyToSidecar(&side)

	if side.RelationshipEdgesDerivedKind != 27407 {
		t.Errorf("RelationshipEdgesDerivedKind = %d, want 27407", side.RelationshipEdgesDerivedKind)
	}
	if side.RelationshipDistinctDerivedKinds != 1 {
		t.Errorf("RelationshipDistinctDerivedKinds = %d, want 1", side.RelationshipDistinctDerivedKinds)
	}
	if got := side.RelationshipDerivedKinds["COMMIT_COUPLED"]; got != 27407 {
		t.Errorf("RelationshipDerivedKinds[COMMIT_COUPLED] = %d, want 27407 (map: %v)",
			got, side.RelationshipDerivedKinds)
	}
	// The two populations stay separate on the sidecar too.
	if side.RelationshipEdgesKindNotInEnum != 238 {
		t.Errorf("RelationshipEdgesKindNotInEnum = %d, want 238", side.RelationshipEdgesKindNotInEnum)
	}
	if _, bad := side.RelationshipKindsNotInEnum["COMMIT_COUPLED"]; bad {
		t.Error("COMMIT_COUPLED landed in RelationshipKindsNotInEnum")
	}
	if _, bad := side.RelationshipDerivedKinds["STEP_IN_PROCESS"]; bad {
		t.Error("STEP_IN_PROCESS landed in RelationshipDerivedKinds")
	}

	// The uncapped-counts rule, on the derived side: a report whose name list
	// was truncated must still state the true distinct total.
	truncated := NonEnumKindReport{
		Scanned:              true,
		DerivedEdges:         900,
		DerivedDistinctKinds: 40,
		DerivedKinds:         []NonEnumKind{{Kind: "A", Edges: 5}, {Kind: "B", Edges: 4}},
	}
	var side2 graph.GraphStatsSidecar
	truncated.ApplyToSidecar(&side2)
	if side2.RelationshipDistinctDerivedKinds != 40 {
		t.Errorf("RelationshipDistinctDerivedKinds = %d, want the uncapped 40, not len(list)=%d",
			side2.RelationshipDistinctDerivedKinds, len(truncated.DerivedKinds))
	}
}

// TestWritePathClassificationMatchesTheTypesPredicates is what makes "the same
// definition of declared" a fact rather than a coincidence (#6773 review D3).
//
// This counter cannot call types.IsDeclaredRelationshipKind per edge — it is a
// linear scan over both vocabularies, on a hot path — so it builds its own
// lookup sets. Nothing forced those sets to agree with the predicates: the
// prose said they did, and mutating IsDeclaredRelationshipKind to ignore the
// derived vocabulary left this package entirely green.
//
// So the write path's verdict is asserted AGAINST the predicates, for every
// kind in both vocabularies plus kinds in neither, in one marshal.
func TestWritePathClassificationMatchesTheTypesPredicates(t *testing.T) {
	var kinds []string
	for _, k := range types.AllRelationshipKinds() {
		kinds = append(kinds, string(k))
	}
	for _, k := range types.AllDerivedRelationshipKinds() {
		kinds = append(kinds, string(k))
	}
	// Kinds in NEITHER vocabulary: without them the agreement could be
	// satisfied by a write path that classifies everything as declared.
	unknown := []string{"STEP_IN_PROCESS", "ENTRY_POINT_OF", "NOT_A_KIND_AT_ALL"}
	for _, k := range unknown {
		if types.IsDeclaredRelationshipKind(k) {
			t.Fatalf("fixture is inert: %q is expected to be in NEITHER vocabulary", k)
		}
	}
	kinds = append(kinds, unknown...)

	doc := &graph.Document{}
	for i, k := range kinds {
		doc.Relationships = append(doc.Relationships, relFixture(k, "a", fmt.Sprintf("b%d", i)))
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}

	countedUnknown := map[string]bool{}
	for _, k := range rep.Kinds {
		countedUnknown[k.Kind] = true
	}
	countedDerived := map[string]bool{}
	for _, k := range rep.DerivedKinds {
		countedDerived[k.Kind] = true
	}
	// Both lists are capped, so a corpus larger than the cap would make the
	// per-kind check unreliable in the truncated tail.
	if rep.DistinctKinds > NonEnumKindListCap || rep.DerivedDistinctKinds > NonEnumKindListCap {
		t.Fatalf("fixture exceeds the report's name cap (%d unknown / %d derived vs cap %d); the "+
			"per-kind assertions below would read a truncated list",
			rep.DistinctKinds, rep.DerivedDistinctKinds, NonEnumKindListCap)
	}

	for _, k := range kinds {
		wantUnknown := !types.IsDeclaredRelationshipKind(k)
		if countedUnknown[k] != wantUnknown {
			t.Errorf("write path counted %q as not-in-vocabulary = %v, but "+
				"types.IsDeclaredRelationshipKind says declared = %v. The counter's sets and the "+
				"types predicates have drifted: they are two spellings of one definition and only "+
				"this test observes that.", k, countedUnknown[k], !wantUnknown)
		}
		if countedDerived[k] != types.IsDerivedRelationshipKind(k) {
			t.Errorf("write path counted %q as derived = %v, but types.IsDerivedRelationshipKind "+
				"says %v", k, countedDerived[k], types.IsDerivedRelationshipKind(k))
		}
	}
}
