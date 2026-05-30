package elixir_test

// ---------------------------------------------------------------------------
// Typespec extractor tests
// ---------------------------------------------------------------------------

import (
	"strings"
	"testing"
)

func TestTypespecTypeDecl(t *testing.T) {
	src := `
defmodule MyApp.Types do
  @type user_id :: integer()
  @typep private_token :: binary()
  @opaque connection :: pid()
end
`
	ents := extract(t, "custom_elixir_typespec", fi("types.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Schema", "user_id") {
		t.Error("expected @type user_id entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "private_token") {
		t.Error("expected @typep private_token entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "connection") {
		t.Error("expected @opaque connection entity")
	}
}

func TestTypespecTypeAlias(t *testing.T) {
	src := `
defmodule MyApp.Types do
  @type name :: String.t()
  @type count :: integer()
end
`
	ents := extract(t, "custom_elixir_typespec", fi("types.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Schema", "name") {
		t.Error("expected @type name alias entity")
	}
}

func TestTypespecSpec(t *testing.T) {
	src := `
defmodule MyApp.Calculator do
  @spec add(integer(), integer()) :: integer()
  def add(a, b), do: a + b

  @spec multiply(number(), number()) :: number()
  def multiply(a, b), do: a * b
end
`
	ents := extract(t, "custom_elixir_typespec", fi("calculator.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "spec:add") {
		t.Error("expected spec:add operation")
	}
	if !containsEntity(ents, "SCOPE.Operation", "spec:multiply") {
		t.Error("expected spec:multiply operation")
	}
}

func TestTypespecCallback(t *testing.T) {
	src := `
defmodule MyApp.Worker do
  @callback perform(args :: map()) :: :ok | {:error, term()}
  @callback max_attempts() :: pos_integer()
end
`
	ents := extract(t, "custom_elixir_typespec", fi("worker.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "perform") {
		t.Error("expected callback perform entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "max_attempts") {
		t.Error("expected callback max_attempts entity")
	}
}

func TestTypespecBehaviour(t *testing.T) {
	src := `
defmodule MyApp.ConcreteWorker do
  @behaviour MyApp.Worker
  @behaviour GenServer

  def perform(args), do: :ok
end
`
	ents := extract(t, "custom_elixir_typespec", fi("concrete_worker.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Component", "implements:MyApp.Worker") {
		t.Error("expected implements:MyApp.Worker behaviour entity")
	}
	if !containsEntity(ents, "SCOPE.Component", "implements:GenServer") {
		t.Error("expected implements:GenServer behaviour entity")
	}
}

func TestTypespecDefProtocol(t *testing.T) {
	src := `
defprotocol MyApp.Serializable do
  @spec serialize(t()) :: binary()
  def serialize(value)
end
`
	ents := extract(t, "custom_elixir_typespec", fi("serializable.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Component", "MyApp.Serializable") {
		t.Error("expected defprotocol interface entity")
	}
}

// ---------------------------------------------------------------------------
// defstruct  → SCOPE.Schema/struct  (deep type_extraction)
// ---------------------------------------------------------------------------

func TestTypespecDefStructFields(t *testing.T) {
	src := `
defmodule MyApp.User do
  @enforce_keys [:id, :email]
  defstruct [:id, :email, :name, age: 0, active: true]
end
`
	ents := extract(t, "custom_elixir_typespec", fi("user.ex", "elixir", src))
	e := findEntity(ents, "SCOPE.Schema", "MyApp.User")
	if e == nil {
		t.Fatal("expected SCOPE.Schema/struct entity named MyApp.User")
	}
	if e.Subtype != "struct" {
		t.Errorf("expected subtype struct, got %q", e.Subtype)
	}
	fields := e.Props["struct_fields"]
	for _, want := range []string{"id", "email", "name", "age", "active"} {
		if !strings.Contains(","+fields+",", ","+want+",") {
			t.Errorf("expected struct field %q in %q", want, fields)
		}
	}
	if e.Props["field_count"] != "5" {
		t.Errorf("expected field_count 5, got %q", e.Props["field_count"])
	}
	enforced := e.Props["enforced_keys"]
	if !strings.Contains(enforced, "id") || !strings.Contains(enforced, "email") {
		t.Errorf("expected enforced_keys id,email, got %q", enforced)
	}
}

func TestTypespecDefStructKeywordForm(t *testing.T) {
	src := `
defmodule MyApp.Point do
  defstruct x: 0, y: 0
end
`
	ents := extract(t, "custom_elixir_typespec", fi("point.ex", "elixir", src))
	e := findEntity(ents, "SCOPE.Schema", "MyApp.Point")
	if e == nil {
		t.Fatal("expected struct entity MyApp.Point")
	}
	fields := e.Props["struct_fields"]
	if !strings.Contains(fields, "x") || !strings.Contains(fields, "y") {
		t.Errorf("expected fields x,y got %q", fields)
	}
}

// ---------------------------------------------------------------------------
// atom-union typespec  → SCOPE.Schema/enum  (literal enum analogue)
// ---------------------------------------------------------------------------

func TestTypespecAtomUnionEnum(t *testing.T) {
	src := `
defmodule MyApp.Account do
  @type role :: :admin | :member | :guest
  @type status :: :active | :suspended
end
`
	ents := extract(t, "custom_elixir_typespec", fi("account.ex", "elixir", src))
	role := findEntity(ents, "SCOPE.Schema", "role")
	if role == nil {
		t.Fatal("expected SCOPE.Schema/enum entity named role")
	}
	if role.Subtype != "enum" {
		t.Errorf("expected subtype enum, got %q", role.Subtype)
	}
	members := role.Props["enum_members"]
	for _, want := range []string{"admin", "member", "guest"} {
		if !strings.Contains(","+members+",", ","+want+",") {
			t.Errorf("expected enum member %q in %q", want, members)
		}
	}
	if role.Props["member_count"] != "3" {
		t.Errorf("expected member_count 3, got %q", role.Props["member_count"])
	}
	// An atom-union type must NOT also be emitted as a generic /type entity.
	for _, e := range ents {
		if e.Name == "role" && e.Subtype == "type" {
			t.Error("role should be enum only, not duplicated as generic type")
		}
	}
	if findEntity(ents, "SCOPE.Schema", "status") == nil {
		t.Error("expected status enum entity")
	}
}

func TestTypespecNonAtomUnionNotEnum(t *testing.T) {
	// A union of non-atom types is NOT an enum; stays a generic @type.
	src := `
defmodule MyApp.Types do
  @type result :: {:ok, term()} | {:error, term()}
  @type id :: integer() | binary()
end
`
	ents := extract(t, "custom_elixir_typespec", fi("types.ex", "elixir", src))
	for _, e := range ents {
		if e.Subtype == "enum" {
			t.Errorf("non-atom union %q must not be classified as enum", e.Name)
		}
	}
}

func TestTypespecNoMatch(t *testing.T) {
	src := `defmodule MyApp.Plain do\n  def hello, do: "world"\nend`
	ents := extract(t, "custom_elixir_typespec", fi("plain.ex", "elixir", src))
	// No typespecs → no entities
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" || (e.Kind == "SCOPE.Operation" && e.Subtype == "spec") {
			t.Errorf("unexpected entity %v", e)
		}
	}
}
