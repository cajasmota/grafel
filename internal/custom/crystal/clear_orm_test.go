package crystal_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/crystal"
)

// cfi builds a Clear-test FileInput.
func cfi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// TestCrystalClearORM_ModelTableColumns proves a class that `include
// Clear::Model` synthesises model + table + column + association SCOPE.Schema
// entities, honours `self.table`, stamps the primary column, trims the nilable
// marker, synthesises timestamps columns, and emits a belongs_to FK edge.
func TestCrystalClearORM_ModelTableColumns(t *testing.T) {
	src := `
require "clear"

class Account
  include Clear::Model
  self.table = "accounts"

  column id : Int64, primary: true
  column name : String
end

class User
  include Clear::Model
  self.table = "users"

  column id : Int64, primary: true
  column name : String
  column email : String?

  timestamps

  has_many posts : Post
  belongs_to account : Account
end
`
	e, ok := extreg.Get("custom_crystal_clear_orm")
	if !ok {
		t.Fatal("custom_crystal_clear_orm not registered")
	}
	ents, err := e.Extract(context.Background(), cfi("src/models.cr", "crystal", src))
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
		if en.Properties["framework"] != "clear" {
			t.Errorf("entity %q missing framework=clear", en.Name)
		}
		got[key{en.Name, en.Subtype}] = true
	}

	for _, m := range []string{"User", "Account"} {
		if !got[key{m, "model"}] {
			t.Errorf("expected SCOPE.Schema/model %q", m)
		}
	}
	for _, tbl := range []string{"users", "accounts"} {
		if !got[key{tbl, "table"}] {
			t.Errorf("expected SCOPE.Schema/table %q", tbl)
		}
	}
	for _, c := range []string{"id", "name", "email", "created_at", "updated_at"} {
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

	primaryStamped := false
	emailTrimmed := false
	tsAuto := false
	fkFound := false
	accTarget := ""
	for _, en := range ents {
		if en.Name == "id" && en.Subtype == "column" && en.Properties["primary_key"] == "true" {
			primaryStamped = true
		}
		if en.Name == "email" && en.Subtype == "column" && en.Properties["column_type"] == "String" {
			emailTrimmed = true
		}
		if en.Name == "updated_at" && en.Subtype == "column" && en.Properties["auto_timestamp"] == "true" {
			tsAuto = true
		}
		if en.Name == "User" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == "Account" && r.Properties["fk_field"] == "account" {
					fkFound = true
				}
			}
		}
		if en.Name == "account" && en.Subtype == "association" {
			accTarget = en.Properties["target"]
		}
	}
	if !primaryStamped {
		t.Error("expected id column primary_key=true")
	}
	if !emailTrimmed {
		t.Error("expected email column column_type=String (nilable `?` trimmed)")
	}
	if !tsAuto {
		t.Error("expected updated_at column auto_timestamp=true from timestamps")
	}
	if !fkFound {
		t.Error("expected REFERENCES edge User→Account (fk_field=account) from belongs_to account : Account")
	}
	if accTarget != "Account" {
		t.Errorf("expected belongs_to account : Account target=Account, got %q", accTarget)
	}
}

// TestCrystalClearORM_NonModelNoop proves a plain class WITHOUT include
// Clear::Model is ignored even when Clear::Model appears elsewhere in the file.
func TestCrystalClearORM_NonModelNoop(t *testing.T) {
	src := `
require "clear"
# Clear::Model referenced in a comment, but no class includes it.
class Config
  def initialize(@host : String)
  end
end

class Helper
  include Clear::Model
end
`
	e, _ := extreg.Get("custom_crystal_clear_orm")
	ents, _ := e.Extract(context.Background(), cfi("src/config.cr", "crystal", src))
	for _, en := range ents {
		if en.Properties["model"] == "Config" || en.Name == "Config" {
			t.Errorf("Config (no include Clear::Model) must not be extracted: got %q/%s", en.Name, en.Subtype)
		}
	}
}

// TestCrystalClearORM_WrongLanguageNoop gates on language=="crystal".
func TestCrystalClearORM_WrongLanguageNoop(t *testing.T) {
	src := `class User
  include Clear::Model
  column id : Int64, primary: true
end`
	e, _ := extreg.Get("custom_crystal_clear_orm")
	ents, _ := e.Extract(context.Background(), cfi("src/models.cr", "ruby", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-crystal language, got %d", len(ents))
	}
}
