package javascript_test

// Tests for issue #3187: Knex migration extractor (custom_js_knex_migrations).
// Proves schema_extraction, foreign_key_extraction, association_extraction and
// relationship_extraction for lang.jsts.orm.knex from the schema-builder DSL in
// Knex migration files.
//
// These are the *proving fixtures* cited by the registry. They live in a
// dedicated _test.go file (not the shared extractors_test.go).

import (
	"testing"
)

// goldenKnexMigration is a representative Knex migration exercising every
// extracted pattern: createTable, column builders, an inline .references()
// .inTable() FK, and an explicit .foreign().references().inTable() FK.
const goldenKnexMigration = `
exports.up = function (knex) {
  return knex.schema
    .createTable('users', (table) => {
      table.increments('id').primary()
      table.string('name').notNullable()
      table.string('email').unique()
    })
    .createTable('posts', (table) => {
      table.increments('id').primary()
      table.string('title')
      table.text('body')
      table.integer('author_id').references('id').inTable('users')
      table.integer('editor_id')
      table.foreign('editor_id').references('id').inTable('users')
    })
}

exports.down = function (knex) {
  return knex.schema.dropTableIfExists('posts').dropTableIfExists('users')
}
`

func TestKnexMigrationSchemaExtraction(t *testing.T) {
	ents := extract(t, "custom_js_knex_migrations", fi("migrations/001_init.js", "javascript", goldenKnexMigration))
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Error("expected users table schema entity (schema_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Schema", "posts") {
		t.Error("expected posts table schema entity (schema_extraction)")
	}
}

func TestKnexMigrationColumnExtraction(t *testing.T) {
	ents := extract(t, "custom_js_knex_migrations", fi("migrations/001_init.js", "javascript", goldenKnexMigration))
	if !containsEntity(ents, "SCOPE.Component", "name") {
		t.Error("expected column entity 'name' (schema_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Component", "email") {
		t.Error("expected column entity 'email' (schema_extraction)")
	}
	if !containsSubtype(ents, "column") {
		t.Error("expected column subtype entities from Knex column builders")
	}
}

func TestKnexMigrationForeignKeyInline(t *testing.T) {
	ents := extract(t, "custom_js_knex_migrations", fi("migrations/001_init.js", "javascript", goldenKnexMigration))
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity from .references().inTable() chain (foreign_key_extraction)")
	}
	// The inline FK should resolve the local column author_id and ref table users.
	if !containsEntity(ents, "SCOPE.Component", "fk:author_id->id") {
		t.Error("expected fk:author_id->id foreign_key entity for inline .references('id').inTable('users')")
	}
}

func TestKnexMigrationForeignKeyExplicit(t *testing.T) {
	ents := extract(t, "custom_js_knex_migrations", fi("migrations/001_init.js", "javascript", goldenKnexMigration))
	// The explicit t.foreign('editor_id').references('id').inTable('users') FK.
	if !containsEntity(ents, "SCOPE.Component", "fk:editor_id->id") {
		t.Error("expected fk:editor_id->id foreign_key entity for explicit .foreign().references().inTable()")
	}
}

func TestKnexMigrationRelationshipExtraction(t *testing.T) {
	ents := extract(t, "custom_js_knex_migrations", fi("migrations/001_init.js", "javascript", goldenKnexMigration))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity derived from FK (relationship_extraction)")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "relation:author_id->users") {
		t.Error("expected relation:author_id->users entity (association_extraction + relationship_extraction)")
	}
}

func TestKnexMigrationAssociationExtraction(t *testing.T) {
	ents := extract(t, "custom_js_knex_migrations", fi("migrations/001_init.js", "javascript", goldenKnexMigration))
	// The association between posts.editor_id and users is the same FK-derived relation.
	if !containsEntity(ents, "SCOPE.Pattern", "relation:editor_id->users") {
		t.Error("expected relation:editor_id->users entity (association_extraction)")
	}
}

// TestKnexMigrationQualifiedReference proves the single-arg qualified
// .references('users.id') spelling is also resolved.
func TestKnexMigrationQualifiedReference(t *testing.T) {
	src := `
exports.up = (knex) =>
  knex.schema.createTable('comments', (table) => {
    table.increments('id')
    table.integer('post_id').references('posts.id')
  })
`
	ents := extract(t, "custom_js_knex_migrations", fi("migrations/002_comments.ts", "typescript", src))
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity for qualified .references('posts.id')")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "relation:post_id->posts") {
		t.Error("expected relation:post_id->posts entity for qualified reference")
	}
}

// TestKnexMigrationNonMigrationNoop proves the extractor gates on Knex-shaped
// sources and does not emit schema entities for arbitrary table.* calls.
func TestKnexMigrationNonMigrationNoop(t *testing.T) {
	src := `
const table = document.querySelector('table')
table.string('not-a-column')
`
	ents := extract(t, "custom_js_knex_migrations", fi("ui/widget.ts", "typescript", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-Knex source, got %d", len(ents))
	}
}
