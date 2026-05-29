package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

func sqlDriversExtract(t *testing.T, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get("custom_go_sql_drivers")
	if !ok {
		t.Fatal("custom_go_sql_drivers not registered")
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	out := make([]entitySummary, 0, len(ents))
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

// ---------------------------------------------------------------------------
// sqlx: Models (db: tags) + Schema columns + queries + FK from CREATE TABLE.
// ---------------------------------------------------------------------------

func TestSqlxModelsAndQueries(t *testing.T) {
	ents := sqlDriversExtract(t, fixtureInput(t, "sqlx_models.go", "go"))

	// Models: structs with db: tags are schemas.
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User schema from db: tags")
	}
	if !containsEntity(ents, "SCOPE.Schema", "Order") {
		t.Error("expected Order schema from db: tags")
	}
	// Schema columns.
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:User.Name") {
		t.Error("expected User.Name field component")
	}
	// db:"email,omitempty" -> column email (option stripped).
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:User.Email") {
		t.Error("expected User.Email field component")
	}
	// db:"-" must NOT produce a column.
	if hasSubtype(ents, "SCOPE.Component", "field", "field:User.Skip") {
		t.Error("did not expect User.Skip column (db:\"-\")")
	}

	// Queries: at least one SQL-literal-derived query operation.
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected at least one query operation")
	}

	// CREATE TABLE in a backquoted literal -> table schema + FK relation.
	if !containsEntity(ents, "SCOPE.Schema", "table:orders") {
		t.Error("expected orders table schema from CREATE TABLE")
	}
	if !hasSubtype(ents, "SCOPE.Component", "relation", "fk:orders.user_id") {
		t.Error("expected FK relation orders.user_id -> users")
	}
}

// ---------------------------------------------------------------------------
// pgx: db: tag model + Exec/Query/QueryRow call-site queries.
// ---------------------------------------------------------------------------

func TestPgxModelsAndQueries(t *testing.T) {
	ents := sqlDriversExtract(t, fixtureInput(t, "pgx_queries.go", "go"))

	if !containsEntity(ents, "SCOPE.Schema", "Product") {
		t.Error("expected Product schema from db: tags")
	}
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:Product.SKU") {
		t.Error("expected Product.SKU field component")
	}
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected query operations for pgx")
	}
}

// ---------------------------------------------------------------------------
// sqlite: db: tag model + CREATE TABLE FK + Exec/Query queries.
// ---------------------------------------------------------------------------

func TestSqliteModelsSchemaAndFK(t *testing.T) {
	ents := sqlDriversExtract(t, fixtureInput(t, "sqlite_store.go", "go"))

	if !containsEntity(ents, "SCOPE.Schema", "Note") {
		t.Error("expected Note schema from db: tags")
	}
	if !containsEntity(ents, "SCOPE.Schema", "table:notes") {
		t.Error("expected notes table schema from CREATE TABLE")
	}
	if !hasSubtype(ents, "SCOPE.Component", "relation", "fk:notes.author_id") {
		t.Error("expected FK relation notes.author_id -> authors")
	}
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected query operations for sqlite")
	}
}

// ---------------------------------------------------------------------------
// Migrations: file-based NNN_slug.up/down.sql (driver-agnostic, lang=sql).
// ---------------------------------------------------------------------------

func TestSqlDriverMigrationFiles(t *testing.T) {
	up := sqlDriversExtract(t, fixtureInput(t, "000123_create_users.up.sql", "sql"))
	if !hasSubtype(up, "SCOPE.Schema", "migration", "migration:000123_create_users.up") {
		t.Error("expected up migration schema entity")
	}
	down := sqlDriversExtract(t, fixtureInput(t, "000123_create_users.down.sql", "sql"))
	if !hasSubtype(down, "SCOPE.Schema", "migration", "migration:000123_create_users.down") {
		t.Error("expected down migration schema entity")
	}
}

// ---------------------------------------------------------------------------
// Negative: a Go file importing no recognised SQL driver yields nothing,
// proving the import gate and that we never poach gorm/other files.
// ---------------------------------------------------------------------------

func TestSqlDriverImportGate(t *testing.T) {
	src := `package x

type Thing struct {
	ID int ` + "`db:\"id\"`" + `
}

func run() { _ = "SELECT 1" }
`
	file := extreg.FileInput{Path: "no_driver.go", Language: "go", Content: []byte(src)}
	ents := sqlDriversExtract(t, file)
	if len(ents) != 0 {
		t.Errorf("expected no entities without a driver import, got %d", len(ents))
	}
}

// hasKindSubtype reports whether any entity matches kind+subtype (name-agnostic).
func hasKindSubtype(ents []entitySummary, kind, subtype string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Subtype == subtype {
			return true
		}
	}
	return false
}
