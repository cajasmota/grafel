package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

func bunRecords(t *testing.T, name, lang string) (sums []entitySummary, props map[string]map[string]string) {
	t.Helper()
	e, ok := extreg.Get("custom_go_bun")
	if !ok {
		t.Fatal("custom_go_bun not registered")
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
// Models / schema_extraction (full): bun.BaseModel + table tag + db columns.
// ---------------------------------------------------------------------------

func TestBunModelsAndFields(t *testing.T) {
	sums, props := bunRecords(t, "bun_models.go", "go")

	for _, m := range []string{"User", "Story", "Profile"} {
		if !containsEntity(sums, "SCOPE.Schema", m) {
			t.Errorf("expected %s schema", m)
		}
	}
	if got := props["User"]["table_name"]; got != "users" {
		t.Errorf("User table_name = %q, want users", got)
	}
	if got := props["Story"]["table_name"]; got != "stories" {
		t.Errorf("Story table_name = %q, want stories", got)
	}

	if !hasSubtype(sums, "SCOPE.Component", "field", "field:User.Name") {
		t.Error("expected field:User.Name")
	}
	if got := props["field:User.Name"]["column_name"]; got != "name" {
		t.Errorf("User.Name column = %q, want name", got)
	}
	if got := props["field:User.CreatedAt"]["column_name"]; got != "created_at" {
		t.Errorf("User.CreatedAt column = %q, want created_at", got)
	}
}

// ---------------------------------------------------------------------------
// Relationships (full): rel: + m2m: tags.
// ---------------------------------------------------------------------------

func TestBunRelationships(t *testing.T) {
	sums, props := bunRecords(t, "bun_models.go", "go")

	cases := map[string]struct{ rel, target string }{
		"rel:User.Stories": {"has_many", "Story"},
		"rel:User.Profile": {"belongs_to", "Profile"},
		"rel:User.Roles":   {"many2many", "Role"},
	}
	for name, want := range cases {
		if !hasSubtype(sums, "SCOPE.Component", "relation", name) {
			t.Errorf("expected relation %s", name)
			continue
		}
		if got := props[name]["relationship"]; got != want.rel {
			t.Errorf("%s relationship = %q, want %q", name, got, want.rel)
		}
		if got := props[name]["target_model"]; got != want.target {
			t.Errorf("%s target_model = %q, want %q", name, got, want.target)
		}
	}
	if got := props["rel:User.Roles"]["join_table"]; got != "user_roles" {
		t.Errorf("Roles join_table = %q, want user_roles", got)
	}
	// A relationship field must NOT also be emitted as a scalar column.
	if hasSubtype(sums, "SCOPE.Component", "field", "field:User.Stories") {
		t.Error("Stories should be a relation, not a field")
	}
}

// ---------------------------------------------------------------------------
// Queries (full): builder entry points + .Model() attribution.
// ---------------------------------------------------------------------------

func TestBunQueries(t *testing.T) {
	sums, props := bunRecords(t, "bun_queries.go", "go")

	for _, verb := range []string{"NewSelect", "NewInsert", "NewUpdate", "NewDelete", "NewCreateTable"} {
		if !hasSubtype(sums, "SCOPE.Operation", "query", "query:"+verb) {
			t.Errorf("expected query:%s", verb)
		}
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "model_query:User") {
		t.Error("expected model_query:User (.Model(&users) bound)")
	}
	if got := props["model_query:User"]["model_name"]; got != "User" {
		t.Errorf("model_query:User model_name = %q, want User", got)
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "model_query:Story") {
		t.Error("expected model_query:Story ((*Story)(nil) bound)")
	}
}

// ---------------------------------------------------------------------------
// Migrations (full): registry + register + file-based.
// ---------------------------------------------------------------------------

func TestBunMigrationsProgrammatic(t *testing.T) {
	sums, _ := bunRecords(t, "bun_migrate.go", "go")

	if !hasSubtype(sums, "SCOPE.Operation", "migration", "migrations:registry") {
		t.Error("expected migrations:registry")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "migration", "migrate_register:MustRegister") {
		t.Error("expected migrate_register:MustRegister")
	}
}

func TestBunMigrationsFileBased(t *testing.T) {
	up, _ := bunRecords(t, "20240101_create_users.up.sql", "sql")
	if !hasSubtype(up, "SCOPE.Schema", "migration", "migration:20240101_create_users.up") {
		t.Error("expected up migration schema entity")
	}
	down, _ := bunRecords(t, "20240101_create_users.down.sql", "sql")
	if !hasSubtype(down, "SCOPE.Schema", "migration", "migration:20240101_create_users.down") {
		t.Error("expected down migration schema entity")
	}
}

func TestBunNoMatch(t *testing.T) {
	e, _ := extreg.Get("custom_go_bun")
	ents, err := e.Extract(context.Background(), extreg.FileInput{Path: "main.go", Language: "go", Content: []byte("package main")})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
