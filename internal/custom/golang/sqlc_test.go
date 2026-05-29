package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// sqlcRecords runs the sqlc extractor against a testdata fixture and returns
// summaries plus per-name properties for assertions.
func sqlcRecords(t *testing.T, name, lang string) (sums []entitySummary, props map[string]map[string]string) {
	t.Helper()
	e, ok := extreg.Get("custom_go_sqlc")
	if !ok {
		t.Fatal("custom_go_sqlc not registered")
	}
	ents, err := e.Extract(context.Background(), fixtureInput(t, name, lang))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	props = map[string]map[string]string{}
	for _, ent := range ents {
		sums = append(sums, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
		props[ent.Name] = ent.Properties
	}
	return sums, props
}

// ---------------------------------------------------------------------------
// Models (partial): generated structs + fields from a sqlc-generated file.
// ---------------------------------------------------------------------------

func TestSqlcModels(t *testing.T) {
	sums, props := sqlcRecords(t, "sqlc_models.go", "go")

	if !containsEntity(sums, "SCOPE.Schema", "Author") {
		t.Error("expected Author model")
	}
	if !containsEntity(sums, "SCOPE.Schema", "Book") {
		t.Error("expected Book model")
	}
	// The Queries holder must NOT be treated as a model.
	if containsEntity(sums, "SCOPE.Schema", "Queries") {
		t.Error("Queries holder should not be a model")
	}
	for _, f := range []string{"Author.ID", "Author.Name", "Author.CreatedAt", "Book.AuthorID"} {
		nm := "field:" + f
		if !hasSubtype(sums, "SCOPE.Component", "field", nm) {
			t.Errorf("expected field %s", nm)
		}
	}
	// snake_case column recovery.
	if got := props["field:Author.CreatedAt"]["column_name"]; got != "created_at" {
		t.Errorf("CreatedAt column_name = %q, want created_at", got)
	}
	if got := props["field:Book.AuthorID"]["column_name"]; got != "author_i_d" {
		// AuthorID -> author_i_d under naive de-camel; documents the heuristic.
		t.Errorf("AuthorID column_name = %q, want author_i_d", got)
	}
}

// A non-generated Go file with structs must NOT yield sqlc models.
func TestSqlcNoModelWithoutHeader(t *testing.T) {
	e, _ := extreg.Get("custom_go_sqlc")
	ents, _ := e.Extract(context.Background(), extreg.FileInput{
		Path: "plain.go", Language: "go",
		Content: []byte("package p\ntype Author struct {\n\tID int\n}\n"),
	})
	for _, ent := range ents {
		if ent.Kind == "SCOPE.Schema" && ent.Subtype == "" {
			t.Errorf("unexpected model %q from non-generated file", ent.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Queries (partial): -- name: X :verb annotations + generated *Queries funcs.
// ---------------------------------------------------------------------------

func TestSqlcQueryAnnotations(t *testing.T) {
	sums, props := sqlcRecords(t, "sqlc_query.sql", "sql")

	if !hasSubtype(sums, "SCOPE.Operation", "query", "query:GetAuthor") {
		t.Error("expected query:GetAuthor")
	}
	if got := props["query:GetAuthor"]["result_kind"]; got != "one" {
		t.Errorf("GetAuthor result_kind = %q, want one", got)
	}
	if got := props["query:ListBooks"]["result_kind"]; got != "many" {
		t.Errorf("ListBooks result_kind = %q, want many", got)
	}
	if got := props["query:CreateAuthor"]["result_kind"]; got != "execresult" {
		t.Errorf("CreateAuthor result_kind = %q, want execresult", got)
	}
}

func TestSqlcQueryFuncs(t *testing.T) {
	sums, props := sqlcRecords(t, "sqlc_queries.go", "go")

	if !hasSubtype(sums, "SCOPE.Operation", "query", "query_func:GetAuthor") {
		t.Error("expected query_func:GetAuthor")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "query_func:DeleteAuthor") {
		t.Error("expected query_func:DeleteAuthor")
	}
	if got := props["query_func:GetAuthor"]["generated"]; got != "true" {
		t.Errorf("GetAuthor generated = %q, want true", got)
	}
	// The const SQL inside the generated file also carries annotations.
	if !hasSubtype(sums, "SCOPE.Operation", "query", "query:GetAuthor") {
		t.Error("expected annotation query:GetAuthor from generated const")
	}
}

// ---------------------------------------------------------------------------
// Config + Migrations (partial): sqlc.yaml + NNN_slug.up/down.sql.
// ---------------------------------------------------------------------------

func TestSqlcConfig(t *testing.T) {
	sums, props := sqlcRecords(t, "sqlc.yaml", "yaml")
	if !hasSubtype(sums, "SCOPE.Schema", "config", "sqlc_config:sqlc.yaml") {
		t.Error("expected sqlc config entity")
	}
	if got := props["sqlc_config:sqlc.yaml"]["has_queries"]; got != "true" {
		t.Errorf("has_queries = %q, want true", got)
	}
}

func TestSqlcMigrationFile(t *testing.T) {
	e, _ := extreg.Get("custom_go_sqlc")
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: "db/migrations/000123_create_users.up.sql", Language: "sql",
		Content: []byte("CREATE TABLE users (id bigserial primary key);"),
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ent := range ents {
		if ent.Subtype == "migration" && ent.Name == "migration:000123_create_users.up" {
			found = true
			if ent.Properties["migration_direction"] != "up" {
				t.Errorf("direction = %q, want up", ent.Properties["migration_direction"])
			}
		}
	}
	if !found {
		t.Error("expected sqlc migration entity")
	}
}

func TestSqlcNoMatch(t *testing.T) {
	e, _ := extreg.Get("custom_go_sqlc")
	ents, err := e.Extract(context.Background(), extreg.FileInput{Path: "main.go", Language: "go", Content: []byte("package main")})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
