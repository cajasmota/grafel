package sql_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// hasFK returns true when there is a column entity (table, col) with a
// REFERENCES edge to (toTable, toCol). Issue #141: column entity Name is
// "<table>.<column>" so we match on Properties["column"] (short name).
func hasFK(entities []types.EntityRecord, table, col, toTable, toCol string) bool {
	for _, e := range entities {
		if e.Subtype != "column" || e.Properties["column"] != col {
			continue
		}
		if e.Properties == nil || e.Properties["table"] != table {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" || r.ToID != toTable {
				continue
			}
			if r.Properties["reference_kind"] != "foreign_key" {
				continue
			}
			if toCol != "" && r.Properties["to_column"] != toCol {
				continue
			}
			if r.Properties["from_table"] != table {
				continue
			}
			return true
		}
	}
	return false
}

// TestSQLExtractor_AlterTableAddConstraintFK covers Pattern 1:
// ALTER TABLE ... ADD CONSTRAINT name FOREIGN KEY (col) REFERENCES tbl(col)
func TestSQLExtractor_AlterTableAddConstraintFK(t *testing.T) {
	src := loadFixture(t, "migration_002_alter_fks.sql")
	entities := extractSQLBytes(t, src, "migrations/002_alter_fks.sql")

	if !hasFK(entities, "orders", "user_id", "users", "id") {
		t.Errorf("expected orders.user_id → users(id) FK from ALTER TABLE ADD CONSTRAINT")
	}
}

// TestSQLExtractor_AlterTableAddConstraintFKOnDeleteCascade covers Pattern 2:
// FK with referential action clause.
func TestSQLExtractor_AlterTableAddConstraintFKOnDeleteCascade(t *testing.T) {
	src := loadFixture(t, "migration_002_alter_fks.sql")
	entities := extractSQLBytes(t, src, "migrations/002_alter_fks.sql")

	if !hasFK(entities, "invoices", "user_id", "users", "id") {
		t.Errorf("expected invoices.user_id → users(id) FK with ON DELETE CASCADE")
	}
}

// TestSQLExtractor_AlterTableAddMultiColumnFK covers Pattern 3:
// composite FOREIGN KEY (a, b) REFERENCES other(c, d).
func TestSQLExtractor_AlterTableAddMultiColumnFK(t *testing.T) {
	src := loadFixture(t, "migration_002_alter_fks.sql")
	entities := extractSQLBytes(t, src, "migrations/002_alter_fks.sql")

	if !hasFK(entities, "memberships", "role_code", "roles", "code") {
		t.Errorf("expected memberships.role_code → roles(code)")
	}
	if !hasFK(entities, "memberships", "role_scope", "roles", "scope") {
		t.Errorf("expected memberships.role_scope → roles(scope)")
	}
}

// TestSQLExtractor_AlterTableAddFKWithoutConstraintName covers Pattern 4:
// Postgres ALTER TABLE ... ADD FOREIGN KEY (...) REFERENCES ...(...).
func TestSQLExtractor_AlterTableAddFKWithoutConstraintName(t *testing.T) {
	src := loadFixture(t, "migration_002_alter_fks.sql")
	entities := extractSQLBytes(t, src, "migrations/002_alter_fks.sql")

	if !hasFK(entities, "shipments", "customer_id", "users", "id") {
		t.Errorf("expected shipments.customer_id → users(id) FK from unnamed ALTER")
	}
}

// TestSQLExtractor_AlterTableAddFK_AttachesToExistingColumn ensures the
// REFERENCES edge is appended to the column entity created by the original
// CREATE TABLE pass rather than emitting a duplicate column entity.
func TestSQLExtractor_AlterTableAddFK_AttachesToExistingColumn(t *testing.T) {
	src := loadFixture(t, "migration_002_alter_fks.sql")
	entities := extractSQLBytes(t, src, "migrations/002_alter_fks.sql")

	count := 0
	for _, e := range entities {
		if e.Subtype != "column" || e.Properties["column"] != "user_id" {
			continue
		}
		if e.Properties != nil && e.Properties["table"] == "orders" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one orders.user_id column entity, got %d", count)
	}
}
