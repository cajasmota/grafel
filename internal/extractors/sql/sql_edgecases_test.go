package sql_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/sql"
	"github.com/cajasmota/archigraph/internal/types"
)

// loadSQLFixture reads a fixture from internal/extractors/sql/testdata.
func loadSQLFixture(t *testing.T, name string) string {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	path := filepath.Join(dir, "testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func extractFixture(t *testing.T, fixture, fakePath string) []types.EntityRecord {
	t.Helper()
	src := loadSQLFixture(t, fixture)
	ext, ok := extractor.Get("sql")
	if !ok {
		t.Fatal("sql extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     fakePath,
		Content:  []byte(src),
		Language: "sql",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return entities
}

// TestSQL_MySQL_AutoIncrementColumns verifies AUTO_INCREMENT columns are
// extracted as columns and the host table appears as a SCOPE.Datastore/table.
func TestSQL_MySQL_AutoIncrementColumns(t *testing.T) {
	entities := extractFixture(t, "migration_003_mysql.sql", "migrations/003.sql")

	tables := map[string]bool{}
	for _, e := range entities {
		if e.Subtype == "table" {
			tables[e.Name] = true
		}
	}
	for _, want := range []string{"products", "order_items"} {
		if !tables[want] {
			t.Errorf("expected MySQL table %q", want)
		}
	}

	// id columns from both tables exist (each tagged with its table via Properties).
	idColsByTable := map[string]bool{}
	for _, e := range entities {
		if e.Subtype != "column" || e.Properties["column"] != "id" {
			continue
		}
		if tbl, ok := e.Properties["table"]; ok {
			idColsByTable[tbl] = true
		}
	}
	for _, want := range []string{"products", "order_items"} {
		if !idColsByTable[want] {
			t.Errorf("expected AUTO_INCREMENT 'id' column for table %q", want)
		}
	}
}

// TestSQL_MySQL_EnumColumnTypes verifies ENUM-typed columns are extracted as
// regular columns (not lost to the parens-handling logic).
func TestSQL_MySQL_EnumColumnTypes(t *testing.T) {
	entities := extractFixture(t, "migration_003_mysql.sql", "migrations/003.sql")

	wantEnumColumns := map[string]string{
		"status":      "products",
		"visibility":  "products",
		"fulfillment": "order_items",
	}
	for col, table := range wantEnumColumns {
		found := false
		for _, e := range entities {
			if e.Subtype == "column" && e.Properties["column"] == col && e.Properties["table"] == table {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected ENUM column %q on table %q", col, table)
		}
	}
}

// TestSQL_MySQL_ForeignKeyExtracted verifies the FK from order_items.product_id
// to products(id) is preserved despite the MySQL-flavored CREATE TABLE.
func TestSQL_MySQL_ForeignKeyExtracted(t *testing.T) {
	entities := extractFixture(t, "migration_003_mysql.sql", "migrations/003.sql")

	hasFK := false
	for _, e := range entities {
		if e.Subtype != "column" || e.Properties["column"] != "product_id" {
			continue
		}
		if e.Properties["table"] != "order_items" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" &&
				r.ToID == "products" &&
				r.Properties["reference_kind"] == "foreign_key" {
				hasFK = true
				break
			}
		}
	}
	if !hasFK {
		t.Errorf("expected FK order_items.product_id → products(id)")
	}
}

// TestSQL_NegativeGuard_CreateTypeNotATable verifies CREATE TYPE statements
// (both AS ENUM and AS composite) do not produce SCOPE.Datastore/table
// entities.
func TestSQL_NegativeGuard_CreateTypeNotATable(t *testing.T) {
	entities := extractFixture(t, "migration_004_negatives.sql", "migrations/004.sql")

	for _, e := range entities {
		if e.Subtype == "table" {
			switch e.Name {
			case "order_status", "address":
				t.Errorf("CREATE TYPE %q must NOT be parsed as a table (got Subtype=table)", e.Name)
			}
		}
	}
}

// TestSQL_NegativeGuard_CreateFunctionNotATable verifies CREATE FUNCTION
// statements do not produce SCOPE.Datastore/table entities (they should be
// emitted as Subtype=function, not table).
func TestSQL_NegativeGuard_CreateFunctionNotATable(t *testing.T) {
	entities := extractFixture(t, "migration_004_negatives.sql", "migrations/004.sql")

	for _, e := range entities {
		if e.Subtype == "table" {
			switch e.Name {
			case "compute_total", "upper_email":
				t.Errorf("CREATE FUNCTION %q must NOT be parsed as a table", e.Name)
			}
		}
	}

	// Sanity: functions are still recognized (as Subtype=function).
	functions := map[string]bool{}
	for _, e := range entities {
		if e.Subtype == "function" {
			functions[e.Name] = true
		}
	}
	for _, want := range []string{"compute_total", "upper_email"} {
		if !functions[want] {
			t.Errorf("expected CREATE FUNCTION %q to be parsed as Subtype=function", want)
		}
	}
}

// TestSQL_NegativeGuard_RealTableStillExtracted ensures the negative-case
// guard didn't over-suppress and the real CREATE TABLE in the fixture is
// still emitted.
func TestSQL_NegativeGuard_RealTableStillExtracted(t *testing.T) {
	entities := extractFixture(t, "migration_004_negatives.sql", "migrations/004.sql")

	hasCustomers := false
	for _, e := range entities {
		if e.Subtype == "table" && e.Name == "customers" {
			hasCustomers = true
			break
		}
	}
	if !hasCustomers {
		t.Errorf("expected real CREATE TABLE 'customers' to still be extracted alongside negatives")
	}
}
