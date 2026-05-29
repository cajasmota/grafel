package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

func entExtract(t *testing.T, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get("custom_go_ent")
	if !ok {
		t.Fatal("custom_go_ent not registered")
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
// Models: ent.Schema struct + field.<Type>("name") columns (full)
// ---------------------------------------------------------------------------

func TestEntSchemaModelsFull(t *testing.T) {
	ents := entExtract(t, fixtureInput(t, "ent_schema_user.go", "go"))

	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User schema from ent.Schema embed")
	}
	for _, col := range []string{"name", "email", "age", "created_at"} {
		if !hasSubtype(ents, "SCOPE.Component", "field", "field:User."+col) {
			t.Errorf("expected User.%s field column", col)
		}
	}
}

// ---------------------------------------------------------------------------
// Relationships: edge.To / edge.From with direction + cardinality (full)
// ---------------------------------------------------------------------------

func TestEntEdgeRelationships(t *testing.T) {
	ents := entExtract(t, fixtureInput(t, "ent_schema_user.go", "go"))

	// to-many edge.To("posts", ...) -> has_many
	if !hasSubtype(ents, "SCOPE.Component", "relation", "rel:User.posts") {
		t.Error("expected User.posts has_many relation")
	}
	// to-one edge.To("profile", ...).Unique() -> has_one
	if !hasSubtype(ents, "SCOPE.Component", "relation", "rel:User.profile") {
		t.Error("expected User.profile has_one relation")
	}
	// inverse edge.From("company", ...).Ref("users").Unique() -> belongs_to
	if !hasSubtype(ents, "SCOPE.Component", "relation", "rel:User.company") {
		t.Error("expected User.company belongs_to relation")
	}
}

func TestEntRelationshipClassification(t *testing.T) {
	e, _ := extreg.Get("custom_go_ent")
	ents, err := e.Extract(context.Background(), fixtureInput(t, "ent_schema_user.go", "go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		rel    string
		target string
		dir    string
	}{
		"rel:User.posts":   {"has_many", "Post", "to"},
		"rel:User.profile": {"has_one", "Profile", "to"},
		"rel:User.company": {"belongs_to", "Company", "from"},
	}
	for _, ent := range ents {
		w, ok := want[ent.Name]
		if !ok {
			continue
		}
		if got := ent.Properties["relationship"]; got != w.rel {
			t.Errorf("%s relationship = %q, want %q", ent.Name, got, w.rel)
		}
		if got := ent.Properties["target_model"]; got != w.target {
			t.Errorf("%s target_model = %q, want %q", ent.Name, got, w.target)
		}
		if got := ent.Properties["edge_direction"]; got != w.dir {
			t.Errorf("%s edge_direction = %q, want %q", ent.Name, got, w.dir)
		}
		delete(want, ent.Name)
	}
	for name := range want {
		t.Errorf("missing relation entity %s", name)
	}
}

// ---------------------------------------------------------------------------
// Queries: generated typed builder usage (full)
// ---------------------------------------------------------------------------

func TestEntQueries(t *testing.T) {
	ents := entExtract(t, fixtureInput(t, "ent_usage.go", "go"))

	cases := []string{
		"query:User.Query",
		"query:User.Create",
		"query:Post.Update",
		"query:Profile.Delete",
		"query:Company.Get",
	}
	for _, c := range cases {
		if !hasSubtype(ents, "SCOPE.Operation", "query", c) {
			t.Errorf("expected ent query %s", c)
		}
	}
}

// ---------------------------------------------------------------------------
// Migrations: Schema.Create(ctx) auto-migration (full)
// ---------------------------------------------------------------------------

func TestEntMigration(t *testing.T) {
	ents := entExtract(t, fixtureInput(t, "ent_usage.go", "go"))
	if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate:ent_schema_create") {
		t.Error("expected ent Schema.Create auto-migration")
	}
}

// A plain (non-schema) Go file must yield no ent schema entities.
func TestEntIgnoresNonSchema(t *testing.T) {
	ents := entExtract(t, fixtureInput(t, "gorm_models.go", "go"))
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" {
			t.Errorf("ent extractor should not emit schema for non-ent file: %s", e.Name)
		}
	}
}
