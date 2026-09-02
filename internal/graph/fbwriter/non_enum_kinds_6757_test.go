package fbwriter

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6757 arm C. The relationship-kind vocabulary is enforced by nothing:
// IsValidRelationshipKind had zero non-test callers, so any string could be
// written to the graph as a relationship kind. Arm B's STATIC ledger proved 22
// kinds absent from the enum exist in source, but 87 relationship-kind fields repo-wide
// resolve to a RUNTIME value, so 22 is a floor and no static scan can see the
// rest.
//
// The arm C contract, in the shape of readDirBounded (daemon/state_path.go):
// the write path cannot reject (buildRelationship is per-edge, hot, and
// returns no error) — but it must not silently forget either. It TALLIES what
// it cannot validate and ADMITS it, naming the distinct kinds.
//
// These tests pin, in order:
//   - only ABSENT FROM THE ENUM kinds are counted (the permissive direction: a counter
//     that tallies every edge is useless and our suites are structurally
//     blind to it);
//   - the count is non-zero when kinds absent from the enum are written (a counter
//     hard-wired to 0 reports "clean" the way #6534 did);
//   - the distinct NAMES are surfaced, not just a total;
//   - the list is capped while the counts are not (the internal/secrets
//     ScanResult.Unread shape from #6752);
//   - both real producer paths — flat and segmented — are wired.

// relFixture builds a relationship with the given kind.
func relFixture(kind, from, to string) graph.Relationship {
	return graph.Relationship{FromID: from, ToID: to, Kind: kind}
}

func TestStreamingWriterTalliesOnlyRelationshipKindsAbsentFromTheEnum(t *testing.T) {
	// Positive control: these MUST be declared, or the fixture proves nothing.
	for _, declared := range []string{
		string(types.RelationshipKindCalls),
		string(types.RelationshipKindContains),
	} {
		if !types.IsValidRelationshipKind(declared) {
			t.Fatalf("fixture is inert: %q is expected to be IN the enum but IsValidRelationshipKind says otherwise", declared)
		}
	}
	// And these must NOT be, or "undeclared" is meaningless here.
	for _, undeclared := range []string{"OWNS", "INDEXES"} {
		if types.IsValidRelationshipKind(undeclared) {
			t.Fatalf("fixture is inert: %q is expected to be ABSENT FROM THE ENUM but IsValidRelationshipKind accepts it", undeclared)
		}
	}

	doc := &graph.Document{
		Relationships: []graph.Relationship{
			relFixture("CALLS", "a", "b"),
			relFixture("CALLS", "a", "c"),
			relFixture("CONTAINS", "a", "d"),
			relFixture("OWNS", "a", "e"),
			relFixture("OWNS", "a", "f"),
			relFixture("INDEXES", "a", "g"),
		},
	}

	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}

	// 6 relationships written, only 3 of them undeclared. A counter that
	// counted EVERY relationship would say 6 here.
	if rep.Edges != 3 {
		t.Errorf("Edges = %d, want 3 (6 relationships written, 3 with an undeclared kind)", rep.Edges)
	}
	if rep.DistinctKinds != 2 {
		t.Errorf("DistinctKinds = %d, want 2 (OWNS, INDEXES)", rep.DistinctKinds)
	}
	if rep.Clean() {
		t.Error("Clean() = true, but 3 undeclared edges were written")
	}

	got := map[string]int{}
	for _, k := range rep.Kinds {
		got[k.Kind] = k.Edges
	}
	if got["OWNS"] != 2 {
		t.Errorf("OWNS edges = %d, want 2 (report: %+v)", got["OWNS"], rep.Kinds)
	}
	if got["INDEXES"] != 1 {
		t.Errorf("INDEXES edges = %d, want 1 (report: %+v)", got["INDEXES"], rep.Kinds)
	}
	for _, declared := range []string{"CALLS", "CONTAINS"} {
		if _, bad := got[declared]; bad {
			t.Errorf("enum kind %q was reported as non-enum — the counter is counting every relationship, not only the undeclared ones", declared)
		}
	}

	// The names, not just the total: a bare count says something is wrong,
	// the names say what.
	sum := rep.Summary()
	for _, want := range []string{"OWNS", "INDEXES"} {
		if !strings.Contains(sum, want) {
			t.Errorf("Summary() = %q, missing non-enum kind name %q", sum, want)
		}
	}
	if strings.Contains(sum, "CALLS") {
		t.Errorf("Summary() = %q, names the enum kind CALLS", sum)
	}
}

func TestNonEnumKindReportIsEmptyButSCANNEDForAnAllEnumGraph(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			relFixture("CALLS", "a", "b"),
			relFixture("CONTAINS", "a", "c"),
		},
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}
	if rep.Edges != 0 || rep.DistinctKinds != 0 || len(rep.Kinds) != 0 {
		t.Fatalf("all-enum graph reported non-enum kinds: %+v", rep)
	}
	// SCANNED and empty — not the same as never scanned. Clean() must require
	// both, or "counted zero" and "never counted" collapse into one state and
	// this arm reports exactly the false all-clear it exists to prevent.
	if !rep.Scanned {
		t.Error("a graph that WAS serialized reported Scanned=false")
	}
	if !rep.Clean() {
		t.Error("Clean() = false for a scanned graph with zero non-enum edges")
	}
	var never NonEnumKindReport
	if never.Clean() {
		t.Error("Clean() = true for a report no write path ever produced — " +
			"\"counted zero\" and \"never counted\" must not be the same answer (#6534)")
	}
	if never.Scanned {
		t.Error("a zero-valued report claims Scanned")
	}
	// Same for the tally's own no-observer case: a nil tally is the one the
	// discarded probe uses, and it must report "never counted", not "clean".
	var noTally *nonEnumKindTally
	if noTally.report().Scanned {
		t.Error("a nil tally — one that observed nothing at all — reports Scanned=true")
	}
	if noTally.report().Clean() {
		t.Error("a nil tally reports Clean(); nothing counted is not the same as counted zero")
	}
	if rep.Summary() != "" {
		t.Errorf("Summary() = %q, want empty for a clean graph", rep.Summary())
	}
}

func TestNonEnumKindReportCapsTheListButNotTheCounts(t *testing.T) {
	// Follows internal/secrets ScanResult.Unread (#6752): the list is capped
	// so a pathological graph cannot flood a summary line, but the totals are
	// never capped, so the report stays honest about the size of the problem.
	const distinct = NonEnumKindListCap + 11
	doc := &graph.Document{}
	for i := 0; i < distinct; i++ {
		kind := fmt.Sprintf("ZZ_NOT_IN_ENUM_%03d", i)
		if types.IsValidRelationshipKind(kind) {
			t.Fatalf("fixture is inert: %q is actually declared", kind)
		}
		doc.Relationships = append(doc.Relationships, relFixture(kind, "a", fmt.Sprintf("b%d", i)))
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}
	if rep.Edges != distinct {
		t.Errorf("Edges = %d, want %d — the total must NOT be capped", rep.Edges, distinct)
	}
	if rep.DistinctKinds != distinct {
		t.Errorf("DistinctKinds = %d, want %d — the distinct count must NOT be capped", rep.DistinctKinds, distinct)
	}
	if len(rep.Kinds) != NonEnumKindListCap {
		t.Errorf("len(Kinds) = %d, want the cap %d", len(rep.Kinds), NonEnumKindListCap)
	}
	if !strings.Contains(rep.Summary(), "more") {
		t.Errorf("Summary() = %q — a truncated list must say so", rep.Summary())
	}
}

func TestWriteGraphGenReportWiresTheFlatProducerPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "0")
	doc := &graph.Document{
		Entities: []graph.Entity{{ID: "a", Kind: "SCOPE.Module"}},
		Relationships: []graph.Relationship{
			relFixture("CALLS", "a", "b"),
			relFixture("OWNS", "a", "c"),
		},
	}
	genPath, rep, err := WriteGraphGenReport(dir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGenReport: %v", err)
	}
	if genPath == "" {
		t.Fatal("fixture is inert: no gen path written, so no relationship was serialized")
	}
	if rep.Edges != 1 || rep.DistinctKinds != 1 || len(rep.Kinds) != 1 || rep.Kinds[0].Kind != "OWNS" {
		t.Fatalf("flat producer path did not report the non-enum kind: %+v", rep)
	}
}

func TestWriteGraphGenReportWiresTheSegmentedProducerPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	// A tiny threshold forces the real multi-segment path (writeSegments)
	// rather than the single-file fast path, so this covers the segment loop
	// AND proves the bounded probe (graphFitsSingleBuilder) does not
	// double-count the relationships it walks before being discarded.
	t.Setenv("GRAFEL_SEGMENT_BYTES", "512")
	// Few, small entities on purpose: the probe must get PAST the entity loop
	// and into the relationship loop before it crosses the threshold, or it
	// never walks a relationship and the double-count assertion below is
	// vacuous. (Verified: with a 40-entity fixture the probe bails in the
	// entity loop and a shared-tally mutant survives.)
	doc := &graph.Document{}
	for i := 0; i < 2; i++ {
		doc.Entities = append(doc.Entities, graph.Entity{ID: fmt.Sprintf("e%02d", i), Kind: "SCOPE.Module"})
	}
	// The ABSENT FROM THE ENUM edges sort FIRST (writeGraphGenSegmented sorts by
	// from,to,kind), so they are the ones the probe walks before it bails.
	// Ordering them last would let a shared-tally mutant survive: the probe
	// would only ever double-count declared edges, which are not tallied.
	for i := 0; i < 20; i++ {
		doc.Relationships = append(doc.Relationships, relFixture("OWNS", "a", fmt.Sprintf("a%02d", i)))
		doc.Relationships = append(doc.Relationships, relFixture("CALLS", "a", fmt.Sprintf("z%02d", i)))
	}
	genPath, rep, err := WriteGraphGenReport(dir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGenReport: %v", err)
	}
	if genPath == "" {
		t.Fatal("fixture is inert: nothing was written")
	}
	if rep.DistinctKinds != 1 || len(rep.Kinds) != 1 || rep.Kinds[0].Kind != "OWNS" {
		t.Fatalf("segmented producer path did not report the non-enum kind: %+v", rep)
	}
	// Exactly 20 — not 40. A double count would mean the discarded probe
	// builder's edges were tallied alongside the real write.
	if rep.Edges != 20 {
		t.Fatalf("Edges = %d, want exactly 20 — the discarded bounded probe must not be tallied "+
			"(sharing one tally between the probe and the real write reports 30 here)", rep.Edges)
	}
}

func TestNonEnumKindsAreCountedNotDropped(t *testing.T) {
	// Arm C counts and reports; it must NOT drop. Dropping would be the same
	// "looked at nothing, reported clean" failure #6534 just fixed elsewhere.
	dir := t.TempDir()
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "0")
	doc := &graph.Document{
		Entities: []graph.Entity{{ID: "a", Kind: "SCOPE.Module"}, {ID: "c", Kind: "SCOPE.Module"}},
		Relationships: []graph.Relationship{
			relFixture("CALLS", "a", "c"),
			relFixture("OWNS", "a", "c"),
			relFixture("REGISTERED_ON", "a", "c"),
		},
	}
	if _, _, err := WriteGraphGenReport(dir, doc); err != nil {
		t.Fatalf("WriteGraphGenReport: %v", err)
	}
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	kinds := map[string]bool{}
	for _, r := range loaded.Relationships {
		kinds[r.Kind] = true
	}
	for _, want := range []string{"CALLS", "OWNS", "REGISTERED_ON"} {
		if !kinds[want] {
			t.Errorf("relationship kind %q was DROPPED from the written graph — arm C counts, it never drops", want)
		}
	}
}
