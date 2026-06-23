// Package ruby — issue #4854 general class field-membership tests.
//
// Root cause: Ruby has no static field declarations, so a plain data class's
// declaratively-present members (attr_accessor/reader/writer symbols and
// Struct.new/Data.define members) were never emitted as graph field entities by
// the primary pass — only the framework-bound custom validation emitter surfaced
// some as ORPHAN dto_field nodes with no CONTAINS edge. A plain Ruby model
// therefore resolved to a SCOPE.Component with ZERO field children, so the
// dashboard shape endpoint returned rows:[] — the same gap #4850/#4855 closed
// for Go and #4845/#4851 for JS/TS.
//
// After #4854 every attr_* symbol gets a SCOPE.Schema/field entity + class→field
// CONTAINS edge, a `Const = Struct.new(:a,:b)` / `Data.define` synthesises a
// data-class Component with field members, and an in-file superclass emits an
// EXTENDS edge.
package ruby_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func rbExtract(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extreg.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

func rbFieldEntityExists(ents []types.EntityRecord, owner, field string) bool {
	want := owner + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			return true
		}
	}
	return false
}

func rbHasContainsField(ents []types.EntityRecord, path, owner, field string) bool {
	want := extreg.BuildSchemaFieldStructuralRef("ruby", path, owner+"."+field)
	for _, e := range ents {
		if e.Name != owner || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" && r.ToID == want {
				return true
			}
		}
	}
	return false
}

func rbHasExtends(ents []types.EntityRecord, owner, base string) bool {
	for _, e := range ents {
		if e.Name != owner || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" && r.ToID == base {
				return true
			}
		}
	}
	return false
}

// TestRubyAttrAccessorFieldsAreContained proves a plain class with attr_* calls
// emits one SCOPE.Schema/field entity per symbol AND a class→field CONTAINS edge.
func TestRubyAttrAccessorFieldsAreContained(t *testing.T) {
	path := "app/models/user.rb"
	src := `
class User
  attr_accessor :name, :email
  attr_reader :id

  def greet
    "hi"
  end
end
`
	ents := rbExtract(t, src, path)
	for _, f := range []string{"name", "email", "id"} {
		if !rbFieldEntityExists(ents, "User", f) {
			t.Errorf("expected SCOPE.Schema/field entity User.%s", f)
		}
		if !rbHasContainsField(ents, path, "User", f) {
			t.Errorf("expected CONTAINS edge from User to field %q", f)
		}
	}
}

// TestRubyStructDefineFieldsAreContained proves Struct.new / Data.define
// synthesise a data-class Component carrying its members as field children.
func TestRubyStructDefineFieldsAreContained(t *testing.T) {
	path := "app/values.rb"
	src := `
Point = Struct.new(:x, :y)
Coord = Data.define(:lat, :lng)
`
	ents := rbExtract(t, src, path)
	for _, f := range []string{"x", "y"} {
		if !rbFieldEntityExists(ents, "Point", f) {
			t.Errorf("expected SCOPE.Schema/field entity Point.%s", f)
		}
		if !rbHasContainsField(ents, path, "Point", f) {
			t.Errorf("expected CONTAINS edge from Point to field %q", f)
		}
	}
	for _, f := range []string{"lat", "lng"} {
		if !rbFieldEntityExists(ents, "Coord", f) {
			t.Errorf("expected SCOPE.Schema/field entity Coord.%s", f)
		}
		if !rbHasContainsField(ents, path, "Coord", f) {
			t.Errorf("expected CONTAINS edge from Coord to field %q", f)
		}
	}
}

// TestRubySuperclassEmitsExtends proves an in-file superclass emits an EXTENDS
// edge so the shape walker recurses into inherited attr fields.
func TestRubySuperclassEmitsExtends(t *testing.T) {
	path := "app/models/account.rb"
	src := `
class BaseEntity
  attr_accessor :id
end

class Account < BaseEntity
  attr_accessor :owner
end
`
	ents := rbExtract(t, src, path)
	if !rbFieldEntityExists(ents, "BaseEntity", "id") {
		t.Errorf("expected SCOPE.Schema/field entity BaseEntity.id")
	}
	if !rbHasContainsField(ents, path, "Account", "owner") {
		t.Errorf("expected CONTAINS edge from Account to field owner")
	}
	if !rbHasExtends(ents, "Account", "BaseEntity") {
		t.Errorf("expected EXTENDS edge from Account to in-file base BaseEntity")
	}
}
