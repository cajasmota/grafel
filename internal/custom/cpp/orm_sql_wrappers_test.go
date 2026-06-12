package cpp_test

// orm_sql_wrappers_test.go — tests for the #4978 query-attribution extractors
// of the three detection-only C/C++ SQL wrappers: nanodbc, SQLiteCpp, and the
// direct SQLite3 C API.

import (
	"testing"
)

// queryVerbs collects (sql_verb) from every SCOPE.Operation/query entity.
func queryVerbs(ents []entitySummary) []string {
	var verbs []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			verbs = append(verbs, e.Props["sql_verb"])
		}
	}
	return verbs
}

// queryByVerb returns the first query entity whose sql_verb matches, or nil.
func queryByVerb(ents []entitySummary, verb string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "query" &&
			ents[i].Props["sql_verb"] == verb {
			return &ents[i]
		}
	}
	return nil
}

// ============================================================================
// nanodbc
// ============================================================================

func TestNanodbcQueryExtraction(t *testing.T) {
	src := `
#include <nanodbc/nanodbc.h>
void run(nanodbc::connection& conn) {
	nanodbc::execute(conn, "SELECT id, name FROM users WHERE id = 1");
	nanodbc::statement stmt(conn, "INSERT INTO logs (msg) VALUES ('x')");
	nanodbc::result r = nanodbc::execute(conn, "DELETE FROM sessions");
}
`
	ents := extract(t, "custom_cpp_nanodbc", fi("dao.cpp", "cpp", src))
	verbs := queryVerbs(ents)
	for _, want := range []string{"SELECT", "INSERT", "DELETE"} {
		if !containsStr(verbs, want) {
			t.Errorf("expected %s verb, got %v", want, verbs)
		}
	}
	// Value assertion: table is resolved from the SQL, not just the verb.
	if q := queryByVerb(ents, "SELECT"); q == nil {
		t.Fatal("no SELECT query")
	} else {
		if got := q.Props["sql_table"]; got != "users" {
			t.Errorf("SELECT sql_table = %q, want users", got)
		}
		if got := q.Props["framework"]; got != "nanodbc" {
			t.Errorf("framework = %q, want nanodbc", got)
		}
	}
}

func TestNanodbcMethodForm(t *testing.T) {
	src := `
#include <nanodbc/nanodbc.h>
void run(nanodbc::connection& conn) {
	conn.execute("UPDATE accounts SET balance = 0");
}
`
	ents := extract(t, "custom_cpp_nanodbc", fi("dao.cpp", "cpp", src))
	if q := queryByVerb(ents, "UPDATE"); q == nil {
		t.Fatalf("expected UPDATE query, got %v", queryVerbs(ents))
	} else if got := q.Props["sql_table"]; got != "accounts" {
		t.Errorf("UPDATE sql_table = %q, want accounts", got)
	}
}

func TestNanodbcNoMatch(t *testing.T) {
	src := `int main() { return 0; }`
	if ents := extract(t, "custom_cpp_nanodbc", fi("main.cpp", "cpp", src)); len(ents) != 0 {
		t.Errorf("expected no entities for non-nanodbc source, got %d", len(ents))
	}
}

// ============================================================================
// SQLiteCpp
// ============================================================================

func TestSQLiteCppQueryExtraction(t *testing.T) {
	src := `
#include <SQLiteCpp/SQLiteCpp.h>
void run(SQLite::Database& db) {
	SQLite::Statement query(db, "SELECT * FROM products WHERE price > 10");
	db.exec("CREATE TABLE widgets (id INTEGER PRIMARY KEY)");
}
`
	ents := extract(t, "custom_cpp_sqlitecpp", fi("store.cpp", "cpp", src))
	verbs := queryVerbs(ents)
	for _, want := range []string{"SELECT", "CREATE"} {
		if !containsStr(verbs, want) {
			t.Errorf("expected %s verb, got %v", want, verbs)
		}
	}
	if q := queryByVerb(ents, "SELECT"); q == nil {
		t.Fatal("no SELECT query")
	} else {
		if got := q.Props["sql_table"]; got != "products" {
			t.Errorf("SELECT sql_table = %q, want products", got)
		}
		if got := q.Props["framework"]; got != "sqlitecpp" {
			t.Errorf("framework = %q, want sqlitecpp", got)
		}
	}
}

func TestSQLiteCppNoMatch(t *testing.T) {
	// `.exec(` is generic; without the SQLiteCpp signal we must NOT match.
	src := `void f() { thread.exec("not sql"); }`
	if ents := extract(t, "custom_cpp_sqlitecpp", fi("x.cpp", "cpp", src)); len(ents) != 0 {
		t.Errorf("expected no entities without SQLiteCpp signal, got %d", len(ents))
	}
}

// ============================================================================
// SQLite direct C API
// ============================================================================

func TestSQLiteCAPIQueryExtraction(t *testing.T) {
	src := `
#include <sqlite3.h>
void run(sqlite3* db) {
	sqlite3_stmt* stmt;
	sqlite3_prepare_v2(db, "SELECT name FROM customers", -1, &stmt, NULL);
	sqlite3_exec(db, "DELETE FROM cache", NULL, NULL, NULL);
}
`
	ents := extract(t, "custom_cpp_sqlite_capi", fi("db.c", "cpp", src))
	verbs := queryVerbs(ents)
	for _, want := range []string{"SELECT", "DELETE"} {
		if !containsStr(verbs, want) {
			t.Errorf("expected %s verb, got %v", want, verbs)
		}
	}
	if q := queryByVerb(ents, "SELECT"); q == nil {
		t.Fatal("no SELECT query")
	} else {
		if got := q.Props["sql_table"]; got != "customers" {
			t.Errorf("SELECT sql_table = %q, want customers", got)
		}
		if got := q.Props["framework"]; got != "sqlite_direct_c_api" {
			t.Errorf("framework = %q, want sqlite_direct_c_api", got)
		}
	}
}

func TestSQLiteCAPINoMatch(t *testing.T) {
	src := `int main() { return 0; }`
	if ents := extract(t, "custom_cpp_sqlite_capi", fi("main.cpp", "cpp", src)); len(ents) != 0 {
		t.Errorf("expected no entities for non-sqlite3 source, got %d", len(ents))
	}
}
