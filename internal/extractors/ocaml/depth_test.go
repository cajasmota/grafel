package ocaml

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func findBySubtype(recs []types.EntityRecord, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Subtype == subtype {
			out = append(out, r)
		}
	}
	return out
}

// TestCaqti_InfixQueries covers the Caqti_request.Infix arity-operator query
// definitions: exec (->.), find (->!) and collect (->*).
func TestCaqti_InfixQueries(t *testing.T) {
	src := `
open Caqti_request.Infix
open Caqti_type

let list_users =
  (unit ->* string) "SELECT name FROM users ORDER BY name"

let find_user =
  (int ->! string) @@: "SELECT name FROM users WHERE id = ?"

let delete_user =
  (int ->. unit) @:- "DELETE FROM users WHERE id = ?"
`
	recs := extractCaqtiQueries(src, "lib/db.ml")
	queries := findBySubtype(recs, "db_query")
	if len(queries) != 3 {
		t.Fatalf("expected 3 caqti queries, got %d: %+v", len(queries), queries)
	}
	verbs := map[string]bool{}
	for _, q := range queries {
		if q.Properties["db_library"] != "caqti" {
			t.Errorf("query %s: db_library=%q want caqti", q.Name, q.Properties["db_library"])
		}
		verbs[q.Properties["query_verb"]] = true
	}
	for _, want := range []string{"SELECT", "DELETE"} {
		if !verbs[want] {
			t.Errorf("expected a %s query verb; got verbs=%v", want, verbs)
		}
	}
}

// TestCaqti_Rapper covers the ppx_rapper [%rapper ...] quotation form.
func TestCaqti_Rapper(t *testing.T) {
	src := `
let get_one =
  [%rapper get_one "SELECT @string{name} FROM users WHERE id = %int{id}"]
`
	queries := findBySubtype(extractCaqtiQueries(src, "lib/queries.ml"), "db_query")
	if len(queries) != 1 {
		t.Fatalf("expected 1 rapper query, got %d", len(queries))
	}
	if queries[0].Properties["query_verb"] != "SELECT" {
		t.Errorf("rapper query_verb=%q want SELECT", queries[0].Properties["query_verb"])
	}
}

// TestCaqti_NonDbFileNoEmit is the negative guard: a file with no Caqti/rapper
// marker yields no db_query entity.
func TestCaqti_NonDbFileNoEmit(t *testing.T) {
	src := `let greet name = "hello " ^ name`
	if recs := extractCaqtiQueries(src, "lib/util.ml"); len(recs) != 0 {
		t.Fatalf("non-db file must not emit queries; got %+v", recs)
	}
}

// TestAlcotest_Suite covers an alcotest test file: test_case cases counted into
// one test_suite operation with a stem-affinity TESTS edge.
func TestAlcotest_Suite(t *testing.T) {
	// Note: OCaml polymorphic-variant speed tags (`Quick) use a backtick that
	// cannot appear inside a Go raw string, so this fixture writes the speed
	// argument as a plain identifier — the test_case label is what the parser
	// counts.
	src := "open Alcotest\n" +
		"\n" +
		"let test_add () = check int \"1+1\" 2 (1 + 1)\n" +
		"let test_sub () = check int \"2-1\" 1 (2 - 1)\n" +
		"\n" +
		"let () =\n" +
		"  run \"math\" [\n" +
		"    \"arith\", [\n" +
		"      test_case \"add\" `Quick test_add;\n" +
		"      test_case \"sub\" `Quick test_sub;\n" +
		"    ];\n" +
		"  ]\n"
	recs := extractAlcotestSuite(src, "test/math_test.ml")
	suites := findBySubtype(recs, "test_suite")
	if len(suites) != 1 {
		t.Fatalf("expected 1 alcotest suite, got %d: %+v", len(suites), recs)
	}
	s := suites[0]
	if s.Properties["framework"] != "alcotest" {
		t.Errorf("framework=%q want alcotest", s.Properties["framework"])
	}
	if s.Properties["example_count"] != "2" {
		t.Errorf("example_count=%q want 2", s.Properties["example_count"])
	}
	// Stem-affinity TESTS edge: math_test.ml → math.
	var hasTests bool
	for _, rel := range s.Relationships {
		if rel.Kind == string(types.RelationshipKindTests) && rel.ToID == "math" {
			hasTests = true
		}
	}
	if !hasTests {
		t.Errorf("expected stem-affinity TESTS edge to 'math'; got %+v", s.Relationships)
	}
}

// TestAlcotest_NoCasesNoEmit confirms an alcotest-importing file with no
// test_case calls emits no suite (honest).
func TestAlcotest_NoCasesNoEmit(t *testing.T) {
	src := `open Alcotest
let () = run "empty" []`
	if recs := extractAlcotestSuite(src, "test/empty_test.ml"); len(recs) != 0 {
		t.Fatalf("a case-less alcotest file must emit no suite; got %+v", recs)
	}
}

// TestAlcotest_NonAlcotestNoEmit is the negative guard: a `test_case` helper in
// a file with no Alcotest marker must not fabricate a suite.
func TestAlcotest_NonAlcotestNoEmit(t *testing.T) {
	src := `let test_case name fn = fn name
let () = test_case "x" print_string`
	if recs := extractAlcotestSuite(src, "lib/helper.ml"); len(recs) != 0 {
		t.Fatalf("non-alcotest file must emit no suite; got %+v", recs)
	}
}
