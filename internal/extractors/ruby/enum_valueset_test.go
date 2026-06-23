package ruby_test

// enum_valueset_test.go — value-asserting tests for the Rails SCOPE.Enum
// value-set node (data-model, epic #3628 / completes #3806).

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractRubyForEnum(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "ruby", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

func findRubyEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func TestRailsEnumValueSet_HashValues(t *testing.T) {
	recs := extractRubyForEnum(t, "order.rb", `class Order < ApplicationRecord
  enum status: { active: 0, archived: 1 }
end`)
	en := findRubyEnum(recs, "status")
	if en == nil {
		t.Fatal("SCOPE.Enum:status value-set node not found")
	}
	if got := en.Properties["kind_hint"]; got != "rails_enum" {
		t.Fatalf("kind_hint = %q, want rails_enum", got)
	}
	if got := en.Properties["values"]; got != "active=0, archived=1" {
		t.Fatalf("values = %q, want %q", got, "active=0, archived=1")
	}
	wantQN := "scope:enum:order.rb:status"
	if en.QualifiedName != wantQN {
		t.Fatalf("QualifiedName = %q, want %q", en.QualifiedName, wantQN)
	}
}

func TestRailsEnumValueSet_ArraySymbols(t *testing.T) {
	recs := extractRubyForEnum(t, "task.rb", `class Task < ApplicationRecord
  enum priority: [:low, :medium, :high]
end`)
	en := findRubyEnum(recs, "priority")
	if en == nil {
		t.Fatal("SCOPE.Enum:priority value-set node not found")
	}
	if got := en.Properties["members"]; got != "low, medium, high" {
		t.Fatalf("members = %q, want %q", got, "low, medium, high")
	}
	// Array form has no explicit values.
	if got, ok := en.Properties["values"]; ok {
		t.Fatalf("values should be absent for array symbols, got %q", got)
	}
}

func TestRailsEnumValueSet_PositionalName(t *testing.T) {
	recs := extractRubyForEnum(t, "post.rb", `class Post < ApplicationRecord
  enum :state, { draft: 0, published: 1 }
end`)
	en := findRubyEnum(recs, "state")
	if en == nil {
		t.Fatal("SCOPE.Enum:state value-set node not found")
	}
	if got := en.Properties["values"]; got != "draft=0, published=1" {
		t.Fatalf("values = %q, want %q", got, "draft=0, published=1")
	}
}

// Negative: a non-enum DSL call produces no value-set node.
func TestRailsEnumValueSet_NonEnumCall_NoNode(t *testing.T) {
	recs := extractRubyForEnum(t, "user.rb", `class User < ApplicationRecord
  validates :email, presence: true
end`)
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" {
			t.Fatalf("non-enum call should NOT produce a SCOPE.Enum node, got %q", recs[i].Name)
		}
	}
}
