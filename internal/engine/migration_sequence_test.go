package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// makeMigEntity is a tiny helper for building a migration-anchored entity.
func makeMigEntity(id, sourceFile string) graph.Entity {
	return graph.Entity{ID: id, Name: id, Kind: "Migration", SourceFile: sourceFile}
}

// TestApplyMigrationSequence_DjangoSequenceAndName asserts the Django value
// signal: 0002_add_email.py → sequence_number=2, migration_name="add email".
func TestApplyMigrationSequence_DjangoSequenceAndName(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		makeMigEntity("d1", "app/migrations/0002_add_email.py"),
	}}
	stats := ApplyMigrationSequence(doc, nil)
	if stats.EntitiesAnnotated != 1 || stats.FilesMatched != 1 {
		t.Fatalf("expected 1 annotated/1 file, got %+v", stats)
	}
	p := doc.Entities[0].PropsSnapshot()
	if p["sequence_number"] != "2" {
		t.Fatalf("expected sequence_number=2, got %q", p["sequence_number"])
	}
	if p["migration_name"] != "add email" {
		t.Fatalf("expected migration_name='add email', got %q", p["migration_name"])
	}
	if p["migration_pattern"] != "django" {
		t.Fatalf("expected migration_pattern=django, got %q", p["migration_pattern"])
	}
}

// TestApplyMigrationSequence_FlywaySequence asserts Flyway V3__add_index.sql →
// sequence 3. Flyway sequence is a (possibly dotted) string.
func TestApplyMigrationSequence_FlywaySequence(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "f1", Name: "products", Kind: "SCOPE.Datastore", SourceFile: "db/migrations/V3__add_index.sql"},
	}}
	stats := ApplyMigrationSequence(doc, nil)
	if stats.EntitiesAnnotated != 1 {
		t.Fatalf("expected 1 annotated, got %+v", stats)
	}
	p := doc.Entities[0].PropsSnapshot()
	if p["sequence_number"] != "3" {
		t.Fatalf("expected sequence_number=3, got %q", p["sequence_number"])
	}
	if p["migration_name"] != "add index" {
		t.Fatalf("expected migration_name='add index', got %q", p["migration_name"])
	}
	if p["migration_pattern"] != "flyway" {
		t.Fatalf("expected migration_pattern=flyway, got %q", p["migration_pattern"])
	}
}

// TestApplyMigrationSequence_RailsAndGolangMigrate asserts the Rails timestamp
// sequence and the golang-migrate numeric sequence in one document.
func TestApplyMigrationSequence_RailsAndGolangMigrate(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		makeMigEntity("r1", "db/migrate/20231101120000_create_users.rb"),
		{ID: "g1", Name: "users", Kind: "SCOPE.Datastore", SourceFile: "migrations/000005_add_orders.up.sql"},
	}}
	ApplyMigrationSequence(doc, nil)

	rails := doc.Entities[0].PropsSnapshot()
	if rails["sequence_number"] != "20231101120000" || rails["migration_pattern"] != "rails" {
		t.Fatalf("rails: got seq=%q pattern=%q", rails["sequence_number"], rails["migration_pattern"])
	}
	if rails["migration_name"] != "create users" {
		t.Fatalf("rails: expected 'create users', got %q", rails["migration_name"])
	}

	gm := doc.Entities[1].PropsSnapshot()
	if gm["sequence_number"] != "5" || gm["migration_pattern"] != "golang_migrate" {
		t.Fatalf("golang-migrate: got seq=%q pattern=%q", gm["sequence_number"], gm["migration_pattern"])
	}
	if gm["migration_name"] != "add orders" {
		t.Fatalf("golang-migrate: expected 'add orders', got %q", gm["migration_name"])
	}
}

// TestApplyMigrationSequence_AlembicPrecedesEdge asserts the Alembic ordering
// signal: when the child migration declares down_revision = parent, the pass
// emits exactly one PRECEDES edge parent → child, and leaves the base migration
// (down_revision = None) with no incoming edge.
func TestApplyMigrationSequence_AlembicPrecedesEdge(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		makeMigEntity("base", "alembic/versions/aaaaaaaaaaaa_initial.py"),
		makeMigEntity("child", "alembic/versions/bbbbbbbbbbbb_add_col.py"),
	}}

	// In-memory reader: base has down_revision=None; child points back to base.
	src := map[string]string{
		"alembic/versions/aaaaaaaaaaaa_initial.py": "revision = 'aaaaaaaaaaaa'\ndown_revision = None\n",
		"alembic/versions/bbbbbbbbbbbb_add_col.py": "revision = \"bbbbbbbbbbbb\"\ndown_revision = \"aaaaaaaaaaaa\"\n",
	}
	reader := func(f string) (string, bool) { s, ok := src[f]; return s, ok }

	stats := ApplyMigrationSequence(doc, reader)
	if stats.PrecedesEdges != 1 {
		t.Fatalf("expected exactly 1 PRECEDES edge, got %d (stats=%+v)", stats.PrecedesEdges, stats)
	}

	// Find the PRECEDES edge and assert its direction: base PRECEDES child.
	var found *graph.Relationship
	for i := range doc.Relationships {
		if doc.Relationships[i].Kind == "PRECEDES" {
			found = &doc.Relationships[i]
		}
	}
	if found == nil {
		t.Fatal("no PRECEDES edge emitted")
	}
	if found.FromID != "base" || found.ToID != "child" {
		t.Fatalf("expected base→child, got %s→%s", found.FromID, found.ToID)
	}

	// Revision props were stamped from the file body.
	if doc.Entities[1].PropGet("down_revision") != "aaaaaaaaaaaa" {
		t.Fatalf("expected child down_revision=aaaaaaaaaaaa, got %q",
			doc.Entities[1].PropGet("down_revision"))
	}

	// Idempotency: a second run adds no new edge.
	before := len(doc.Relationships)
	ApplyMigrationSequence(doc, reader)
	if len(doc.Relationships) != before {
		t.Fatalf("second run added edges: before=%d after=%d", before, len(doc.Relationships))
	}
}

// TestApplyMigrationSequence_NoMigrationsSkips asserts honest behaviour: a graph
// with no migration files annotates nothing and emits no edges.
func TestApplyMigrationSequence_NoMigrationsSkips(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "x", Name: "handler", Kind: "Operation", SourceFile: "internal/api/handler.go"},
	}}
	stats := ApplyMigrationSequence(doc, nil)
	if stats.EntitiesAnnotated != 0 || stats.PrecedesEdges != 0 {
		t.Fatalf("expected no annotation, got %+v", stats)
	}
	if _, ok := doc.Entities[0].PropLookup("sequence_number"); ok {
		t.Fatal("non-migration entity should not be stamped")
	}
}
