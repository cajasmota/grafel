package crystal_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/crystal"
)

// afi builds an Avram-test FileInput.
func afi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// TestCrystalAvramORM_ModelTableColumns proves a `class T < BaseModel` (in a
// file referencing Avram::Model) synthesises model + table + column +
// association SCOPE.Schema entities from the `table do … end` block, stamps the
// primary_key column, trims the nilable marker, synthesises timestamps columns,
// honours an explicit table name, and emits a belongs_to FK edge.
func TestCrystalAvramORM_ModelTableColumns(t *testing.T) {
	src := `
require "avram"
abstract class BaseModel < Avram::Model
end

class Account < BaseModel
  table :accounts do
    primary_key id : Int64
    column name : String
  end
end

class User < BaseModel
  table do
    primary_key id : Int64
    column name : String
    column email : String?
    timestamps
    has_many posts : Post
    belongs_to account : Account
  end
end
`
	e, ok := extreg.Get("custom_crystal_avram_orm")
	if !ok {
		t.Fatal("custom_crystal_avram_orm not registered")
	}
	ents, err := e.Extract(context.Background(), afi("src/models.cr", "crystal", src))
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
		if en.Properties["framework"] != "avram" {
			t.Errorf("entity %q missing framework=avram", en.Name)
		}
		got[key{en.Name, en.Subtype}] = true
	}

	for _, m := range []string{"User", "Account"} {
		if !got[key{m, "model"}] {
			t.Errorf("expected SCOPE.Schema/model %q", m)
		}
	}
	// Account has an explicit `table :accounts do`; User uses anonymous `table do`
	// so the table is keyed by the class name.
	if !got[key{"accounts", "table"}] {
		t.Error("expected SCOPE.Schema/table accounts (explicit table name)")
	}
	if !got[key{"User", "table"}] {
		t.Error("expected SCOPE.Schema/table User (anonymous table do → class name)")
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
	for _, en := range ents {
		if en.Name == "id" && en.Subtype == "column" && en.Properties["primary_key"] == "true" {
			primaryStamped = true
		}
		if en.Name == "email" && en.Subtype == "column" && en.Properties["column_type"] == "String" {
			emailTrimmed = true
		}
		if en.Name == "created_at" && en.Subtype == "column" && en.Properties["auto_timestamp"] == "true" {
			tsAuto = true
		}
		if en.Name == "User" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == "Account" && r.Properties["fk_field"] == "account" {
					fkFound = true
				}
			}
		}
	}
	if !primaryStamped {
		t.Error("expected id column primary_key=true from primary_key declaration")
	}
	if !emailTrimmed {
		t.Error("expected email column column_type=String (nilable `?` trimmed)")
	}
	if !tsAuto {
		t.Error("expected created_at column auto_timestamp=true from timestamps")
	}
	if !fkFound {
		t.Error("expected REFERENCES edge User→Account (fk_field=account) from belongs_to account : Account")
	}
}

// TestCrystalAvramORM_NonModelNoop proves a plain class (no BaseModel parent) is
// ignored even when Avram::Model appears in the file.
func TestCrystalAvramORM_NonModelNoop(t *testing.T) {
	src := `
require "avram"
abstract class BaseModel < Avram::Model
end

class Config
  def initialize(@host : String)
  end
end
`
	e, _ := extreg.Get("custom_crystal_avram_orm")
	ents, _ := e.Extract(context.Background(), afi("src/config.cr", "crystal", src))
	for _, en := range ents {
		if en.Name == "Config" {
			t.Errorf("Config (not a BaseModel subclass) must not be extracted: got %q/%s", en.Name, en.Subtype)
		}
	}
}

// TestCrystalAvramORM_WrongLanguageNoop gates on language=="crystal".
func TestCrystalAvramORM_WrongLanguageNoop(t *testing.T) {
	src := `class User < BaseModel
  table do
    primary_key id : Int64
  end
end
# Avram::Model`
	e, _ := extreg.Get("custom_crystal_avram_orm")
	ents, _ := e.Extract(context.Background(), afi("src/models.cr", "ruby", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-crystal language, got %d", len(ents))
	}
}
