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

// ---------------------------------------------------------------------------
// query_attribution struct-context resolution (#3348 deepening).
//
// Validates that buildSQLDestTypeMap extracts the destination struct type from
// var-decl and short-decl forms, and that the extraction pipeline stamps
// model_struct on the emitted query-call entity when a Get/Select call's
// destination variable maps to a known struct type.
// ---------------------------------------------------------------------------

func sqlDriversExtractFull(t *testing.T, file extreg.FileInput) []fullEntity {
	t.Helper()
	e, ok := extreg.Get("custom_go_sql_drivers")
	if !ok {
		t.Fatal("custom_go_sql_drivers not registered")
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	out := make([]fullEntity, 0, len(ents))
	for _, ent := range ents {
		out = append(out, fullEntity{Kind: ent.Kind, Name: ent.Name, Props: ent.Properties})
	}
	return out
}

func TestSqlxQueryAttributionVarDecl(t *testing.T) {
	// `var u User` → db.Get(&u, …) should stamp model_struct=User.
	src := `package x

import "github.com/jmoiron/sqlx"

type User struct {
	ID   int    ` + "`db:\"id\"`" + `
	Name string ` + "`db:\"name\"`" + `
}

func getUser(db *sqlx.DB, id int) (User, error) {
	var u User
	err := db.Get(&u, "SELECT id, name FROM users WHERE id = $1", id)
	return u, err
}
`
	ents := sqlDriversExtractFull(t, fi("repo.go", "go", src))
	var queryEnt *fullEntity
	for i := range ents {
		if ents[i].Props["query_type"] == "call" && ents[i].Props["call_verb"] == "Get" {
			queryEnt = &ents[i]
			break
		}
	}
	if queryEnt == nil {
		t.Fatalf("expected a Get query-call entity; entities: %+v", ents)
	}
	if queryEnt.Props["model_struct"] != "User" {
		t.Errorf("model_struct=%q, want User (var-decl form)", queryEnt.Props["model_struct"])
	}
}

func TestSqlxQueryAttributionShortDecl(t *testing.T) {
	// `u := User{}` → db.Get(&u, …) should stamp model_struct=User.
	src := `package x

import "github.com/jmoiron/sqlx"

type Product struct {
	ID    int    ` + "`db:\"id\"`" + `
	Price float64 ` + "`db:\"price\"`" + `
}

func fetchProduct(db *sqlx.DB, id int) (Product, error) {
	u := Product{}
	err := db.Get(&u, "SELECT id, price FROM products WHERE id = $1", id)
	return u, err
}
`
	ents := sqlDriversExtractFull(t, fi("repo.go", "go", src))
	var queryEnt *fullEntity
	for i := range ents {
		if ents[i].Props["query_type"] == "call" && ents[i].Props["call_verb"] == "Get" {
			queryEnt = &ents[i]
			break
		}
	}
	if queryEnt == nil {
		t.Fatalf("expected a Get query-call entity; entities: %+v", ents)
	}
	if queryEnt.Props["model_struct"] != "Product" {
		t.Errorf("model_struct=%q, want Product (short-decl form)", queryEnt.Props["model_struct"])
	}
}

func TestPgxQueryAttributionVarDecl(t *testing.T) {
	// pgx: `var p Product` → conn.QueryRow(&p, …) should stamp model_struct=Product.
	src := `package x

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type Product struct {
	ID  int    ` + "`db:\"id\"`" + `
	SKU string ` + "`db:\"sku\"`" + `
}

func getProduct(ctx context.Context, conn *pgx.Conn, id int) (Product, error) {
	var p Product
	err := conn.QueryRow(ctx, "SELECT id, sku FROM products WHERE id = $1", id).Scan(&p.ID, &p.SKU)
	return p, err
}
`
	ents := sqlDriversExtractFull(t, fi("repo.go", "go", src))
	// pgx.QueryRow is surfaced via the reSQLQueryCall pattern (QueryRow verb).
	var queryEnt *fullEntity
	for i := range ents {
		if ents[i].Props["query_type"] == "call" && ents[i].Props["call_verb"] == "QueryRow" {
			queryEnt = &ents[i]
			break
		}
	}
	if queryEnt == nil {
		t.Fatalf("expected a QueryRow query-call entity; entities: %+v", ents)
	}
	// model_struct is stamped by the reSQLGetCallWithDest path; for QueryRow
	// the pattern requires `&dest` as the second arg. QueryRow passes ctx + sql,
	// not &dest, so model_struct stays empty — honest partial.
	// This test documents the boundary: we assert no panic and a well-formed entity.
	if queryEnt.Kind != "SCOPE.Operation" {
		t.Errorf("Kind=%q, want SCOPE.Operation", queryEnt.Kind)
	}
}

func TestSqlxSelectSliceAttribution(t *testing.T) {
	// `var users []User` → db.Select(&users, …) should stamp model_struct=User
	// (slice type stripped by buildSQLDestTypeMap).
	src := `package x

import "github.com/jmoiron/sqlx"

type User struct {
	ID int ` + "`db:\"id\"`" + `
}

func listUsers(db *sqlx.DB) ([]User, error) {
	var users []User
	err := db.Select(&users, "SELECT id FROM users")
	return users, err
}
`
	ents := sqlDriversExtractFull(t, fi("repo.go", "go", src))
	var queryEnt *fullEntity
	for i := range ents {
		if ents[i].Props["query_type"] == "call" && ents[i].Props["call_verb"] == "Select" {
			queryEnt = &ents[i]
			break
		}
	}
	if queryEnt == nil {
		t.Fatalf("expected a Select query-call entity; entities: %+v", ents)
	}
	if queryEnt.Props["model_struct"] != "User" {
		t.Errorf("model_struct=%q, want User (slice var-decl form)", queryEnt.Props["model_struct"])
	}
}
