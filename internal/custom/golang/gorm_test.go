package golang_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// fixtureInput reads a golden fixture from testdata/ and wraps it as a
// FileInput. The on-disk filename is preserved so filename-driven detection
// (file-based SQL migrations) exercises the real path.
func fixtureInput(t *testing.T, name, lang string) extreg.FileInput {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return extreg.FileInput{Path: filepath.Join("testdata", name), Language: lang, Content: content}
}

func gormExtract(t *testing.T, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get("custom_go_gorm")
	if !ok {
		t.Fatal("custom_go_gorm not registered")
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

func hasSubtype(ents []entitySummary, kind, subtype, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Models: field tags (schema_extraction = full)
// ---------------------------------------------------------------------------

func TestGORMFieldTagsSchemaFull(t *testing.T) {
	ents := gormExtract(t, fixtureInput(t, "gorm_models.go", "go"))

	// gorm.Model embed schema (pre-existing capability).
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User schema")
	}
	// Plain struct with gorm field tags but NO gorm.Model embed.
	if !containsEntity(ents, "SCOPE.Schema", "Company") {
		t.Error("expected Company schema inferred from field tags")
	}
	// Field-level columns with explicit column mapping.
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:User.Name") {
		t.Error("expected User.Name field component")
	}
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:User.Email") {
		t.Error("expected User.Email field component")
	}
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:Company.Name") {
		t.Error("expected Company.Name field component")
	}
}

// ---------------------------------------------------------------------------
// Relationships: association tags (full for tag-driven, all 4 kinds)
// ---------------------------------------------------------------------------

func TestGORMRelationships(t *testing.T) {
	ents := gormExtract(t, fixtureInput(t, "gorm_models.go", "go"))

	// belongs_to: Company Company `foreignKey:CompanyID`
	if !hasSubtype(ents, "SCOPE.Component", "relation", "rel:User.Company") {
		t.Error("expected User.Company belongs_to relation")
	}
	// has_many: Orders []Order
	if !hasSubtype(ents, "SCOPE.Component", "relation", "rel:User.Orders") {
		t.Error("expected User.Orders has_many relation")
	}
	// has_one: Profile *Profile `references:ID`
	if !hasSubtype(ents, "SCOPE.Component", "relation", "rel:User.Profile") {
		t.Error("expected User.Profile has_one relation")
	}
	// many2many: Roles []Role `many2many:user_roles`
	if !hasSubtype(ents, "SCOPE.Component", "relation", "rel:User.Roles") {
		t.Error("expected User.Roles many2many relation")
	}
}

func TestGORMRelationshipKinds(t *testing.T) {
	e, _ := extreg.Get("custom_go_gorm")
	ents, err := e.Extract(context.Background(), fixtureInput(t, "gorm_models.go", "go"))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}  // name -> relationship
	targets := map[string]string{} // name -> target_model
	for _, ent := range ents {
		if ent.Subtype == "relation" {
			byName[ent.Name] = ent.Properties["relationship"]
			targets[ent.Name] = ent.Properties["target_model"]
		}
	}
	cases := map[string]struct{ rel, target string }{
		"rel:User.Company": {"belongs_to", "Company"},
		"rel:User.Orders":  {"has_many", "Order"},
		"rel:User.Profile": {"has_one", "Profile"},
		"rel:User.Roles":   {"many2many", "Role"},
	}
	for name, want := range cases {
		if byName[name] != want.rel {
			t.Errorf("%s: relationship = %q, want %q", name, byName[name], want.rel)
		}
		if targets[name] != want.target {
			t.Errorf("%s: target_model = %q, want %q", name, targets[name], want.target)
		}
	}
}

// ---------------------------------------------------------------------------
// Queries: chainers (query_attribution = partial)
// ---------------------------------------------------------------------------

func TestGORMQueryChainers(t *testing.T) {
	ents := gormExtract(t, fixtureInput(t, "gorm_queries.go", "go"))

	// Model-bound query (full attribution).
	if !containsEntity(ents, "SCOPE.Operation", "query:User") {
		t.Error("expected query:User (db.Model bound)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "query:legacy_users") {
		t.Error("expected query:legacy_users (db.Table bound)")
	}
	// Chainers (heuristic, partial attribution).
	for _, verb := range []string{"Where", "Joins", "Preload", "Select", "Order", "Limit", "Updates", "Delete", "Count"} {
		if !containsEntity(ents, "SCOPE.Operation", "chain:"+verb) {
			t.Errorf("expected chain:%s operation", verb)
		}
	}
	if !containsEntity(ents, "SCOPE.Operation", "create:user") {
		t.Error("expected create:user operation")
	}
}

// ---------------------------------------------------------------------------
// Migrations: AutoMigrate + Migrator ops + file-based (full)
// ---------------------------------------------------------------------------

func TestGORMMigrationsProgrammatic(t *testing.T) {
	ents := gormExtract(t, fixtureInput(t, "gorm_migrate.go", "go"))

	// AutoMigrate emits both a Schema and a migration Operation per model.
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User schema from AutoMigrate")
	}
	if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate:User") {
		t.Error("expected migrate:User migration operation")
	}
	// Migrator() programmatic ops.
	for _, op := range []string{"CreateTable", "AddColumn", "CreateIndex", "DropTable"} {
		if !hasSubtype(ents, "SCOPE.Operation", "migration", "migrate_op:"+op) {
			t.Errorf("expected migrate_op:%s migration operation", op)
		}
	}
}

func TestGORMMigrationsFileBased(t *testing.T) {
	up := gormExtract(t, fixtureInput(t, "000123_create_users.up.sql", "sql"))
	if !hasSubtype(up, "SCOPE.Schema", "migration", "migration:000123_create_users.up") {
		t.Error("expected up migration schema entity")
	}
	down := gormExtract(t, fixtureInput(t, "000123_create_users.down.sql", "sql"))
	if !hasSubtype(down, "SCOPE.Schema", "migration", "migration:000123_create_users.down") {
		t.Error("expected down migration schema entity")
	}
}

func TestGORMNoMatchDedicated(t *testing.T) {
	ents := gormExtract(t, extreg.FileInput{Path: "main.go", Language: "go", Content: []byte("package main")})
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
