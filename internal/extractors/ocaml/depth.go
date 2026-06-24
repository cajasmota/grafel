// depth.go — framework-aware OCaml extraction: Caqti DB queries and alcotest
// test suites (#5374, bootstrap epic #5360).
//
// These sit ALONGSIDE the structural base extractor (extractor.go). The base
// extractor mines modules / let-functions / type declarations and
// IMPORTS/CALLS/CONTAINS edges; this file adds two framework records:
//
//   - Caqti DB: query request definitions written with the `Caqti_request.Infix`
//     operators (`(unit ->. unit) @:- "SQL"`, `(int ->! string) @@: "SQL"`) and
//     the `[%rapper ...]` ppx_rapper query quotations are recognised as data-
//     access call-sites, each emitted as one SCOPE.Operation(subtype=db_query)
//     carrying db_library=caqti and the recovered SQL verb (SELECT/INSERT/…).
//     This is the data-access surface the coverage Queries lane keys off.
//
//   - alcotest testing: `Alcotest.test_case "name" `Quick f` cases (and the
//     `Alcotest.run "suite" [...]` driver) are lifted into one
//     SCOPE.Operation(subtype=test_suite) per test file, carrying the example
//     count, mirroring the Haskell hspec / Crystal spec-suite model.
//
// Honest scope (partial, no fabrication):
//   - Caqti: the SQL string + leading verb are recovered; the typed
//     row-encoder/decoder parameter spec, the connection-module dispatch
//     (`Db.exec` / `Db.find` against a pool) and the bound table/model graph are
//     NOT modelled — query_attribution is a documented partial.
//   - alcotest: literal `test_case` cases inside a test file are counted; the
//     `(group, cases)` nesting structure and QCheck property generators are not
//     separately modelled (follow-up).
package ocaml

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// Caqti DB queries
// ---------------------------------------------------------------------------

// caqtiInfixRE matches a Caqti_request.Infix query operator applied to a
// SQL string literal. The Infix operators encode the result arity:
//
//	(unit ->. unit)        @:- "DELETE FROM users WHERE id = ?"   (exec)
//	(int  ->! string)      @@: "SELECT name FROM users WHERE id = ?"  (find)
//	(unit ->* string)      ... "SELECT name FROM users"            (collect)
//
// The query string is the trailing string literal; capture group 1 is the SQL.
// Both the `@:-`/`@@:`/`@/`-family operators and a plain string after a Caqti
// arity arrow are matched by anchoring on the closing `)` of the type spec.
var caqtiInfixRE = regexp.MustCompile(
	`->[.!*]\s*[a-zA-Z_][a-zA-Z0-9_' ]*\)\s*[@/:.-]*\s*"([^"]*)"`)

// caqtiRapperRE matches a ppx_rapper query quotation: `[%rapper get_one "SQL"]`
// / `[%rapper execute "SQL" ...]`. Capture group 1 is the SQL string.
var caqtiRapperRE = regexp.MustCompile(
	`\[%rapper\s+\w+\s+"([^"]*)"`)

// sqlVerbRE pulls the leading SQL verb out of a query string.
var sqlVerbRE = regexp.MustCompile(`(?i)^\s*(select|insert|update|delete|with|create|alter|drop)\b`)

// extractCaqtiQueries scans an OCaml source for Caqti query definitions and
// emits one SCOPE.Operation(subtype=db_query) per recovered SQL query.
func extractCaqtiQueries(src, filePath string) []types.EntityRecord {
	if !strings.Contains(src, "Caqti") && !strings.Contains(src, "rapper") {
		return nil
	}
	var out []types.EntityRecord
	seen := make(map[string]bool)

	emit := func(sql string, pos int) {
		sql = strings.TrimSpace(sql)
		if sql == "" {
			return
		}
		// A Caqti query string always contains a SQL verb; reject incidental
		// matches (e.g. a non-SQL string after an arrow) honestly.
		vm := sqlVerbRE.FindStringSubmatch(sql)
		if vm == nil {
			return
		}
		verb := strings.ToUpper(vm[1])
		key := verb + "|" + sql
		if seen[key] {
			return
		}
		seen[key] = true
		line := 1 + strings.Count(src[:pos], "\n")
		// A stable, readable name: the verb + table-ish first words.
		name := "caqti_query:" + verb + ":" + strconv.Itoa(line)
		out = append(out, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "db_query",
			SourceFile: filePath,
			Language:   "ocaml",
			StartLine:  line,
			EndLine:    line,
			Signature:  truncateSQL(sql),
			Properties: map[string]string{
				"db_library": "caqti",
				"query_verb": verb,
				"query":      truncateSQL(sql),
				"provenance": "INFERRED_FROM_CAQTI_QUERY",
			},
		})
	}

	for _, m := range caqtiInfixRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], m[0])
	}
	for _, m := range caqtiRapperRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], m[0])
	}
	return out
}

// truncateSQL caps a recovered SQL string to a readable single-line signature.
func truncateSQL(sql string) string {
	sql = strings.Join(strings.Fields(sql), " ")
	if len(sql) > 120 {
		return sql[:117] + "..."
	}
	return sql
}

// ---------------------------------------------------------------------------
// alcotest testing
// ---------------------------------------------------------------------------

// alcotestCaseRE matches an alcotest `test_case "label" speed fn` test-case
// constructor. Capture group 1 is the case label. The `Alcotest.` qualifier is
// optional (modules commonly `open Alcotest`).
var alcotestCaseRE = regexp.MustCompile(`\btest_case\s+"([^"\n\r]*)"`)

// isAlcotestFile reports whether a path looks like an OCaml test file
// (conventionally under a `test`/`tests` dir or named *_test.ml / test_*.ml).
func isAlcotestFile(filePath string) bool {
	p := filepath.ToSlash(filePath)
	base := filepath.Base(p)
	if strings.HasSuffix(base, "_test.ml") || strings.HasPrefix(base, "test_") {
		return true
	}
	for _, seg := range strings.Split(filepath.Dir(p), "/") {
		if seg == "test" || seg == "tests" {
			return true
		}
	}
	return false
}

// extractAlcotestSuite emits one SCOPE.Operation(subtype=test_suite) per OCaml
// test file carrying the alcotest test_case count. No suite is emitted for a
// file with no cases or one with no alcotest marker.
func extractAlcotestSuite(src, filePath string) []types.EntityRecord {
	hasAlcotest := strings.Contains(src, "Alcotest") || strings.Contains(src, "test_case")
	if !isAlcotestFile(filePath) && !hasAlcotest {
		return nil
	}
	matches := alcotestCaseRE.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return nil
	}
	// Require a real alcotest marker so a `test_case` helper in non-alcotest
	// code does not fabricate a suite.
	if !strings.Contains(src, "Alcotest") {
		return nil
	}
	exampleCount := len(matches)
	base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(filePath)), ".ml")
	rec := types.EntityRecord{
		Name:       "test_suite:" + base,
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: filePath,
		Language:   "ocaml",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":      "alcotest",
			"test_framework": "alcotest",
			"provenance":     "INFERRED_FROM_ALCOTEST_SUITE",
			"example_count":  strconv.Itoa(exampleCount),
		},
	}
	// Subject affinity: a `foo_test.ml` / `test_foo.ml` file conventionally
	// tests the module `Foo`, so emit a name-affinity TESTS edge.
	stem := alcotestSubjectStem(base)
	if stem != "" && stem != base {
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			ToID: stem,
			Kind: string(types.RelationshipKindTests),
			Properties: map[string]string{
				"framework":    "alcotest",
				"match_source": "test_stem_affinity",
			},
		})
	}
	return []types.EntityRecord{rec}
}

// alcotestSubjectStem derives the tested-module stem from a test-file base name
// (`user_test` → `user`, `test_user` → `user`).
func alcotestSubjectStem(base string) string {
	switch {
	case strings.HasSuffix(base, "_test"):
		return strings.TrimSuffix(base, "_test")
	case strings.HasPrefix(base, "test_"):
		return strings.TrimPrefix(base, "test_")
	default:
		return ""
	}
}
