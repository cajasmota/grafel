package javascript_test

// Tests for issue #5599: Kysely migration schema extractor
// (custom_js_kysely + custom_js_kysely_migrations). Proves schema_extraction,
// foreign_key_extraction, relationship_extraction, migration_parsing and
// migration_schema_ops for lang.jsts.orm.kysely from the imperative
// schema-builder DSL in Kysely migration modules.
//
// These are the proving fixtures cited by the registry.

import (
	"testing"
)

// goldenKyselyMigration exercises every extracted pattern: createTable,
// .addColumn column builders, an explicit .addForeignKeyConstraint FK, a
// column-level .references() FK, an alterTable, and a dropTable, all inside
// up()/down() migration modules. Generic table/column names.
const goldenKyselyMigration = `
import { Kysely, sql } from 'kysely'

export async function up(db: Kysely<any>): Promise<void> {
  await db.schema
    .createTable('account')
    .addColumn('id', 'serial', (cb) => cb.primaryKey())
    .addColumn('name', 'varchar(255)', (cb) => cb.notNull())
    .execute()

  await db.schema
    .createTable('entry')
    .addColumn('id', 'serial', (cb) => cb.primaryKey())
    .addColumn('title', 'text')
    .addColumn('owner_id', 'integer', (cb) => cb.references('account.id'))
    .addColumn('editor_id', 'integer')
    .addForeignKeyConstraint('fk_editor', ['editor_id'], 'account', ['id'])
    .execute()

  await db.schema.alterTable('account').addColumn('status', 'varchar(32)').execute()
}

export async function down(db: Kysely<any>): Promise<void> {
  await db.schema.dropTable('entry').execute()
  await db.schema.dropTable('account').execute()
}
`

func TestKyselyMigrationSchemaExtraction(t *testing.T) {
	ents := extract(t, "custom_js_kysely_migrations", fi("migrations/001_init.ts", "typescript", goldenKyselyMigration))
	if !containsEntity(ents, "SCOPE.Schema", "account") {
		t.Error("expected account table schema entity (schema_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Schema", "entry") {
		t.Error("expected entry table schema entity (schema_extraction)")
	}
}

func TestKyselyMigrationColumnExtraction(t *testing.T) {
	ents := extract(t, "custom_js_kysely_migrations", fi("migrations/001_init.ts", "typescript", goldenKyselyMigration))
	if !containsEntity(ents, "SCOPE.Component", "name") {
		t.Error("expected column entity 'name' (schema_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Component", "title") {
		t.Error("expected column entity 'title' (schema_extraction)")
	}
	if !containsSubtype(ents, "column") {
		t.Error("expected column subtype entities from Kysely .addColumn() builders")
	}
}

func TestKyselyMigrationForeignKeyConstraint(t *testing.T) {
	ents := extract(t, "custom_js_kysely_migrations", fi("migrations/001_init.ts", "typescript", goldenKyselyMigration))
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity (foreign_key_extraction)")
	}
	// Explicit .addForeignKeyConstraint('fk_editor', ['editor_id'], 'account', ['id']).
	if !containsEntity(ents, "SCOPE.Component", "fk:editor_id->account.id") {
		t.Error("expected fk:editor_id->account.id from .addForeignKeyConstraint")
	}
}

func TestKyselyMigrationColumnLevelForeignKey(t *testing.T) {
	ents := extract(t, "custom_js_kysely_migrations", fi("migrations/001_init.ts", "typescript", goldenKyselyMigration))
	// Column-level .references('account.id') on owner_id.
	if !containsEntity(ents, "SCOPE.Component", "fk:owner_id->account.id") {
		t.Error("expected fk:owner_id->account.id from column-level .references()")
	}
}

func TestKyselyMigrationRelationshipExtraction(t *testing.T) {
	ents := extract(t, "custom_js_kysely_migrations", fi("migrations/001_init.ts", "typescript", goldenKyselyMigration))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity derived from FK (relationship_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "relation:owner_id->account") {
		t.Error("expected relation:owner_id->account (relationship/association extraction)")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "relation:editor_id->account") {
		t.Error("expected relation:editor_id->account (relationship/association extraction)")
	}
}

// TestKyselyMigrationOps proves the base extractor emits the up()/down()
// entry points and the SCOPE.Evolution schema ops the #3628 engine pass
// converges to MODIFIES_TABLE (migration_parsing + migration_schema_ops).
func TestKyselyMigrationOps(t *testing.T) {
	ents := extract(t, "custom_js_kysely", fi("migrations/001_init.ts", "typescript", goldenKyselyMigration))
	if !containsEntity(ents, "SCOPE.Operation", "migration:up") {
		t.Error("expected migration:up entry-point entity (migration_parsing)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "migration:down") {
		t.Error("expected migration:down entry-point entity (migration_parsing)")
	}
	if !containsEntity(ents, "SCOPE.Evolution", "create_table:account") {
		t.Error("expected create_table:account evolution op (migration_schema_ops)")
	}
	if !containsEntity(ents, "SCOPE.Evolution", "alter_table:account") {
		t.Error("expected alter_table:account evolution op (migration_schema_ops)")
	}
	if !containsEntity(ents, "SCOPE.Evolution", "drop_table:entry") {
		t.Error("expected drop_table:entry evolution op (migration_schema_ops)")
	}
}

// TestKyselyMigrationNonMigrationNoop proves the extractors gate on Kysely-shaped
// sources and do not emit schema entities for arbitrary createTable/addColumn calls.
func TestKyselyMigrationNonMigrationNoop(t *testing.T) {
	src := `
const ui = makeBuilder()
ui.createTable('not-a-db-table')
ui.addColumn('not-a-column', 'x')
`
	ents := extract(t, "custom_js_kysely_migrations", fi("ui/widget.ts", "typescript", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-Kysely source, got %d", len(ents))
	}
	ents2 := extract(t, "custom_js_kysely", fi("ui/widget.ts", "typescript", src))
	if len(ents2) != 0 {
		t.Errorf("expected no base entities for non-Kysely source, got %d", len(ents2))
	}
}
