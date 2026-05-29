package python_test

// alembic_schema_test.go — proving fixtures for the Alembic schema extractor.
// Dedicated to issue #3192.
//
// These tests assert that the structural Alembic migration operations
// (op.create_table / op.add_column / op.create_index) are parsed into
// SCOPE.Schema table, column, and index entities.

import "testing"

// findAlembicEntity returns the first entity matching name+kind, or nil.
func findAlembicEntity(result []extractResult, name, kind string) *extractResult {
	for i := range result {
		if result[i].Name == name && result[i].Kind == kind {
			return &result[i]
		}
	}
	return nil
}

// alembicColumnNames returns the column entity names (-> type) for a parent table.
func alembicColumnNames(result []extractResult, parentTable string) map[string]string {
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

func TestAlembicSchema_CreateTable(t *testing.T) {
	src := fixtureSchema(t, "alembic_schema.py")
	ents := extract(t, "python_alembic_schema", src)

	users := findAlembicEntity(ents, "users", "SCOPE.Schema")
	if users == nil {
		t.Fatal("expected users table entity")
	}
	if users.Subtype != "" {
		t.Fatalf("expected table subtype empty, got %q", users.Subtype)
	}
	if users.Props["framework"] != "alembic" || users.Props["pattern_type"] != "table" {
		t.Fatalf("users props wrong: %+v", users.Props)
	}
	if users.Props["source"] != "alembic_migration" {
		t.Fatalf("expected source=alembic_migration, got %q", users.Props["source"])
	}

	if findAlembicEntity(ents, "orders", "SCOPE.Schema") == nil {
		t.Fatal("expected orders table entity")
	}

	cols := alembicColumnNames(ents, "users")
	for _, want := range []string{"users.id", "users.username", "users.email", "users.created_at"} {
		if _, ok := cols[want]; !ok {
			t.Fatalf("expected column %q, got %v", want, cols)
		}
	}
	// Type token is the final identifier of the sa.<Type>() argument.
	if cols["users.username"] != "String" {
		t.Fatalf("expected users.username String, got %q", cols["users.username"])
	}
	if cols["users.id"] != "Integer" {
		t.Fatalf("expected users.id Integer, got %q", cols["users.id"])
	}
	// PrimaryKeyConstraint / UniqueConstraint must NOT leak as columns.
	if _, leaked := cols["users.PrimaryKeyConstraint"]; leaked {
		t.Fatal("PrimaryKeyConstraint leaked as a column")
	}

	ocols := alembicColumnNames(ents, "orders")
	if ocols["orders.total"] != "Numeric" {
		t.Fatalf("expected orders.total Numeric, got %q", ocols["orders.total"])
	}
	// Dialect-qualified type (postgresql.JSONB) captures the final identifier.
	if ocols["orders.metadata"] != "JSONB" {
		t.Fatalf("expected orders.metadata JSONB, got %q", ocols["orders.metadata"])
	}
}

func TestAlembicSchema_AddColumn(t *testing.T) {
	src := fixtureSchema(t, "alembic_schema.py")
	ents := extract(t, "python_alembic_schema", src)

	cols := alembicColumnNames(ents, "users")
	if cols["users.is_active"] != "Boolean" {
		t.Fatalf("expected users.is_active Boolean from add_column, got %q (cols=%v)", cols["users.is_active"], cols)
	}
}

func TestAlembicSchema_CreateIndex(t *testing.T) {
	src := fixtureSchema(t, "alembic_schema.py")
	ents := extract(t, "python_alembic_schema", src)

	// Plain string index name.
	ix := findAlembicEntity(ents, "users.ix_users_email", "SCOPE.Schema")
	if ix == nil {
		t.Fatal("expected users.ix_users_email index entity")
	}
	if ix.Subtype != "index" || ix.Props["parent_table"] != "users" {
		t.Fatalf("index props wrong: subtype=%q props=%+v", ix.Subtype, ix.Props)
	}
	if ix.Props["index_name"] != "ix_users_email" {
		t.Fatalf("expected index_name=ix_users_email, got %q", ix.Props["index_name"])
	}

	// op.f("...") wrapped index name.
	if findAlembicEntity(ents, "orders.ix_orders_user_id", "SCOPE.Schema") == nil {
		t.Fatal("expected orders.ix_orders_user_id index entity (op.f wrapped)")
	}
}

// Negative: a non-Alembic module that happens to call create_table on some
// local `op` object must yield nothing (no `from alembic import op`).
func TestAlembicSchema_NotAlembic(t *testing.T) {
	src := `
import sqlalchemy as sa

op = something_else()
op.create_table("ghost", sa.Column("id", sa.Integer()))
`
	if got := extract(t, "python_alembic_schema", src); len(got) != 0 {
		t.Fatalf("expected no entities for non-Alembic module, got %d: %+v", len(got), got)
	}
}
