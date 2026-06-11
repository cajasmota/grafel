package ruby_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
)

// Issue #4715 — Ruby ActiveModel::Serializer (AMS) DTO FIELD-as-member indexing.
// A serializer's `attributes :a, :b` / `attribute :c` declarations must be
// emitted as SCOPE.Schema/field members with name AND a CONTAINS edge back to
// the serializer — the SAME shape as the JS/Python/Java/Go/C# DTO field members.

func TestRubyAMS_FieldMembers(t *testing.T) {
	src := `class UserSerializer < ActiveModel::Serializer
  attributes :id, :name, :email
  attribute :full_name

  def full_name
    "#{object.first} #{object.last}"
  end
end
`
	e, ok := extreg.Get("custom_ruby_ams_serializer")
	if !ok {
		t.Fatal("custom_ruby_ams_serializer not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "user_serializer.rb", Language: "ruby", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	field := func(name string) *types.EntityRecord {
		for i := range ents {
			if ents[i].Name == name && ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "field" {
				return &ents[i]
			}
		}
		return nil
	}
	containsTo := func(id string) bool {
		for _, e := range ents {
			for _, r := range e.Relationships {
				if r.Kind == string(types.RelationshipKindContains) && r.ToID == id {
					return true
				}
			}
		}
		return false
	}

	for _, attr := range []string{"id", "name", "email", "full_name"} {
		f := field("UserSerializer." + attr)
		if f == nil {
			t.Fatalf("expected UserSerializer.%s field member", attr)
		}
		if f.Properties["field_name"] != attr {
			t.Errorf("%s field_name = %q, want %q", attr, f.Properties["field_name"], attr)
		}
		if f.Properties["parent_class"] != "UserSerializer" {
			t.Errorf("%s parent_class = %q, want UserSerializer", attr, f.Properties["parent_class"])
		}
		if f.Signature == "" {
			t.Errorf("%s field must carry a Signature", attr)
		}
		if !containsTo(f.ID) {
			t.Errorf("expected CONTAINS edge to UserSerializer.%s", attr)
		}
	}

	// The serializer DTO node must exist for the membership edge to resolve.
	var dtoFound bool
	for i := range ents {
		if ents[i].Name == "UserSerializer" && ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "dto" {
			dtoFound = true
		}
	}
	if !dtoFound {
		t.Error("expected UserSerializer SCOPE.Schema dto node")
	}
}
