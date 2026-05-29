package python_test

// driver_schema_test.go — proving fixtures for the raw-driver schema
// extractor (mysql/postgres/sqlite). Dedicated to issue #3189.
//
// These tests assert that CREATE TABLE DDL embedded in raw-driver
// cursor.execute(...) string literals is parsed into SCOPE.Schema table and
// column entities for each of the three supported drivers.

import "testing"

// findDriverEntity returns the first entity matching name+kind, or nil.
func findDriverEntity(result []extractResult, name, kind string) *extractResult {
	for i := range result {
		if result[i].Name == name && result[i].Kind == kind {
			return &result[i]
		}
	}
	return nil
}

// driverColumnNames returns the column entity names belonging to a parent table.
func driverColumnNames(result []extractResult, parentTable string) map[string]string {
	cols := make(map[string]string)
	for _, e := range result {
		if e.Kind != "SCOPE.Schema" || e.Subtype != "column" {
			continue
		}
		if e.Props["parent_table"] == parentTable {
			cols[e.Name] = e.Props["column_type"]
		}
	}
	return cols
}

// ---------------------------------------------------------------------------
// MySQL (pymysql)
// ---------------------------------------------------------------------------

func TestDriverSchema_MySQL(t *testing.T) {
	src := fixtureSchema(t, "driver_schema_mysql.py")
	ents := extract(t, "python_driver_schema", src)

	users := findDriverEntity(ents, "users", "SCOPE.Schema")
	if users == nil {
		t.Fatal("expected users table entity")
	}
	if users.Props["framework"] != "mysql" || users.Props["pattern_type"] != "table" {
		t.Fatalf("users props wrong: %+v", users.Props)
	}
	if users.Props["driver_call"] != "execute" {
		t.Fatalf("expected driver_call=execute, got %q", users.Props["driver_call"])
	}

	cols := driverColumnNames(ents, "users")
	for _, want := range []string{"users.id", "users.username", "users.email", "users.created_at"} {
		if _, ok := cols[want]; !ok {
			t.Fatalf("expected column %q, got %v", want, cols)
		}
	}
	// Column types are uppercased.
	if cols["users.username"] != "VARCHAR" {
		t.Fatalf("expected username VARCHAR, got %q", cols["users.username"])
	}
	// Table-level constraint (UNIQUE KEY) must NOT be emitted as a column.
	if _, leaked := cols["users.UNIQUE"]; leaked {
		t.Fatal("UNIQUE KEY constraint leaked as a column")
	}

	if findDriverEntity(ents, "orders", "SCOPE.Schema") == nil {
		t.Fatal("expected orders table entity")
	}
	ocols := driverColumnNames(ents, "orders")
	if ocols["orders.total"] != "DECIMAL" {
		t.Fatalf("expected orders.total DECIMAL, got %q", ocols["orders.total"])
	}
}

// ---------------------------------------------------------------------------
// PostgreSQL (psycopg2)
// ---------------------------------------------------------------------------

func TestDriverSchema_Postgres(t *testing.T) {
	src := fixtureSchema(t, "driver_schema_postgres.py")
	ents := extract(t, "python_driver_schema", src)

	acct := findDriverEntity(ents, "accounts", "SCOPE.Schema")
	if acct == nil {
		t.Fatal("expected accounts table entity")
	}
	if acct.Props["framework"] != "postgres" {
		t.Fatalf("expected framework=postgres, got %q", acct.Props["framework"])
	}

	cols := driverColumnNames(ents, "accounts")
	for _, want := range []string{"accounts.id", "accounts.owner_name", "accounts.balance", "accounts.opened_at"} {
		if _, ok := cols[want]; !ok {
			t.Fatalf("expected column %q, got %v", want, cols)
		}
	}
	if cols["accounts.id"] != "SERIAL" {
		t.Fatalf("expected id SERIAL, got %q", cols["accounts.id"])
	}
}

// ---------------------------------------------------------------------------
// SQLite (sqlite3) — executescript with multiple CREATE TABLE statements
// ---------------------------------------------------------------------------

func TestDriverSchema_SQLite(t *testing.T) {
	src := fixtureSchema(t, "driver_schema_sqlite.py")
	ents := extract(t, "python_driver_schema", src)

	notes := findDriverEntity(ents, "notes", "SCOPE.Schema")
	if notes == nil {
		t.Fatal("expected notes table entity")
	}
	if notes.Props["framework"] != "sqlite" || notes.Props["driver_call"] != "executescript" {
		t.Fatalf("notes props wrong: %+v", notes.Props)
	}

	if findDriverEntity(ents, "tags", "SCOPE.Schema") == nil {
		t.Fatal("expected tags table entity (second CREATE TABLE in executescript)")
	}

	ncols := driverColumnNames(ents, "notes")
	for _, want := range []string{"notes.id", "notes.title", "notes.body", "notes.pinned"} {
		if _, ok := ncols[want]; !ok {
			t.Fatalf("expected column %q, got %v", want, ncols)
		}
	}
	tcols := driverColumnNames(ents, "tags")
	if _, ok := tcols["tags.label"]; !ok {
		t.Fatalf("expected tags.label column, got %v", tcols)
	}
}

// ---------------------------------------------------------------------------
// Negative: file without a supported driver import must yield nothing.
// ---------------------------------------------------------------------------

func TestDriverSchema_NoDriverImport(t *testing.T) {
	src := `
from sqlalchemy import create_engine
engine = create_engine("sqlite://")
engine.execute("CREATE TABLE x (id INTEGER PRIMARY KEY)")
`
	if got := extract(t, "python_driver_schema", src); len(got) != 0 {
		t.Fatalf("expected no entities without a raw-driver import, got %d", len(got))
	}
}
