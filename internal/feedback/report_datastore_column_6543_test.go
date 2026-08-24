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

const sqlSource6543 = `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP
);
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

// TestSQLTableColumnsMeasuredNonVacuously_6543 is the killing test named in
// #6543: a SQL table with column children must be IN the denominator and must
// NOT report a 100% zero-field rate.
//
// It goes red in both permissive directions the issue names — remove
// "datastore" from classLikeKindTails and ClassTotal drops to 0 (the metric is
// blind again); narrow the member-child predicate back to Subtype "field" only
// and the rate jumps to 100% (the metric is vacuous).
func TestSQLTableColumnsMeasuredNonVacuously_6543(t *testing.T) {
	recs := extractSQL6543(t, sqlSource6543)

	// Premise, taken from the extractor: a SCOPE.Datastore table exists and it
	// declares Subtype "column" children. If the extractor ever stops emitting
	// this shape the test must fail loudly rather than pass vacuously.
	var tables, columns int
	for i := range recs {
		switch {
		case recs[i].Kind == "SCOPE.Datastore" && recs[i].Subtype == "table":
			tables++
		case recs[i].Subtype == "column":
			columns++
		}
	}
	if tables == 0 || columns == 0 {
		t.Fatalf("premise gone: sql extractor emitted %d SCOPE.Datastore tables and "+
			"%d column entities for the fixture; this test would measure nothing",
			tables, columns)
	}

	doc := recordsToDoc6543(t, recs)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if r.FieldExtractionRate.ClassTotal != tables {
		t.Errorf("ClassTotal = %d, want %d (one per SCOPE.Datastore table): SQL tables "+
			"are class-like containers with declared members and must be in the "+
			"field-extraction denominator; columns are member LEAVES and must not be",
			r.FieldExtractionRate.ClassTotal, tables)
	}
	if r.FieldExtractionRate.ZeroFieldsPct == 100 {
		t.Errorf("ZeroFieldsPct = 100 for a table with %d column children: the "+
			"member-child predicate cannot see Subtype %q, so the metric reports a "+
			"guaranteed failure instead of a measurement (#6543)", columns, "column")
	}
	if got := r.FieldExtractionRate.ZeroFieldsPct; got != 0 {
		t.Errorf("ZeroFieldsPct = %v, want 0: every table in the fixture has columns", got)
	}
}

// TestColumnlessTableStillCountsAsZeroField_6543 pins the other direction: the
// member-child predicate must not be so loose that ANY structural child makes a
// container "pass". The table below owns a structural child that is not a
// declared member (a trigger), and no columns — it is a genuine zero-field
// container and must still be counted as one. Widening the numerator from the
// "field" literal to a member-subtype SET is only safe while the set stays a
// set: replace it with "any CONTAINS child" and this test goes red.
func TestColumnlessTableStillCountsAsZeroField_6543(t *testing.T) {
	recs := extractSQL6543(t, sqlSource6543)
	doc := recordsToDoc6543(t, recs)

	ents := append([]graph.Entity(nil), doc.Entities...)
	ents = append(ents,
		graph.Entity{
			ID:       "bare-table",
			Name:     "audit_log",
			Kind:     "SCOPE.Datastore",
			Subtype:  "table",
			Language: "sql",
		}.WithProperties(map[string]string{}),
		graph.Entity{
			ID:       "bare-table-trigger",
			Name:     "audit_log_ins",
			Kind:     "SCOPE.Operation",
			Subtype:  "trigger",
			Language: "sql",
		}.WithProperties(map[string]string{}),
	)
	rels := append([]graph.Relationship(nil), doc.Relationships...)
	rels = append(rels, graph.Relationship{
		ID:     "bare-table-contains-trigger",
		FromID: "bare-table",
		ToID:   "bare-table-trigger",
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
			"container with no declared members is a real zero-field observation and " +
			"must not be masked")
	}
}

// TestNonColumnBearingDatastoresExcluded_6543 is the exemption guard. Only the
// SQL extractor emits SCOPE.Datastore entities that own member children. A JCL
// dataset, a COBOL IMS message queue / CICS queue / file resource and an
// Erlang ETS table are all SCOPE.Datastore too, and none of them has a column
// or field child anywhere in the extractor output. Admitting the tail globally
// would enrol three populations that structurally cannot pass — which is
// exactly #6536, pointed at cobol/jcl/erlang instead of at C#.
//
// Each kind/language pair below is emitted at a cited non-test site:
// jcl/extractor.go:664,:779; cobol/ims.go:170, cobol/depth.go:701,:864;
// erlang/otp_deepen.go:407.
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
		{"cobol", "message-queue"},
		{"cobol", "queue"},
		{"cobol", "file"},
		{"erlang", "ets-table"},
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

// TestDatastoreMemberBearingLanguagesIsTheOnlyGate_6543 pins the exemption
// mechanism as DATA rather than as a conditional buried in the predicate: the
// set is the whole story, and a language absent from it is excluded no matter
// what subtype it carries.
func TestDatastoreMemberBearingLanguagesIsTheOnlyGate_6543(t *testing.T) {
	if !datastoreMemberBearingLanguages["sql"] {
		t.Fatalf("sql missing from datastoreMemberBearingLanguages: SQL tables own " +
			"Subtype \"column\" children and are the population #6543 is about")
	}
	for _, lang := range []string{"cobol", "jcl", "erlang"} {
		if datastoreMemberBearingLanguages[lang] {
			t.Errorf("%s in datastoreMemberBearingLanguages: its SCOPE.Datastore "+
				"entities have no member children", lang)
		}
		if isFieldExtractionCandidate("SCOPE.Datastore", "table", lang) {
			t.Errorf("isFieldExtractionCandidate(SCOPE.Datastore, table, %q) is true: the "+
				"exemption must be keyed on the language, not on the subtype the "+
				"entity happens to carry", lang)
		}
	}
	if !isFieldExtractionCandidate("SCOPE.Datastore", "table", "SQL") {
		t.Errorf("isFieldExtractionCandidate(SCOPE.Datastore, table, \"SQL\") is false: " +
			"the language gate must be case-insensitive like the others")
	}
}
