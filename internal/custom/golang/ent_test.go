package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// entRecords runs the ent extractor and returns the raw records so property
// assertions are possible.
func entRecords(t *testing.T, name string) (sums []entitySummary, props map[string]map[string]string) {
	t.Helper()
	e, ok := extreg.Get("custom_go_ent")
	if !ok {
		t.Fatal("custom_go_ent not registered")
	}
	ents, err := e.Extract(context.Background(), fixtureInput(t, name, "go"))
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
// Models / schema_extraction (full): ent.Schema embed + field.X("col").
// ---------------------------------------------------------------------------

func TestEntSchemaAndFields(t *testing.T) {
	sums, props := entRecords(t, "ent_user.go")

	if !containsEntity(sums, "SCOPE.Schema", "User") {
		t.Error("expected User ent schema")
	}
	for _, col := range []string{"name", "email", "age", "created_at", "active"} {
		nm := "field:User." + col
		if !hasSubtype(sums, "SCOPE.Component", "field", nm) {
			t.Errorf("expected field %s", nm)
		}
		if props[nm]["column_name"] != col {
			t.Errorf("%s: column_name = %q, want %q", nm, props[nm]["column_name"], col)
		}
	}
	// SQL type mapping.
	if got := props["field:User.age"]["sql_type"]; got != "integer" {
		t.Errorf("age sql_type = %q, want integer", got)
	}
	if got := props["field:User.created_at"]["sql_type"]; got != "timestamp" {
		t.Errorf("created_at sql_type = %q, want timestamp", got)
	}
}

// ---------------------------------------------------------------------------
// Relationships (full): edge.To / edge.From.
// ---------------------------------------------------------------------------

func TestEntEdges(t *testing.T) {
	sums, props := entRecords(t, "ent_user.go")

	if !hasSubtype(sums, "SCOPE.Component", "relation", "rel:User.pets") {
		t.Error("expected rel:User.pets (edge.To)")
	}
	if got := props["rel:User.pets"]["relationship"]; got != "has_many" {
		t.Errorf("pets relationship = %q, want has_many", got)
	}
	if got := props["rel:User.pets"]["target_model"]; got != "Pet" {
		t.Errorf("pets target_model = %q, want Pet", got)
	}
	if !hasSubtype(sums, "SCOPE.Component", "relation", "rel:User.group") {
		t.Error("expected rel:User.group (edge.From)")
	}
	if got := props["rel:User.group"]["relationship"]; got != "belongs_to" {
		t.Errorf("group relationship = %q, want belongs_to", got)
	}
	if got := props["rel:User.group"]["target_model"]; got != "Group" {
		t.Errorf("group target_model = %q, want Group", got)
	}
}

// ---------------------------------------------------------------------------
// Queries (full): typed client builder attribution.
// ---------------------------------------------------------------------------

func TestEntQueries(t *testing.T) {
	sums, props := entRecords(t, "ent_client.go")

	if !hasSubtype(sums, "SCOPE.Operation", "query", "query:User.Query") {
		t.Error("expected query:User.Query")
	}
	if got := props["query:User.Query"]["model_name"]; got != "User" {
		t.Errorf("query:User.Query model_name = %q, want User", got)
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "query:User.Create") {
		t.Error("expected query:User.Create")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "query:Pet.Delete") {
		t.Error("expected query:Pet.Delete")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "query:Group.Get") {
		t.Error("expected query:Group.Get")
	}
	for _, verb := range []string{"Where", "Order", "Limit", "All"} {
		if !hasSubtype(sums, "SCOPE.Operation", "query", "chain:"+verb) {
			t.Errorf("expected chain:%s", verb)
		}
	}
}

// ---------------------------------------------------------------------------
// Migrations (full): Schema.Create auto-migration + migrate.With* options.
// ---------------------------------------------------------------------------

func TestEntMigrations(t *testing.T) {
	sums, _ := entRecords(t, "ent_client.go")

	if !hasSubtype(sums, "SCOPE.Operation", "migration", "migrate:schema_create") {
		t.Error("expected migrate:schema_create auto-migration")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "migration", "migrate_opt:WithForeignKeys") {
		t.Error("expected migrate_opt:WithForeignKeys")
	}
}

func TestEntNoMatch(t *testing.T) {
	e, _ := extreg.Get("custom_go_ent")
	ents, err := e.Extract(context.Background(), extreg.FileInput{Path: "main.go", Language: "go", Content: []byte("package main")})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
