package feedback

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/sql"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6543. The field-extraction metric could not see SQL at all: every SQL table
// is emitted as SCOPE.Datastore (internal/extractors/sql/sql.go), a tail
// classLikeKindTails did not admit, so tables were never in the denominator.
// Admitting the tail alone would have been worse than the gap: SQL's declared
// members are Subtype "column", not "field", so every table would have entered
// the denominator with a zero field-child count and reported a guaranteed 100%
// — #6536's defect re-created from the other side.
//
// As in the #6536 tests, the kind and subtype strings come FROM THE EXTRACTOR,
// never from a hand-written literal. The whole defect class is the tail list
// and the extractors disagreeing with nothing comparing them.

// sqlSource6543 exercises EVERY SCOPE.Datastore subtype the sql extractor
// emits, not just the one that passes: table (sql.go:249), view (:419),
// index (:444), function (:527), procedure (:585) and trigger (:634). Only
// `table` owns CONTAINS(contained_kind=column) children; the other five are
// guaranteed zero-field entities and must stay OUT of the denominator.
//
// A bare-CREATE-TABLE fixture is what let the first cut of this fix enrol all
// six — the population the metric measured was not the population the
// extractor emits. Indexes alone outnumber tables in most real schema files.
const sqlSource6543 = `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP
);

CREATE VIEW active_users AS SELECT id, email FROM users;

CREATE INDEX idx_users_email ON users (email);

CREATE FUNCTION user_count() RETURNS INTEGER AS $$
    SELECT COUNT(*) FROM users;
$$ LANGUAGE SQL;

CREATE PROCEDURE purge_users() AS $$
    DELETE FROM users;
$$ LANGUAGE SQL;

CREATE TRIGGER users_audit AFTER INSERT ON users
    FOR EACH ROW EXECUTE FUNCTION user_count();
`

func extractSQL6543(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex := &sql.Extractor{}
	recs, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "schema.sql",
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(recs) == 0 {
		t.Fatalf("Extract returned no records")
	}
	return recs
}

// recordsToDoc6543 projects SQL extractor records onto a graph.Document,
// binding structural-ref ToIDs the way the resolver's byLocation fallback
// does. A table→column CONTAINS edge points at
// "scope:schema:column:sql:<file>:<table>#<column>"
// (internal/extractor.BuildSchemaColumnStructuralRef) while the column
// entity's Name is "<table>.<column>", so the trailing segment maps to a name
// by replacing the "#" with a ".".
func recordsToDoc6543(t *testing.T, recs []types.EntityRecord) *graph.Document {
	t.Helper()
	ents := make([]graph.Entity, 0, len(recs))
	byName := make(map[string]string, len(recs))
	for i := range recs {
		id := "s" + string(rune('A'+i))
		r := &recs[i]
		byName[r.Name] = id
		ents = append(ents, graph.Entity{
			ID:         id,
			Name:       r.Name,
			Kind:       r.Kind,
			Subtype:    r.Subtype,
			Language:   r.Language,
			SourceFile: r.SourceFile,
			StartLine:  r.StartLine,
		}.WithProperties(map[string]string{}))
	}

	var rels []graph.Relationship
	for i := range recs {
		from := ents[i].ID
		for j, rr := range recs[i].Relationships {
			name := rr.ToID
			if k := strings.LastIndex(name, ":"); k >= 0 {
				name = name[k+1:]
			}
			name = strings.Replace(name, "#", ".", 1)
			to, ok := byName[name]
			if !ok {
				continue // unresolved external — irrelevant to this metric
			}
			props := map[string]string{}
			for _, p := range rr.Properties {
				props[p.K] = p.V
			}
			rels = append(rels, graph.Relationship{
				ID:     "r" + string(rune('A'+i)) + string(rune('a'+j)),
				FromID: from,
				ToID:   to,
				Kind:   rr.Kind,
			}.WithProperties(props))
		}
	}
	return makeDoc(ents, rels)
}

// datastoreSubtypes6543 returns the SCOPE.Datastore entities the sql extractor
// emitted for the fixture, grouped by Subtype. The population is taken FROM
// THE EXTRACTOR: the first cut of this fix enrolled all six SQL datastore
// subtypes because the fixture only contained the one that passes.
func datastoreSubtypes6543(recs []types.EntityRecord) map[string]int {
	out := map[string]int{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Datastore" {
			out[recs[i].Subtype]++
		}
	}
	return out
}

// TestSQLTableColumnsMeasuredNonVacuously_6543 is the killing test named in
// #6543: a SQL table with column children must be IN the denominator and must
// NOT report a 100% zero-field rate — while its five non-member-bearing
// datastore siblings stay out.
//
// It goes red in every permissive direction: remove "sql/table" from
// datastoreMemberBearingKinds and ClassTotal drops to 0 (the metric is blind
// again); narrow the member-child predicate back to Subtype "field" only and
// the rate is 100% (vacuous); widen the gate to the LANGUAGE and ClassTotal
// becomes 6 at 83% (the denominator is dominated by entities that cannot pass).
func TestSQLTableColumnsMeasuredNonVacuously_6543(t *testing.T) {
	recs := extractSQL6543(t, sqlSource6543)

	// Premise, taken from the extractor: a SCOPE.Datastore table exists, it
	// declares Subtype "column" children, and it is a MINORITY of the
	// SCOPE.Datastore entities in the file. If the fixture ever stops
	// containing the non-passing siblings this test silently weakens, so
	// assert that too.
	bySubtype := datastoreSubtypes6543(recs)
	var columns int
	for i := range recs {
		if recs[i].Subtype == "column" {
			columns++
		}
	}
	tables := bySubtype["table"]
	var otherDatastores int
	for st, n := range bySubtype {
		if st != "table" {
			otherDatastores += n
		}
	}
	if tables == 0 || columns == 0 {
		t.Fatalf("premise gone: sql extractor emitted %d SCOPE.Datastore tables and "+
			"%d column entities; this test would measure nothing", tables, columns)
	}
	if otherDatastores == 0 {
		t.Fatalf("premise gone: the fixture emitted no non-table SCOPE.Datastore "+
			"entities (subtypes seen: %v). A table-only fixture cannot observe the "+
			"gate granularity and is exactly what hid the first defect", bySubtype)
	}

	doc := recordsToDoc6543(t, recs)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if r.FieldExtractionRate.ClassTotal != tables {
		t.Errorf("ClassTotal = %d, want %d (one per SCOPE.Datastore TABLE): SQL tables "+
			"are the only member-bearing datastore emit site. The file also has %d "+
			"other SCOPE.Datastore entities (%v) which own no members, and columns are "+
			"member LEAVES — none of them belongs in the denominator",
			r.FieldExtractionRate.ClassTotal, tables, otherDatastores, bySubtype)
	}
	if r.FieldExtractionRate.ZeroFieldsPct == 100 {
		t.Errorf("ZeroFieldsPct = 100 for a table with %d column children: the "+
			"member-child predicate cannot see Subtype %q, so the metric reports a "+
			"guaranteed failure instead of a measurement (#6543)", columns, "column")
	}
	if got := r.FieldExtractionRate.ZeroFieldsPct; got != 0 {
		t.Errorf("ZeroFieldsPct = %v, want 0: every table in the fixture has columns, "+
			"so any non-zero rate means a population that cannot pass is enrolled", got)
	}
}

// TestOnlyTableSubtypeIsMemberBearing_6543 pins the gate GRANULARITY, which is
// the property a language-keyed gate got wrong. Every SCOPE.Datastore subtype
// the sql extractor emits is enumerated from the extractor itself and checked
// individually: `table` in, everything else out.
//
// A language-keyed gate — `datastoreMemberBearingLanguages["sql"]` — passes
// every other test in this file and fails here.
func TestOnlyTableSubtypeIsMemberBearing_6543(t *testing.T) {
	recs := extractSQL6543(t, sqlSource6543)
	bySubtype := datastoreSubtypes6543(recs)

	// The fixture must actually cover the sibling subtypes, or this test is
	// an assertion about an empty set.
	for _, want := range []string{"table", "view", "index", "function", "procedure", "trigger"} {
		if bySubtype[want] == 0 {
			t.Fatalf("fixture no longer emits a SCOPE.Datastore/%s (subtypes seen: %v): "+
				"the gate granularity is unobserved for that subtype", want, bySubtype)
		}
	}

	for subtype := range bySubtype {
		got := isFieldExtractionCandidate("SCOPE.Datastore", subtype, "sql")
		want := subtype == "table"
		if got != want {
			if want {
				t.Errorf("isFieldExtractionCandidate(SCOPE.Datastore, %q, sql) = false: a "+
					"SQL table owns CONTAINS(contained_kind=column) children and is the "+
					"population this metric exists to measure", subtype)
			} else {
				t.Errorf("isFieldExtractionCandidate(SCOPE.Datastore, %q, sql) = true: the "+
					"sql extractor emits no member children for this subtype, so every "+
					"one of them is a guaranteed zero-field failure in the denominator. "+
					"Gating on the LANGUAGE rather than the emit site enrols all %d "+
					"datastore subtypes in this file (#6543)", subtype, len(bySubtype))
			}
		}
	}
}

// TestUnknownDatastoreEmitSitesAreExcluded_6543 observes the ALLOWLIST choice
// itself — the one property the first cut argued for in prose and never
// tested.
//
// An allowlist and a denylist of the three known non-member-bearing languages
// are indistinguishable on every language anyone has enumerated. They differ
// only on an emit site nobody thought of, which is the entire reason the
// allowlist was chosen: a new SCOPE.Datastore emitter must be OUT of the
// denominator until someone checks that it owns members. Replace the map
// lookup with a denylist and this test is the only one that fails.
func TestUnknownDatastoreEmitSitesAreExcluded_6543(t *testing.T) {
	for _, tc := range []struct{ lang, subtype, why string }{
		{"duckdb", "table", "a new SQL-family extractor whose tables may or may not own columns"},
		{"prisma", "model", "a hypothetical schema-language datastore emitter"},
		{"sql", "materialized_view", "a new sql subtype added alongside the existing seven"},
		{"erlang", "ets_table", "erlang's actual ETS subtype (otp_deepen.go:407) — `<engine>_table`, one rename from colliding with a bare \"table\" allowlist"},
		{"erlang", "mnesia_table", "erlang's Mnesia subtype, same shape"},
	} {
		if isFieldExtractionCandidate("SCOPE.Datastore", tc.subtype, tc.lang) {
			t.Errorf("isFieldExtractionCandidate(SCOPE.Datastore, %q, %q) is true: %s. An "+
				"un-enumerated emit site must be EXCLUDED until someone verifies it owns "+
				"members — that is what makes this gate an allowlist rather than a "+
				"denylist of the languages that happen to be known today (#6543)",
				tc.subtype, tc.lang, tc.why)
		}
	}

	// The allowlist must still be case-insensitive like the other language
	// gates in report.go, in BOTH dimensions.
	for _, tc := range []struct{ lang, subtype string }{
		{"SQL", "table"},
		{"sql", "TABLE"},
		{"Sql", "Table"},
	} {
		if !isFieldExtractionCandidate("SCOPE.Datastore", tc.subtype, tc.lang) {
			t.Errorf("isFieldExtractionCandidate(SCOPE.Datastore, %q, %q) is false: the "+
				"emit-site key must fold case in both the language and the subtype",
				tc.subtype, tc.lang)
		}
	}
}

// TestColumnlessTableStillCountsAsZeroField_6543 pins the other direction: the
// member-child predicate must not be so loose that ANY structural child makes
// a container "pass". The table below owns a structural child that is not a
// declared member, and no columns — it is a genuine zero-field container and
// must still be counted as one. Widening the numerator from the "field"
// literal to a member-subtype SET is only safe while the set stays a set:
// replace it with "any CONTAINS child" and this test goes red.
//
// The non-member child is an entity the sql EXTRACTOR emitted (a trigger,
// SCOPE.Datastore/trigger, sql.go:634), located by subtype rather than
// hand-written — the earlier hand-written stand-in used SCOPE.Operation/
// trigger, a shape no extractor produces, and departing from the
// take-it-from-the-extractor rule is exactly where the gate defect hid.
func TestColumnlessTableStillCountsAsZeroField_6543(t *testing.T) {
	recs := extractSQL6543(t, sqlSource6543)
	doc := recordsToDoc6543(t, recs)

	var triggerID string
	for i := range doc.Entities {
		if doc.Entities[i].Kind == "SCOPE.Datastore" && doc.Entities[i].Subtype == "trigger" {
			triggerID = doc.Entities[i].ID
			break
		}
	}
	if triggerID == "" {
		t.Fatalf("premise gone: the fixture emitted no SCOPE.Datastore/trigger to use " +
			"as a non-member child; this test would observe nothing")
	}

	ents := append([]graph.Entity(nil), doc.Entities...)
	ents = append(ents, graph.Entity{
		ID:       "bare-table",
		Name:     "audit_log",
		Kind:     "SCOPE.Datastore",
		Subtype:  "table",
		Language: "sql",
	}.WithProperties(map[string]string{}))
	rels := append([]graph.Relationship(nil), doc.Relationships...)
	rels = append(rels, graph.Relationship{
		ID:     "bare-table-contains-trigger",
		FromID: "bare-table",
		ToID:   triggerID,
		Kind:   "CONTAINS",
	}.WithProperties(map[string]string{"contained_kind": "trigger"}))
	d2 := makeDoc(ents, rels)

	r, err := Generate(context.Background(), []*graph.Document{d2}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.FieldExtractionRate.ClassTotal < 2 {
		t.Fatalf("ClassTotal = %d, want >= 2: the columnless table was not admitted, "+
			"so this test observes nothing", r.FieldExtractionRate.ClassTotal)
	}
	if r.FieldExtractionRate.ZeroFieldsPct == 0 {
		t.Errorf("ZeroFieldsPct = 0 with one columnless table in the denominator: a " +
			"container whose only structural child is a non-member (a trigger) has no " +
			"declared members, and that is a real zero-field observation that must not " +
			"be masked by counting any CONTAINS child as a member")
	}
}

// TestNonColumnBearingDatastoresExcluded_6543 is the cross-language exemption
// guard, and the direct replacement for the SCOPE.Datastore arm of
// TestClassLikeKindTailsConstrainedFromAbove_6536.
//
// Each pair below is emitted at a cited non-test site and owns no member
// children: jcl/extractor.go:664,:779; cobol/ims.go:170,
// cobol/depth.go:701,:864.
//
// erlang is deliberately NOT in this table. Its SCOPE.Datastore entities are
// already excluded wholesale by nonFieldBearingLanguages, so an erlang arm
// here passes with this gate deleted entirely — it would look like coverage
// while observing nothing. The erlang subtypes are checked in
// TestUnknownDatastoreEmitSitesAreExcluded_6543 instead, where the assertion
// is about the gate rather than about a pre-existing exemption.
func TestNonColumnBearingDatastoresExcluded_6543(t *testing.T) {
	recs := extractSQL6543(t, sqlSource6543)
	doc := recordsToDoc6543(t, recs)

	base, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := base.FieldExtractionRate.ClassTotal
	if want == 0 {
		t.Fatalf("premise gone: baseline ClassTotal is 0, so admitting anything below " +
			"could not be distinguished from the blind state this issue fixes")
	}

	for _, tc := range []struct{ lang, subtype string }{
		{"jcl", "dataset"},
		{"cobol", "ims-database"},
		{"cobol", "queue"},
		{"cobol", "message-queue"},
		{"cobol", "file"},
	} {
		ents := append([]graph.Entity(nil), doc.Entities...)
		ents = append(ents, graph.Entity{
			ID:       "ds-" + tc.lang + "-" + tc.subtype,
			Name:     "Store",
			Kind:     "SCOPE.Datastore",
			Subtype:  tc.subtype,
			Language: tc.lang,
		}.WithProperties(map[string]string{}))
		d2 := makeDoc(ents, doc.Relationships)

		r, err := Generate(context.Background(), []*graph.Document{d2}, Opts{GroupName: "g", Version: "t"})
		if err != nil {
			t.Fatalf("Generate(%s/%s): %v", tc.lang, tc.subtype, err)
		}
		if got := r.FieldExtractionRate.ClassTotal; got != want {
			t.Errorf("adding one %s SCOPE.Datastore/%s changed ClassTotal %d -> %d: that "+
				"extractor emits no member children for this kind, so it is a "+
				"guaranteed zero-field failure in the denominator (#6543)",
				tc.lang, tc.subtype, want, got)
		}
	}
}
