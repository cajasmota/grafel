package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func bunExtract(t *testing.T, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get("custom_go_bun")
	if !ok {
		t.Fatal("custom_go_bun not registered")
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

func bunExtractRecords(t *testing.T, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, _ := extreg.Get("custom_go_bun")
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	return ents
}

// ---------------------------------------------------------------------------
// Models: struct + bun:"table:..." + column tags (full)
// ---------------------------------------------------------------------------

func TestBunModelsFull(t *testing.T) {
	file := fixtureInput(t, "bun_models.go", "go")
	ents := bunExtract(t, file)

	for _, m := range []string{"User", "Company", "Order", "Profile", "Role"} {
		if !containsEntity(ents, "SCOPE.Schema", m) {
			t.Errorf("expected %s schema", m)
		}
	}
	// Columns (explicit + implicit names).
	for _, c := range []string{"field:User.ID", "field:User.Name", "field:User.Email", "field:User.CompanyID"} {
		if !hasSubtype(ents, "SCOPE.Component", "field", c) {
			t.Errorf("expected column %s", c)
		}
	}

	// Table name + column-name properties resolved from tags.
	recs := bunExtractRecords(t, file)
	props := func(name string) map[string]string {
		for _, r := range recs {
			if r.Name == name {
				return r.Properties
			}
		}
		return nil
	}
	if p := props("User"); p == nil || p["table_name"] != "users" {
		t.Errorf("User table_name = %v, want users", p)
	}
	if p := props("field:User.Email"); p == nil || p["column_name"] != "email_address" {
		t.Errorf("User.Email column_name = %v, want email_address", p)
	}
	if p := props("field:User.ID"); p == nil || p["primary_key"] != "true" {
		t.Errorf("User.ID primary_key = %v, want true", p)
	}
}

// ---------------------------------------------------------------------------
// Relationships: rel: tags, all four kinds (full)
// ---------------------------------------------------------------------------

func TestBunRelationships(t *testing.T) {
	file := fixtureInput(t, "bun_models.go", "go")
	recs := bunExtractRecords(t, file)

	want := map[string]struct {
		rel    string
		target string
	}{
		"rel:User.Company": {"belongs_to", "Company"},
		"rel:User.Orders":  {"has_many", "Order"},
		"rel:User.Profile": {"has_one", "Profile"},
		"rel:User.Roles":   {"many2many", "Role"},
	}
	for _, r := range recs {
		w, ok := want[r.Name]
		if !ok {
			continue
		}
		if r.Properties["relationship"] != w.rel {
			t.Errorf("%s relationship = %q, want %q", r.Name, r.Properties["relationship"], w.rel)
		}
		if r.Properties["target_model"] != w.target {
			t.Errorf("%s target_model = %q, want %q", r.Name, r.Properties["target_model"], w.target)
		}
		delete(want, r.Name)
	}
	for name := range want {
		t.Errorf("missing relation %s", name)
	}
}

func TestBunM2MJoinTable(t *testing.T) {
	recs := bunExtractRecords(t, fixtureInput(t, "bun_models.go", "go"))
	for _, r := range recs {
		if r.Name == "rel:User.Roles" {
			if r.Properties["join_table"] != "user_roles" {
				t.Errorf("Roles join_table = %q, want user_roles", r.Properties["join_table"])
			}
			return
		}
	}
	t.Error("rel:User.Roles not found")
}

// ---------------------------------------------------------------------------
// Queries: NewSelect/NewInsert/... bound to model where possible (full)
// ---------------------------------------------------------------------------

func TestBunQueries(t *testing.T) {
	ents := bunExtract(t, fixtureInput(t, "bun_queries.go", "go"))

	// Model-bound forms.
	if !hasSubtype(ents, "SCOPE.Operation", "query", "query:Select:User") {
		t.Error("expected model-bound NewSelect on User")
	}
	if !hasSubtype(ents, "SCOPE.Operation", "query", "query:Select:Order") {
		t.Error("expected model-bound NewSelect on Order via (*T)(nil)")
	}
	if !hasSubtype(ents, "SCOPE.Operation", "query", "query:Insert:User") {
		t.Error("expected model-bound NewInsert on User")
	}
	if !hasSubtype(ents, "SCOPE.Operation", "query", "query:Delete:User") {
		t.Error("expected model-bound NewDelete on User")
	}
	// Unbound raw query.
	if !hasSubtype(ents, "SCOPE.Operation", "query", "query:Raw") {
		t.Error("expected unbound NewRaw query")
	}
}

// ---------------------------------------------------------------------------
// Migrations: DDL builders + bun migrate API (full)
// ---------------------------------------------------------------------------

func TestBunMigrationsDDL(t *testing.T) {
	ents := bunExtract(t, fixtureInput(t, "bun_queries.go", "go"))
	if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate_ddl:CreateTable") {
		t.Error("expected NewCreateTable DDL migration")
	}
	if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate_ddl:DropTable") {
		t.Error("expected NewDropTable DDL migration")
	}
}

func TestBunMigrationsAPI(t *testing.T) {
	ents := bunExtract(t, fixtureInput(t, "bun_migrate.go", "go"))
	if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate_api:migrate.NewMigrations") {
		t.Error("expected migrate.NewMigrations migration")
	}
	if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate_api:Migrations.MustRegister") {
		t.Error("expected Migrations.MustRegister migration")
	}
	if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate_api:migrator.Migrate") {
		t.Error("expected migrator.Migrate migration")
	}
}

// Non-bun Go files must not yield bun schema entities.
func TestBunIgnoresNonBun(t *testing.T) {
	ents := bunExtract(t, fixtureInput(t, "gin_middleware_auth.go", "go"))
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" {
			t.Errorf("bun extractor emitted schema for non-bun file: %s", e.Name)
		}
	}
}
