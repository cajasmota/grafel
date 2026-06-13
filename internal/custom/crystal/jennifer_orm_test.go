package crystal_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/crystal"
)

// jfi builds a Jennifer-test FileInput.
func jfi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// TestCrystalJenniferORM_ModelTableColumns proves a `class T <
// Jennifer::Model::Base` model synthesises model + table + column + association
// SCOPE.Schema entities, honours `table_name`, maps Primary32 to a primary
// column, trims the nilable marker, synthesises with_timestamps columns, and
// emits a belongs_to FK edge.
func TestCrystalJenniferORM_ModelTableColumns(t *testing.T) {
	src := `
require "jennifer"

class Account < Jennifer::Model::Base
  table_name "accounts"

  mapping(
    id: Primary32,
    name: String,
  )
end

class User < Jennifer::Model::Base
  table_name "users"

  mapping(
    id: Primary32,
    name: String,
    email: String?,
  )

  with_timestamps

  has_many :posts, Post
  belongs_to :account, Account
end
`
	e, ok := extreg.Get("custom_crystal_jennifer_orm")
	if !ok {
		t.Fatal("custom_crystal_jennifer_orm not registered")
	}
	ents, err := e.Extract(context.Background(), jfi("src/models.cr", "crystal", src))
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
		if en.Properties["framework"] != "jennifer" {
			t.Errorf("entity %q missing framework=jennifer", en.Name)
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
	postsTarget := ""
	for _, en := range ents {
		if en.Name == "id" && en.Subtype == "column" && en.Properties["primary_key"] == "true" {
			if en.Properties["column_type"] == "Int32" {
				primaryStamped = true
			}
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
		if en.Name == "posts" && en.Subtype == "association" {
			postsTarget = en.Properties["target"]
		}
	}
	if !primaryStamped {
		t.Error("expected id column primary_key=true + column_type=Int32 from Primary32")
	}
	if !emailTrimmed {
		t.Error("expected email column column_type=String (nilable `?` trimmed)")
	}
	if !tsAuto {
		t.Error("expected created_at column auto_timestamp=true from with_timestamps")
	}
	if !fkFound {
		t.Error("expected REFERENCES edge User→Account (fk_field=account) from belongs_to :account")
	}
	if postsTarget != "Post" {
		t.Errorf("expected has_many :posts, Post target=Post, got %q", postsTarget)
	}
}

// TestCrystalJenniferORM_NonModelNoop proves a plain (non-Jennifer) class is
// ignored.
func TestCrystalJenniferORM_NonModelNoop(t *testing.T) {
	src := `
class Config
  def initialize(@host : String)
  end
end
`
	e, _ := extreg.Get("custom_crystal_jennifer_orm")
	ents, _ := e.Extract(context.Background(), jfi("src/config.cr", "crystal", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for a non-Jennifer class, got %d", len(ents))
	}
}

// TestCrystalJenniferORM_WrongLanguageNoop gates on language=="crystal".
func TestCrystalJenniferORM_WrongLanguageNoop(t *testing.T) {
	src := `class User < Jennifer::Model::Base
  mapping(id: Primary32)
end`
	e, _ := extreg.Get("custom_crystal_jennifer_orm")
	ents, _ := e.Extract(context.Background(), jfi("src/models.cr", "ruby", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-crystal language, got %d", len(ents))
	}
}
