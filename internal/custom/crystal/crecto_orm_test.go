package crystal_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/crystal"
)

// rfi builds a Crecto-test FileInput.
func rfi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// TestCrystalCrectoORM_ModelTableColumns proves a class with a `schema "<table>"
// do … field … end` block synthesises model + table + column + association
// SCOPE.Schema entities, synthesises the implicit id primary key, and emits a
// belongs_to FK edge.
func TestCrystalCrectoORM_ModelTableColumns(t *testing.T) {
	src := `
require "crecto"

class User
  include Crecto::Schema

  schema "users" do
    field :name, String
    field :email, String
    field :age, Int32
    has_many :posts, Post
    belongs_to :account, Account
  end
end
`
	e, ok := extreg.Get("custom_crystal_crecto_orm")
	if !ok {
		t.Fatal("custom_crystal_crecto_orm not registered")
	}
	ents, err := e.Extract(context.Background(), rfi("src/user.cr", "crystal", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	type key struct{ name, sub string }
	got := map[key]bool{}
	for _, en := range ents {
		if en.Kind != "SCOPE.Schema" {
			t.Errorf("unexpected kind %q for %q", en.Kind, en.Name)
			continue
		}
		if en.Properties["framework"] != "crecto" {
			t.Errorf("entity %q missing framework=crecto", en.Name)
		}
		got[key{en.Name, en.Subtype}] = true
	}

	if !got[key{"User", "model"}] {
		t.Error("expected SCOPE.Schema/model User")
	}
	if !got[key{"users", "table"}] {
		t.Error("expected SCOPE.Schema/table users (schema name)")
	}
	for _, c := range []string{"id", "name", "email", "age"} {
		if !got[key{c, "column"}] {
			t.Errorf("expected SCOPE.Schema/column %q", c)
		}
	}
	if !got[key{"posts", "association"}] {
		t.Error("expected SCOPE.Schema/association posts (has_many)")
	}
	if !got[key{"account", "association"}] {
		t.Error("expected SCOPE.Schema/association account (belongs_to)")
	}

	implicitPK := false
	ageType := ""
	fkFound := false
	for _, en := range ents {
		if en.Name == "id" && en.Subtype == "column" && en.Properties["primary_key"] == "true" {
			implicitPK = true
		}
		if en.Name == "age" && en.Subtype == "column" {
			ageType = en.Properties["column_type"]
		}
		if en.Name == "User" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == "Account" && r.Properties["fk_field"] == "account" {
					fkFound = true
				}
			}
		}
	}
	if !implicitPK {
		t.Error("expected synthesised implicit id primary-key column (primary_key=true)")
	}
	if ageType != "Int32" {
		t.Errorf("expected age column column_type=Int32, got %q", ageType)
	}
	if !fkFound {
		t.Error("expected REFERENCES edge User→Account (fk_field=account) from belongs_to :account")
	}
}

// TestCrystalCrectoORM_NonModelNoop proves a class with no `schema "…" do`
// block is ignored even when Crecto::Schema is referenced in the file.
func TestCrystalCrectoORM_NonModelNoop(t *testing.T) {
	src := `
require "crecto"
# Crecto::Schema referenced, but Config declares no schema block.
class Config
  def initialize(@host : String)
  end
end
`
	e, _ := extreg.Get("custom_crystal_crecto_orm")
	ents, _ := e.Extract(context.Background(), rfi("src/config.cr", "crystal", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for a class with no schema block, got %d", len(ents))
	}
}

// TestCrystalCrectoORM_WrongLanguageNoop gates on language=="crystal".
func TestCrystalCrectoORM_WrongLanguageNoop(t *testing.T) {
	src := `class User
  include Crecto::Schema
  schema "users" do
    field :name, String
  end
end`
	e, _ := extreg.Get("custom_crystal_crecto_orm")
	ents, _ := e.Extract(context.Background(), rfi("src/user.cr", "ruby", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-crystal language, got %d", len(ents))
	}
}
