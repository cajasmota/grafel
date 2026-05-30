package elixir_test

// ---------------------------------------------------------------------------
// Typespec extractor tests
// ---------------------------------------------------------------------------

import "testing"

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
