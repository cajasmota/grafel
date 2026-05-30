package elixir_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/elixir"
)

// extractFull returns the full EntityRecord set (with Properties) so tests can
// assert exact field + validator + bound/regex values — the TS/JS bar.

// findByName returns the first entity with the given Name, or nil.
func findByName(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// assertValProp fails unless entity `name` exists and has property key=want.
func assertValProp(t *testing.T, ents []types.EntityRecord, name, key, want string) {
	t.Helper()
	e := findByName(ents, name)
	if e == nil {
		t.Fatalf("expected entity %q, not found", name)
	}
	got := e.Properties[key]
	if got != want {
		t.Errorf("entity %q: prop %q = %q, want %q", name, key, got, want)
	}
}

// ---------------------------------------------------------------------------
// Ecto deep DTO extraction (cast field list)
// ---------------------------------------------------------------------------

func TestEctoCastDTOPerField(t *testing.T) {
	src := `
defmodule MyApp.Accounts.User do
  use Ecto.Schema
  import Ecto.Changeset

  schema "users" do
    field :name, :string
    field :email, :string
    field :age, :integer
  end

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:name, :email, :age])
    |> validate_required([:name, :email])
  end
end
`
	ents := extractFull(t, "custom_elixir_ecto", fi("user.ex", "elixir", src))

	// Each cast field becomes its own DTO entity with cast_type + field props.
	assertValProp(t, ents, "ecto_cast_field:name", "field", "name")
	assertValProp(t, ents, "ecto_cast_field:name", "cast_type", "scalar")
	assertValProp(t, ents, "ecto_cast_field:email", "field", "email")
	assertValProp(t, ents, "ecto_cast_field:age", "field", "age")

	// DTO fields are enriched with their declared schema type.
	assertValProp(t, ents, "ecto_cast_field:name", "field_type", "string")
	assertValProp(t, ents, "ecto_cast_field:email", "field_type", "string")
	assertValProp(t, ents, "ecto_cast_field:age", "field_type", "integer")

	// Subtype identifies the DTO capability.
	if e := findByName(ents, "ecto_cast_field:name"); e == nil || e.Subtype != "dto_extraction" {
		t.Errorf("ecto_cast_field:name subtype = %v, want dto_extraction", e)
	}
}

// ---------------------------------------------------------------------------
// Ecto deep request_validation (per-field validate_* + constraints)
// ---------------------------------------------------------------------------

func TestEctoChangesetValidationsPerField(t *testing.T) {
	src := `
defmodule MyApp.Accounts.User do
  import Ecto.Changeset

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:name, :email, :age, :role])
    |> validate_required([:name, :email])
    |> validate_format(:email, ~r/@/)
    |> validate_length(:name, min: 1, max: 20)
    |> validate_number(:age, greater_than: 0)
    |> validate_inclusion(:role, ["admin", "user"])
    |> unique_constraint(:email)
    |> foreign_key_constraint(:org_id)
  end
end
`
	ents := extractFull(t, "custom_elixir_ecto", fi("user.ex", "elixir", src))

	// validate_required → one entity per field, validator=required.
	assertValProp(t, ents, "ecto_val:name:required", "validator", "required")
	assertValProp(t, ents, "ecto_val:name:required", "field", "name")
	assertValProp(t, ents, "ecto_val:email:required", "field", "email")

	// validate_format(:email, ~r/@/) — exact regex literal captured.
	assertValProp(t, ents, "ecto_val:email:format", "field", "email")
	assertValProp(t, ents, "ecto_val:email:format", "validator", "format")
	assertValProp(t, ents, "ecto_val:email:format", "regex", "~r/@/")

	// validate_length(:name, min: 1, max: 20) — exact bounds, NOT len>0.
	assertValProp(t, ents, "ecto_val:name:length", "field", "name")
	assertValProp(t, ents, "ecto_val:name:length", "bound", "min:1,max:20")

	// validate_number(:age, greater_than: 0) — exact bound.
	assertValProp(t, ents, "ecto_val:age:number", "field", "age")
	assertValProp(t, ents, "ecto_val:age:number", "bound", "greater_than:0")

	// validate_inclusion(:role, [...]) — set captured.
	assertValProp(t, ents, "ecto_val:role:inclusion", "field", "role")
	assertValProp(t, ents, "ecto_val:role:inclusion", "validator", "inclusion")
	assertValProp(t, ents, "ecto_val:role:inclusion", "bound", `["admin", "user"]`)

	// unique_constraint(:email) → ecto_val:email:unique_constraint.
	assertValProp(t, ents, "ecto_val:email:unique_constraint", "field", "email")
	assertValProp(t, ents, "ecto_val:email:unique_constraint", "validator", "unique_constraint")

	// foreign_key_constraint(:org_id).
	assertValProp(t, ents, "ecto_val:org_id:foreign_key_constraint", "field", "org_id")
	assertValProp(t, ents, "ecto_val:org_id:foreign_key_constraint", "validator", "foreign_key_constraint")

	// Subtype identifies the request_validation capability.
	if e := findByName(ents, "ecto_val:email:format"); e == nil || e.Subtype != "request_validation" {
		t.Errorf("ecto_val:email:format subtype = %v, want request_validation", e)
	}
}

func TestEctoValidationConfirmationAndSingleArgRequired(t *testing.T) {
	src := `
defmodule MyApp.Accounts.User do
  import Ecto.Changeset

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:password, :terms])
    |> validate_required(:password)
    |> validate_confirmation(:password)
    |> validate_acceptance(:terms)
  end
end
`
	ents := extractFull(t, "custom_elixir_ecto", fi("user.ex", "elixir", src))

	// validate_required with a single bare symbol (not a list).
	assertValProp(t, ents, "ecto_val:password:required", "field", "password")
	// validate_confirmation(:password).
	assertValProp(t, ents, "ecto_val:password:confirmation", "validator", "confirmation")
	// validate_acceptance(:terms).
	assertValProp(t, ents, "ecto_val:terms:acceptance", "validator", "acceptance")
}
