package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// helper: build a doc from entities (no pre-existing edges).
func docOf(repo string, ents ...graph.Entity) *graph.Document {
	return &graph.Document{Repo: repo, Entities: ents}
}

// helper: find the synthetic SCOPE.Table node id for a normalised table.
func tableNodeFor(t *testing.T, doc *graph.Document, repo, norm string) string {
	t.Helper()
	want := graph.EntityID(repo, string(types.EntityKindTable), norm, "")
	for i := range doc.Entities {
		if doc.Entities[i].ID == want {
			if doc.Entities[i].Kind != string(types.EntityKindTable) {
				t.Fatalf("node %s has kind %q, want SCOPE.Table", want, doc.Entities[i].Kind)
			}
			if got := doc.Entities[i].Properties["table"]; got != norm {
				t.Fatalf("SCOPE.Table.table=%q, want %q", got, norm)
			}
			return want
		}
	}
	t.Fatalf("no SCOPE.Table node for (%s,%s) [id %s]", repo, norm, want)
	return ""
}

// helper: assert a MODIFIES_TABLE edge fromID→tableNode(norm) with op[/column].
func assertModifies(t *testing.T, doc *graph.Document, fromID, repo, norm, op, col string) {
	t.Helper()
	tableID := tableNodeFor(t, doc, repo, norm)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != string(types.RelationshipKindModifiesTable) {
			continue
		}
		if r.FromID != fromID || r.ToID != tableID {
			continue
		}
		if r.Properties["op"] != op {
			continue // same from/to, different op — keep looking
		}
		if r.Properties["table"] != norm {
			t.Fatalf("MODIFIES_TABLE table=%q, want %q", r.Properties["table"], norm)
		}
		if col != "" && r.Properties["column"] != col {
			t.Fatalf("MODIFIES_TABLE op=%s column=%q, want %q", op, r.Properties["column"], col)
		}
		return
	}
	t.Fatalf("no MODIFIES_TABLE edge %s -> Table:%s op=%s", fromID, norm, op)
}

// ---- Alembic ---------------------------------------------------------------

func TestAlembicCreateTableAndAddColumn(t *testing.T) {
	createTbl := graph.Entity{
		ID: "a1", Name: "orders", Kind: string(types.EntityKindSchema),
		Properties: map[string]string{
			"framework": "alembic", "pattern_type": "table", "table_name": "orders",
		},
	}
	addCol := graph.Entity{
		ID: "a2", Name: "orders.total", Kind: string(types.EntityKindSchema),
		Properties: map[string]string{
			"framework": "alembic", "pattern_type": "column", "parent_table": "orders",
		},
	}
	doc := docOf("svc", createTbl, addCol)

	st := ApplyMigrationSchemaOps(doc)
	if st.Skipped {
		t.Fatal("pass skipped, want edges")
	}
	assertModifies(t, doc, "a1", "svc", "orders", "create_table", "")
	assertModifies(t, doc, "a2", "svc", "orders", "add_column", "total")

	// Convergence: both ops point at the SAME table node.
	id1 := graph.EntityID("svc", string(types.EntityKindTable), "orders", "")
	tableCount := 0
	for i := range doc.Entities {
		if doc.Entities[i].Kind == string(types.EntityKindTable) {
			tableCount++
		}
	}
	if tableCount != 1 {
		t.Fatalf("expected 1 SCOPE.Table node (orders), got %d", tableCount)
	}
	_ = id1
}

// ---- Django ----------------------------------------------------------------

func TestDjangoCreateModelAndAddField(t *testing.T) {
	mig := graph.Entity{
		ID: "d1", Name: "0001_initial", Kind: "Migration", Subtype: "django",
		Properties: map[string]string{
			"operations": `[{"type":"CreateModel","model":"order"},` +
				`{"type":"AddField","model":"order","field":"total"},` +
				`{"type":"RemoveField","model":"order","field":"legacy"}]`,
		},
	}
	doc := docOf("svc", mig)
	st := ApplyMigrationSchemaOps(doc)
	if st.Skipped {
		t.Fatal("pass skipped")
	}
	assertModifies(t, doc, "d1", "svc", "order", "create_table", "")
	assertModifies(t, doc, "d1", "svc", "order", "add_column", "total")
	assertModifies(t, doc, "d1", "svc", "order", "drop_column", "legacy")
}

// ---- Rails / ActiveRecord --------------------------------------------------

func TestRailsAddColumn(t *testing.T) {
	op := graph.Entity{
		ID: "r1", Name: "add_column:orders.total", Kind: string(types.EntityKindEvolution),
		Subtype: "add_column",
		Properties: map[string]string{
			"framework": "activerecord", "ddl_operation": "add_column",
			"table_name": "orders", "column_name": "total",
		},
	}
	doc := docOf("svc", op)
	st := ApplyMigrationSchemaOps(doc)
	if st.Skipped {
		t.Fatal("skipped")
	}
	assertModifies(t, doc, "r1", "svc", "orders", "add_column", "total")
}

// ---- JS (knex / typeorm) ---------------------------------------------------

func TestKnexCreateTable(t *testing.T) {
	op := graph.Entity{
		ID: "k1", Name: "create_table:users", Kind: string(types.EntityKindEvolution),
		Subtype: "create_table",
		Properties: map[string]string{
			"framework": "knex", "migration_op": "createTable", "table": "users",
		},
	}
	doc := docOf("svc", op)
	st := ApplyMigrationSchemaOps(doc)
	if st.Skipped {
		t.Fatal("skipped")
	}
	assertModifies(t, doc, "k1", "svc", "users", "create_table", "")
}

func TestTypeORMAddColumn(t *testing.T) {
	op := graph.Entity{
		ID: "t1", Name: "add_column:users", Kind: string(types.EntityKindEvolution),
		Subtype: "add_column",
		Properties: map[string]string{
			"framework": "typeorm", "migration_op": "addColumn",
			"table": "users", "column": "email",
		},
	}
	doc := docOf("svc", op)
	ApplyMigrationSchemaOps(doc)
	assertModifies(t, doc, "t1", "svc", "users", "add_column", "email")
}

// TestAllographerAlterDropMigration proves a Nim Allographer SCOPE.Evolution
// migration-op entity (framework=allographer, #5029) derives a MODIFIES_TABLE
// edge for both a column-scoped alter op and a table-level drop.
func TestAllographerAlterDropMigration(t *testing.T) {
	add := graph.Entity{
		ID: "a1", Name: "add_column:users.bio", Kind: string(types.EntityKindEvolution),
		Subtype: "add_column",
		Properties: map[string]string{
			"framework": "allographer", "migration_op": "add_column",
			"table": "users", "column": "bio",
		},
	}
	drop := graph.Entity{
		ID: "a2", Name: "drop_table:posts", Kind: string(types.EntityKindEvolution),
		Subtype: "drop_table",
		Properties: map[string]string{
			"framework": "allographer", "migration_op": "drop_table", "table": "posts",
		},
	}
	doc := docOf("svc", add, drop)
	st := ApplyMigrationSchemaOps(doc)
	if st.Skipped {
		t.Fatal("skipped")
	}
	assertModifies(t, doc, "a1", "svc", "users", "add_column", "bio")
	assertModifies(t, doc, "a2", "svc", "posts", "drop_table", "")
}

// ---- Flyway / Liquibase SQL ------------------------------------------------

func TestFlywaySQLCreateTable(t *testing.T) {
	tbl := graph.Entity{
		ID: "f1", Name: "accounts", Kind: string(types.EntityKindDatastore),
		Subtype: "table",
		Properties: map[string]string{
			"migration_file": "V1__init.sql", "migration_order": "1",
		},
	}
	doc := docOf("svc", tbl)
	ApplyMigrationSchemaOps(doc)
	assertModifies(t, doc, "f1", "svc", "accounts", "create_table", "")
}

// ---- Convergence with query→table (ACCESSES_TABLE) -------------------------

func TestConvergenceMigrationAndQuerySameTableNode(t *testing.T) {
	// A migration add_column on `orders` ...
	mig := graph.Entity{
		ID: "m1", Name: "add_column:orders.total", Kind: string(types.EntityKindEvolution),
		Subtype: "add_column",
		Properties: map[string]string{
			"framework": "activerecord", "ddl_operation": "add_column",
			"table_name": "orders", "column_name": "total",
		},
	}
	// ... and a query SELECT on the SAME table (a SCOPE.DataAccess node).
	query := graph.Entity{
		ID: "q1", Name: "SELECT orders", Kind: "SCOPE.DataAccess",
		Properties: map[string]string{"table": "orders", "operation": "SELECT"},
	}
	doc := docOf("svc", mig, query)
	st := ApplyMigrationSchemaOps(doc)
	if st.Skipped {
		t.Fatal("skipped")
	}
	tableID := tableNodeFor(t, doc, "svc", "orders")

	// MODIFIES_TABLE points at the converged node.
	assertModifies(t, doc, "m1", "svc", "orders", "add_column", "total")

	// ACCESSES_TABLE from the query points at the SAME converged node.
	foundAccess := false
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind == sharedRelAccessTable && r.FromID == "q1" && r.ToID == tableID {
			foundAccess = true
		}
	}
	if !foundAccess {
		t.Fatalf("query SCOPE.DataAccess q1 not converged onto Table node %s", tableID)
	}
	if st.AccessConvergedEdges != 1 {
		t.Fatalf("AccessConvergedEdges=%d, want 1", st.AccessConvergedEdges)
	}
}

// ---- Negatives -------------------------------------------------------------

func TestDynamicTableSkipped(t *testing.T) {
	// Rails op whose table_name is dynamic/unknown → normTable("") → no edge.
	op := graph.Entity{
		ID: "n1", Name: "add_column", Kind: string(types.EntityKindEvolution),
		Subtype: "add_column",
		Properties: map[string]string{
			"framework": "activerecord", "ddl_operation": "add_column",
			"table_name": "UNKNOWN", "column_name": "x",
		},
	}
	doc := docOf("svc", op)
	st := ApplyMigrationSchemaOps(doc)
	if !st.Skipped {
		t.Fatalf("expected Skipped (no resolvable table), got edges=%d", st.ModifiesEdges)
	}
	for i := range doc.Relationships {
		if doc.Relationships[i].Kind == string(types.RelationshipKindModifiesTable) {
			t.Fatal("emitted MODIFIES_TABLE for a dynamic table")
		}
	}
}

func TestNonMigrationCreateTableLookalikeIgnored(t *testing.T) {
	// A SCOPE.Datastore table WITHOUT migration_file (e.g. a plain schema.sql
	// CREATE TABLE) must NOT be treated as a migration op.
	tbl := graph.Entity{
		ID: "x1", Name: "users", Kind: string(types.EntityKindDatastore),
		Subtype:    "table",
		Properties: map[string]string{}, // no migration_file
	}
	// And a SCOPE.Schema column NOT from alembic.
	col := graph.Entity{
		ID: "x2", Name: "users.id", Kind: string(types.EntityKindSchema),
		Properties: map[string]string{"framework": "sqlalchemy", "pattern_type": "column"},
	}
	doc := docOf("svc", tbl, col)
	st := ApplyMigrationSchemaOps(doc)
	if !st.Skipped {
		t.Fatalf("non-migration lookalikes produced %d edges, want 0", st.ModifiesEdges)
	}
}

// ---- Idempotency -----------------------------------------------------------

func TestIdempotentReRun(t *testing.T) {
	op := graph.Entity{
		ID: "i1", Name: "create_table:users", Kind: string(types.EntityKindEvolution),
		Subtype: "create_table",
		Properties: map[string]string{
			"framework": "knex", "migration_op": "createTable", "table": "users",
		},
	}
	doc := docOf("svc", op)
	ApplyMigrationSchemaOps(doc)
	ents1, rels1 := len(doc.Entities), len(doc.Relationships)
	ApplyMigrationSchemaOps(doc)
	if len(doc.Entities) != ents1 || len(doc.Relationships) != rels1 {
		t.Fatalf("re-run not idempotent: entities %d->%d rels %d->%d",
			ents1, len(doc.Entities), rels1, len(doc.Relationships))
	}
}
