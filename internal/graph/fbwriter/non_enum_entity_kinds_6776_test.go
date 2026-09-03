package fbwriter

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6776 arm A. #6744 froze a STATIC ledger of the entity kinds rule YAML
// declares outside types.AllEntityKinds() — 532 declaration sites, 25 invalid
// values. #6776 proposes migrating them, and warns in its own closing line
// that "532 sites is an inventory, not a measurement": #6757 learned that a
// static ledger ranked 22 relationship kinds as equals while ONE of them was
// 99.1% of the runtime population.
//
// So this arm counts ENTITY kinds at the serialization leaf, buildEntity, the
// same way arm C counts relationship kinds at buildRelationship. It cannot
// reject (per-entity, hot, returns no error); it tallies and admits.
//
// These tests pin, in order:
//   - only kinds ABSENT FROM types.AllEntityKinds() are counted, in BOTH
//     directions — a valid kind is never counted, an invalid one always is
//     (the permissive direction is the one a recall-shaped suite is blind to);
//   - a scanned-and-empty report is DISTINGUISHABLE from a never-scanned one,
//     which is the whole reason the metric is not vacuous by construction;
//   - the distinct NAMES are surfaced, since the names are what a migration is
//     ranked by;
//   - the name list is capped while the counts are not;
//   - both real producer paths — flat and segmented — are wired, and the
//     discarded bounded probe is NOT tallied;
//   - the entity half reaches graph-stats.json, under its own Scanned flag;
//   - EntitySummary is textually separable from Summary;
//   - nothing is dropped;
//   - every kind on #6744's ledger is countable by this path.

// entFixture builds an entity with the given id and kind.
func entFixture(id, kind string) graph.Entity {
	return graph.Entity{ID: id, Kind: kind, Name: id, QualifiedName: id}
}

// TestStreamingWriterTalliesOnlyEntityKindsAbsentFromTheEnum
//
// Varies: the entity KIND (two in the enum, two not).
// Holds constant: the writer (marshalWithReport), one entity per id, and the
// relationship vector (empty), so nothing but kind membership can move the
// numbers.
//
// This is the negative control. Recall alone cannot detect over-firing: a
// counter that tallied EVERY entity would report 6 here and would satisfy any
// assertion that only checks the invalid kinds are present.
func TestStreamingWriterTalliesOnlyEntityKindsAbsentFromTheEnum(t *testing.T) {
	// Positive control: these MUST be in the enum, or the fixture proves
	// nothing about the restraint direction.
	for _, valid := range []string{
		string(types.EntityKindFunction),
		string(types.EntityKindModule),
	} {
		if !types.IsValidEntityKind(valid) {
			t.Fatalf("fixture is inert: %q is expected to be IN the enum but IsValidEntityKind says otherwise", valid)
		}
	}
	// And these must NOT be, or "absent from the enum" is meaningless here.
	// Both are real #6744 ledger entries, not invented strings.
	for _, invalid := range []string{"Route", "Config"} {
		if types.IsValidEntityKind(invalid) {
			t.Fatalf("fixture is inert: %q is expected to be ABSENT FROM THE ENUM but IsValidEntityKind accepts it", invalid)
		}
	}

	doc := &graph.Document{
		Entities: []graph.Entity{
			entFixture("a", string(types.EntityKindFunction)),
			entFixture("b", string(types.EntityKindFunction)),
			entFixture("c", string(types.EntityKindModule)),
			entFixture("d", "Route"),
			entFixture("e", "Route"),
			entFixture("f", "Config"),
		},
	}

	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}

	// 6 entities written, only 3 of them outside the enum. A counter that
	// counted EVERY entity would say 6 here.
	if rep.Entities != 3 {
		t.Errorf("Entities = %d, want 3 (6 entities written, 3 with a kind absent from the enum)", rep.Entities)
	}
	if rep.EntityDistinctKinds != 2 {
		t.Errorf("EntityDistinctKinds = %d, want 2 (Route, Config)", rep.EntityDistinctKinds)
	}
	if rep.EntityKindsClean() {
		t.Error("EntityKindsClean() = true, but 3 entities with non-enum kinds were written")
	}

	got := map[string]int{}
	for _, k := range rep.EntityKinds {
		got[k.Kind] = k.Entities
	}
	if got["Route"] != 2 {
		t.Errorf("Route entities = %d, want 2 (report: %+v)", got["Route"], rep.EntityKinds)
	}
	if got["Config"] != 1 {
		t.Errorf("Config entities = %d, want 1 (report: %+v)", got["Config"], rep.EntityKinds)
	}
	for _, valid := range []string{string(types.EntityKindFunction), string(types.EntityKindModule)} {
		if _, bad := got[valid]; bad {
			t.Errorf("enum kind %q was reported as non-enum — the counter is counting every entity, not only the ones outside the vocabulary", valid)
		}
	}

	// The names, not just the total: a bare count says something is wrong,
	// the names say what, and what is the input to the migration ranking.
	sum := rep.EntitySummary()
	for _, want := range []string{"Route", "Config"} {
		if !strings.Contains(sum, want) {
			t.Errorf("EntitySummary() = %q, missing non-enum kind name %q", sum, want)
		}
	}
	if strings.Contains(sum, string(types.EntityKindFunction)) {
		t.Errorf("EntitySummary() = %q, names the enum kind %s", sum, types.EntityKindFunction)
	}
}

// TestEntityKindReportIsEmptyButSCANNEDForAnAllEnumGraph
//
// Varies: whether a write path ran the tally at all (a real marshal, a
// zero-valued report, a nil tally).
// Holds constant: the entity kinds — every one of them is in the enum, so the
// only thing separating the three states is whether anything was counted.
//
// This is the anti-vacuity pin. Ask what this counter would report if it had
// examined nothing: zero. If that were indistinguishable from a clean result
// the metric would be worthless — #6534 was a scanner calling a repo clean
// that it had read zero bytes of.
func TestEntityKindReportIsEmptyButSCANNEDForAnAllEnumGraph(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			entFixture("a", string(types.EntityKindFunction)),
			entFixture("b", string(types.EntityKindClass)),
		},
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}
	if rep.Entities != 0 || rep.EntityDistinctKinds != 0 || len(rep.EntityKinds) != 0 {
		t.Fatalf("all-enum graph reported non-enum entity kinds: %+v", rep)
	}
	if !rep.Scanned {
		t.Error("a graph that WAS serialized reported Scanned=false")
	}
	if !rep.EntityKindsClean() {
		t.Error("EntityKindsClean() = false for a scanned graph with zero non-enum entities")
	}
	if rep.EntitySummary() != "" {
		t.Errorf("EntitySummary() = %q, want empty for a clean graph", rep.EntitySummary())
	}

	var never NonEnumKindReport
	if never.EntityKindsClean() {
		t.Error("EntityKindsClean() = true for a report no write path ever produced — " +
			"\"counted zero\" and \"never counted\" must not be the same answer (#6534)")
	}
	// Same for the tally's own no-observer case: a nil tally is the one the
	// discarded probe uses, and it must report "never counted", not "clean".
	var noTally *nonEnumKindTally
	if noTally.report().EntityKindsClean() {
		t.Error("a nil tally reports EntityKindsClean(); nothing counted is not the same as counted zero")
	}
	// And a nil tally must survive being handed an entity kind, since that is
	// exactly what graphFitsSingleBuilder does on every probed entity.
	noTally.observeEntity("Route")
	if noTally.report().Scanned {
		t.Error("a nil tally reports Scanned=true after observing an entity")
	}
}

// TestEntityKindReportCapsTheListButNotTheCounts
//
// Varies: the number of distinct non-enum entity kinds (cap + 11).
// Holds constant: exactly one entity per kind, so Entities and
// EntityDistinctKinds are the same number and a cap applied to the wrong field
// is unmistakable.
func TestEntityKindReportCapsTheListButNotTheCounts(t *testing.T) {
	const distinct = NonEnumKindListCap + 11
	doc := &graph.Document{}
	for i := 0; i < distinct; i++ {
		kind := fmt.Sprintf("ZZ_NOT_AN_ENTITY_KIND_%03d", i)
		if types.IsValidEntityKind(kind) {
			t.Fatalf("fixture is inert: %q is actually a valid entity kind", kind)
		}
		doc.Entities = append(doc.Entities, entFixture(fmt.Sprintf("e%03d", i), kind))
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}
	if rep.Entities != distinct {
		t.Errorf("Entities = %d, want %d — the total must NOT be capped", rep.Entities, distinct)
	}
	if rep.EntityDistinctKinds != distinct {
		t.Errorf("EntityDistinctKinds = %d, want %d — the distinct count must NOT be capped", rep.EntityDistinctKinds, distinct)
	}
	if len(rep.EntityKinds) != NonEnumKindListCap {
		t.Errorf("len(EntityKinds) = %d, want the cap %d", len(rep.EntityKinds), NonEnumKindListCap)
	}
	if !strings.Contains(rep.EntitySummary(), "more") {
		t.Errorf("EntitySummary() = %q — a truncated list must say so", rep.EntitySummary())
	}
}

// TestWriteGraphGenReportWiresTheFlatEntityProducerPath
//
// Varies: nothing — this is a wiring pin on the flag-OFF producer.
// Holds constant: GRAFEL_STREAM_SEGMENTS=0, so writeGraphGenFlat is the path
// under test and the segmented loop cannot be the thing that reported.
func TestWriteGraphGenReportWiresTheFlatEntityProducerPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "0")
	doc := &graph.Document{
		Entities: []graph.Entity{
			entFixture("a", string(types.EntityKindFunction)),
			entFixture("b", "Middleware"),
		},
	}
	genPath, rep, err := WriteGraphGenReport(dir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGenReport: %v", err)
	}
	if genPath == "" {
		t.Fatal("fixture is inert: no gen path written, so no entity was serialized")
	}
	if rep.Entities != 1 || rep.EntityDistinctKinds != 1 ||
		len(rep.EntityKinds) != 1 || rep.EntityKinds[0].Kind != "Middleware" {
		t.Fatalf("flat producer path did not report the non-enum entity kind: %+v", rep)
	}
}

// TestWriteGraphGenReportWiresTheSegmentedEntityProducerPath
//
// Varies: the producer path (segmented, multi-segment).
// Holds constant: the entity population — 20 entities, all with the same
// non-enum kind — so the expected count is exactly 20 and ANY contribution
// from the discarded bounded probe shows up as a number larger than 20.
//
// The probe walks the ENTITY loop first, so it is guaranteed to observe
// entities before it crosses the threshold; that is what makes the
// double-count assertion non-vacuous here (arm C had to order its
// relationships carefully for the same reason).
func TestWriteGraphGenReportWiresTheSegmentedEntityProducerPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	t.Setenv("GRAFEL_SEGMENT_BYTES", "512")
	if types.IsValidEntityKind("Controller") {
		t.Fatal("fixture is inert: Controller is a valid entity kind")
	}
	doc := &graph.Document{}
	for i := 0; i < 20; i++ {
		e := entFixture(fmt.Sprintf("e%02d", i), "Controller")
		// Padding so the 20 entities alone exceed the 512-byte threshold and
		// the write really segments (and the probe really bails mid-loop).
		e.QualifiedName = strings.Repeat("q", 120) + e.ID
		doc.Entities = append(doc.Entities, e)
	}
	genPath, rep, err := WriteGraphGenReport(dir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGenReport: %v", err)
	}
	if genPath == "" {
		t.Fatal("fixture is inert: nothing was written")
	}
	if rep.EntityDistinctKinds != 1 || len(rep.EntityKinds) != 1 || rep.EntityKinds[0].Kind != "Controller" {
		t.Fatalf("segmented producer path did not report the non-enum entity kind: %+v", rep)
	}
	// Exactly 20 — not more. A larger number means the discarded probe
	// builder's entities were tallied alongside the real write.
	if rep.Entities != 20 {
		t.Fatalf("Entities = %d, want exactly 20 — the discarded bounded probe must not be tallied "+
			"(sharing one tally between the probe and the real write over-counts here)", rep.Entities)
	}
	// And the write really took the segment loop, not the single-file fast
	// path — otherwise this test is a duplicate of the flat one above.
	if !strings.Contains(genPath, "graph.") || strings.HasSuffix(genPath, ".fb") {
		t.Fatalf("genPath = %q — expected a segment-set gen DIRECTORY; the fast path was taken, "+
			"so writeSegments' entity loop was never exercised", genPath)
	}
}

// TestEntityKindsAreCountedNotDropped
//
// Varies: nothing.
// Holds constant: the document. This pins the "counts, never drops" contract —
// dropping would be the very "looked at nothing, reported clean" failure the
// arm exists to avoid.
func TestEntityKindsAreCountedNotDropped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "0")
	doc := &graph.Document{
		Entities: []graph.Entity{
			entFixture("a", string(types.EntityKindFunction)),
			entFixture("b", "Route"),
			entFixture("c", "Schema"),
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
	for _, e := range loaded.Entities {
		kinds[e.Kind] = true
	}
	for _, want := range []string{string(types.EntityKindFunction), "Route", "Schema"} {
		if !kinds[want] {
			t.Errorf("entity kind %q was DROPPED from the written graph — arm A counts, it never drops", want)
		}
	}
}

// TestApplyToSidecarCarriesTheEntityHalf
//
// Varies: whether the report was scanned, and the relationship between the
// uncapped counts and the truncated name list.
// Holds constant: one ApplyToSidecar call per case — the sidecar's entity
// fields must come from the SAME single conversion the relationship fields do,
// so the two writers of graph-stats.json cannot drift.
func TestApplyToSidecarCarriesTheEntityHalf(t *testing.T) {
	rep := NonEnumKindReport{
		Scanned:             true,
		Entities:            17,
		EntityDistinctKinds: NonEnumKindListCap + 4,
		EntityKinds: []NonEnumEntityKind{
			{Kind: "Route", Entities: 11},
			{Kind: "Config", Entities: 6},
		},
	}
	var side graph.GraphStatsSidecar
	rep.ApplyToSidecar(&side)

	if !side.EntityKindsScanned {
		t.Error("EntityKindsScanned = false for a scanned report")
	}
	if side.EntitiesKindNotInEnum != 17 {
		t.Errorf("EntitiesKindNotInEnum = %d, want 17", side.EntitiesKindNotInEnum)
	}
	// The DISTINCT count, not len(EntityKinds): reading it off the truncated
	// list would cap the report in exactly the dimension it measures.
	if side.EntityDistinctKindsNotInEnum != NonEnumKindListCap+4 {
		t.Errorf("EntityDistinctKindsNotInEnum = %d, want %d — it must be the uncapped count, not len(EntityKinds)=%d",
			side.EntityDistinctKindsNotInEnum, NonEnumKindListCap+4, len(rep.EntityKinds))
	}
	if side.EntityKindsNotInEnum["Route"] != 11 || side.EntityKindsNotInEnum["Config"] != 6 {
		t.Errorf("EntityKindsNotInEnum = %v, want Route:11 Config:6", side.EntityKindsNotInEnum)
	}

	// An unscanned report must leave the flag false, so a consumer can tell
	// "clean" from "nobody looked" off the sidecar alone.
	var unscanned NonEnumKindReport
	var side2 graph.GraphStatsSidecar
	unscanned.ApplyToSidecar(&side2)
	if side2.EntityKindsScanned {
		t.Error("EntityKindsScanned = true for a report no write path produced")
	}
	if side2.EntitiesKindNotInEnum != 0 || side2.EntityKindsNotInEnum != nil {
		t.Errorf("unscanned report wrote entity counts: %+v", side2)
	}
}

// TestEntitySummaryIsSeparableFromTheRelationshipSummary
//
// Varies: which vector carries the non-enum kind.
// Holds constant: the report is a single value carrying both populations.
//
// An over-broad assertion fails like no assertion: cmd/grafel prints these on
// two lines under two prefixes, and a check scoped to the relationship report
// must not start matching entity text (or vice versa) if the two are ever
// merged.
func TestEntitySummaryIsSeparableFromTheRelationshipSummary(t *testing.T) {
	doc := &graph.Document{
		Entities:      []graph.Entity{entFixture("a", "Route")},
		Relationships: []graph.Relationship{relFixture("OWNS", "a", "b")},
	}
	_, rep, err := marshalWithReport(doc)
	if err != nil {
		t.Fatalf("marshalWithReport: %v", err)
	}
	relSum, entSum := rep.Summary(), rep.EntitySummary()
	if relSum == "" || entSum == "" {
		t.Fatalf("fixture is inert: Summary()=%q EntitySummary()=%q — both must be non-empty here", relSum, entSum)
	}
	if strings.Contains(relSum, "Route") {
		t.Errorf("Summary() = %q — the relationship line names the entity kind Route", relSum)
	}
	if strings.Contains(entSum, "OWNS") {
		t.Errorf("EntitySummary() = %q — the entity line names the relationship kind OWNS", entSum)
	}
	if strings.Contains(relSum, "entity(s)") {
		t.Errorf("Summary() = %q — the relationship line reports an entity count", relSum)
	}
	if strings.Contains(entSum, "relationship edge(s)") {
		t.Errorf("EntitySummary() = %q — the entity line reports a relationship count", entSum)
	}
}

// ruleDeclaredKinds6776 is #6744's ledger, transcribed. It is NOT the source of
// truth — internal/entkinds' guard is, and it fails if the population moves —
// but it is the list #6776 proposes to migrate, and the point of arm A is that
// every one of these must be COUNTABLE at the write path before anyone ranks
// the migration by declaration-site count.
var ruleDeclaredKinds6776 = []string{
	"Component", "Config", "Constraint", "Controller", "Decorator",
	"Dependency", "Endpoint", "Fixture", "Implementation", "Interface",
	"Middleware", "Migration", "Model", "Operation", "Plugin",
	"Relationship", "Route", "Schema", "Service", "Task",
	"Template", "Test", "TestClass", "TestConfig", "View",
}

// TestEveryRuleDeclaredKindOnTheLedgerIsCountedByTheWritePath
//
// Varies: the entity kind, across ALL 25 ledger entries — the name of this
// test says "every", so the body drives every one of them, individually, and
// asserts a per-kind count rather than a total that one lucky kind could
// satisfy.
// Holds constant: one entity per kind, the flat writer, and an empty
// relationship vector.
//
// This is the pin under the measurement #6776 asked for: if any ledger kind
// were silently invisible to the write path, the runtime table would report a
// zero for it that means "not measurable" rather than "not produced", and
// those are the two answers a migration ranking must never confuse.
func TestEveryRuleDeclaredKindOnTheLedgerIsCountedByTheWritePath(t *testing.T) {
	if len(ruleDeclaredKinds6776) != 25 {
		t.Fatalf("ledger transcription has %d entries, want 25 (see internal/entkinds)", len(ruleDeclaredKinds6776))
	}
	for _, kind := range ruleDeclaredKinds6776 {
		t.Run(kind, func(t *testing.T) {
			if types.IsValidEntityKind(kind) {
				t.Fatalf("%q is now a valid entity kind — it has been migrated, so drop it from "+
					"ruleDeclaredKinds6776 and from internal/entkinds' ledger in the same change", kind)
			}
			doc := &graph.Document{Entities: []graph.Entity{
				entFixture("x", kind),
				entFixture("y", string(types.EntityKindFunction)),
			}}
			_, rep, err := marshalWithReport(doc)
			if err != nil {
				t.Fatalf("marshalWithReport: %v", err)
			}
			if rep.Entities != 1 || rep.EntityDistinctKinds != 1 {
				t.Fatalf("kind %q: Entities=%d DistinctKinds=%d, want 1/1 (the SCOPE.Function entity must not be counted)",
					kind, rep.Entities, rep.EntityDistinctKinds)
			}
			if len(rep.EntityKinds) != 1 || rep.EntityKinds[0].Kind != kind || rep.EntityKinds[0].Entities != 1 {
				t.Fatalf("kind %q: EntityKinds=%+v, want exactly one entry naming it once", kind, rep.EntityKinds)
			}
		})
	}
}
