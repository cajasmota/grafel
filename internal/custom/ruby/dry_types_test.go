package ruby_test

// dry_types_test.go — tests for the ruby_dry_types extractor.
// Part of #3282.

import (
	"testing"
)

func dryExtract(t *testing.T, src string) []entitySummary {
	t.Helper()
	return extract(t, "ruby_dry_types", fi("types.rb", "ruby", src))
}

// ---------------------------------------------------------------------------
// Dry::Struct class extraction
// ---------------------------------------------------------------------------

func TestDryTypes_StructClass(t *testing.T) {
	src := `
module Types
  include Dry.Types()
end

class UserProfile < Dry::Struct
  attribute :name,  Types::String
  attribute :email, Types::String
  attribute :age,   Types::Integer
end
`
	ents := dryExtract(t, src)
	if !containsEntity(ents, "SCOPE.Schema", "dry_struct:UserProfile") {
		t.Error("expected dry_struct:UserProfile schema entity")
	}
}

func TestDryTypes_StructAttributes(t *testing.T) {
	src := `
class Order < Dry::Struct
  attribute :id,     Types::Integer
  attribute :total,  Types::Float
  attribute :status, Types::String
end
`
	ents := dryExtract(t, src)
	if !containsEntity(ents, "SCOPE.Schema", "dry_attr:id") {
		t.Error("expected dry_attr:id column entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "dry_attr:total") {
		t.Error("expected dry_attr:total column entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "dry_attr:status") {
		t.Error("expected dry_attr:status column entity")
	}
}

// ---------------------------------------------------------------------------
// Optional attributes
// ---------------------------------------------------------------------------

func TestDryTypes_OptionalAttribute(t *testing.T) {
	src := `
class Profile < Dry::Struct
  attribute  :name,  Types::String
  attribute? :bio,   Types::String
end
`
	ents := dryExtract(t, src)
	if !containsEntity(ents, "SCOPE.Schema", "dry_attr_opt:bio") {
		t.Error("expected dry_attr_opt:bio optional attribute entity")
	}
}

// ---------------------------------------------------------------------------
// Types module container
// ---------------------------------------------------------------------------

func TestDryTypes_TypesModule(t *testing.T) {
	src := `
module Types
  include Dry.Types()
  String = Dry::Types["coercible.string"]
end
`
	ents := dryExtract(t, src)
	if !containsEntity(ents, "SCOPE.Component", "dry_types_module") {
		t.Error("expected dry_types_module component entity")
	}
}

// ---------------------------------------------------------------------------
// Named type aliases
// ---------------------------------------------------------------------------

func TestDryTypes_TypeAlias(t *testing.T) {
	src := `
module Types
  include Dry.Types()
  StrippedString = Types::String.constructor { |v| v.strip }
  PositiveInt    = Types::Integer.constrained(gt: 0)
end
`
	ents := dryExtract(t, src)
	if !containsEntity(ents, "SCOPE.Schema", "dry_type_alias:StrippedString") {
		t.Error("expected dry_type_alias:StrippedString schema entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "dry_type_alias:PositiveInt") {
		t.Error("expected dry_type_alias:PositiveInt schema entity")
	}
}

// ---------------------------------------------------------------------------
// dry-schema / dry-validation contract
// ---------------------------------------------------------------------------

func TestDryTypes_SchemaContract(t *testing.T) {
	src := `
class CreateUserContract < Dry::Validation::Contract
  schema do
    required(:name).filled(:string)
    required(:age).value(:integer)
    optional(:email).filled(:string)
  end
end
`
	ents := dryExtract(t, src)
	if !containsEntity(ents, "SCOPE.Schema", "dry_schema_contract") {
		t.Error("expected dry_schema_contract schema entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "dry_schema_rule:required:name") {
		t.Error("expected dry_schema_rule:required:name schema entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "dry_schema_rule:optional:email") {
		t.Error("expected dry_schema_rule:optional:email schema entity")
	}
}

// ---------------------------------------------------------------------------
// Dry module inclusions
// ---------------------------------------------------------------------------

func TestDryTypes_IncludeDryMonads(t *testing.T) {
	src := `
class UserService
  include Dry::Monads[:result, :do]
  extend  Dry::Initializer
end
`
	ents := dryExtract(t, src)
	if !containsEntity(ents, "SCOPE.Pattern", "dry_include:Dry::Monads") {
		t.Error("expected dry_include:Dry::Monads pattern entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "dry_include:Dry::Initializer") {
		t.Error("expected dry_include:Dry::Initializer pattern entity")
	}
}

// ---------------------------------------------------------------------------
// Non-dry files → no entities
// ---------------------------------------------------------------------------

func TestDryTypes_NoMatch_PlainRuby(t *testing.T) {
	src := `
class User < ApplicationRecord
  belongs_to :account
end
`
	ents := dryExtract(t, src)
	for _, e := range ents {
		if len(e.Name) > 4 && e.Name[:3] == "dry" {
			t.Errorf("unexpected dry entity in plain ruby: %+v", e)
		}
	}
}

func TestDryTypes_NoMatch_EmptyFile(t *testing.T) {
	ents := dryExtract(t, "")
	if len(ents) != 0 {
		t.Errorf("expected no entities for empty file, got %d", len(ents))
	}
}
