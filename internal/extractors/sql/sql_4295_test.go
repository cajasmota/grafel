package sql_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findColumn returns the column entity for table.col, or nil.
func findColumn(entities []types.EntityRecord, table, col string) *types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "column" &&
			e.Properties["table"] == table && e.Properties["column"] == col {
			return e
		}
	}
	return nil
}

// TestSQL4295_ColumnFlagsAndFK validates the offline DDL slice of #4295:
// two tables + a FK -> two model entities, column field members carrying
// type/nullable/PK/unique/default flags, and the users.org_id -> orgs FK edge.
func TestSQL4295_ColumnFlagsAndFK(t *testing.T) {
	src := loadFixture(t, "schema_4295_flags.sql")
	entities := extractSQLBytes(t, src, "db/schema_4295_flags.sql")

	// Two table model entities.
	for _, tbl := range []string{"orgs", "users"} {
		e := findEntity(entities, "SCOPE.Datastore", tbl)
		if e == nil {
			t.Fatalf("missing table entity %q", tbl)
		}
		if e.Subtype != "table" {
			t.Errorf("table %q: subtype = %q, want table", tbl, e.Subtype)
		}
	}

	// orgs.id: PK, not nullable, type present.
	if c := findColumn(entities, "orgs", "id"); c == nil {
		t.Fatal("missing orgs.id column")
	} else {
		if c.Properties["is_primary_key"] != "true" {
			t.Errorf("orgs.id is_primary_key = %q, want true", c.Properties["is_primary_key"])
		}
		if c.Properties["nullable"] != "false" {
			t.Errorf("orgs.id nullable = %q, want false (PK implies NOT NULL)", c.Properties["nullable"])
		}
		if c.Properties["col_type"] == "" {
			t.Errorf("orgs.id col_type empty, want a type")
		}
	}

	// orgs.name: NOT NULL + UNIQUE, type VARCHAR(255).
	if c := findColumn(entities, "orgs", "name"); c == nil {
		t.Fatal("missing orgs.name column")
	} else {
		if c.Properties["nullable"] != "false" {
			t.Errorf("orgs.name nullable = %q, want false", c.Properties["nullable"])
		}
		if c.Properties["is_unique"] != "true" {
			t.Errorf("orgs.name is_unique = %q, want true", c.Properties["is_unique"])
		}
		if c.Properties["col_type"] != "VARCHAR(255)" {
			t.Errorf("orgs.name col_type = %q, want VARCHAR(255)", c.Properties["col_type"])
		}
	}

	// users.email: NOT NULL, not PK, type TEXT, nullable=false.
	if c := findColumn(entities, "users", "email"); c == nil {
		t.Fatal("missing users.email column")
	} else {
		if c.Properties["nullable"] != "false" {
			t.Errorf("users.email nullable = %q, want false", c.Properties["nullable"])
		}
		if c.Properties["is_primary_key"] == "true" {
			t.Errorf("users.email wrongly flagged primary key")
		}
		if c.Properties["col_type"] != "TEXT" {
			t.Errorf("users.email col_type = %q, want TEXT", c.Properties["col_type"])
		}
	}

	// users.org_id: nullable (no NOT NULL), carries the FK.
	if c := findColumn(entities, "users", "org_id"); c == nil {
		t.Fatal("missing users.org_id column")
	} else if c.Properties["nullable"] != "true" {
		t.Errorf("users.org_id nullable = %q, want true", c.Properties["nullable"])
	}

	// users.status: DEFAULT 'active'.
	if c := findColumn(entities, "users", "status"); c == nil {
		t.Fatal("missing users.status column")
	} else if c.Properties["default"] != "'active'" {
		t.Errorf("users.status default = %q, want 'active'", c.Properties["default"])
	}

	// users.created_at: DEFAULT now() (function call captured whole).
	if c := findColumn(entities, "users", "created_at"); c == nil {
		t.Fatal("missing users.created_at column")
	} else if c.Properties["default"] != "now()" {
		t.Errorf("users.created_at default = %q, want now()", c.Properties["default"])
	}

	// The FK edge users.org_id -> orgs (REFERENCES). The relationship lives on
	// the column entity; from_table/to(table+column) are carried as properties.
	var foundFK bool
	for i := range entities {
		for _, rel := range entities[i].Relationships {
			if rel.Kind == "REFERENCES" &&
				rel.Properties["from_table"] == "users" &&
				rel.ToID == "orgs" {
				foundFK = true
				if rel.Properties["to_column"] != "id" {
					t.Errorf("FK to_column = %q, want id", rel.Properties["to_column"])
				}
				if c := findColumn(entities, "users", "org_id"); c != nil &&
					entities[i].Name != c.Name {
					t.Errorf("FK relationship on %q, want it on the org_id column", entities[i].Name)
				}
			}
		}
	}
	if !foundFK {
		t.Error("missing REFERENCES edge users.org_id -> orgs")
	}
}

// TestSQL4295_SelectOnlyNoTable is the negative fixture: a query-only .sql
// file emits no table model entity.
func TestSQL4295_SelectOnlyNoTable(t *testing.T) {
	src := loadFixture(t, "select_only_4295.sql")
	entities := extractSQLBytes(t, src, "queries/select_only_4295.sql")

	for i := range entities {
		if entities[i].Kind == "SCOPE.Datastore" && entities[i].Subtype == "table" {
			t.Errorf("SELECT-only file produced table entity %q", entities[i].Name)
		}
		if entities[i].Kind == "SCOPE.Schema" && entities[i].Subtype == "column" {
			t.Errorf("SELECT-only file produced column entity %q", entities[i].Name)
		}
	}
}
