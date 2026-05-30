package elixir_test

// ---------------------------------------------------------------------------
// Plug extractor tests
// ---------------------------------------------------------------------------

import "testing"

func TestPlugRouter(t *testing.T) {
	src := `
defmodule MyApp.Router do
  use Plug.Router

  plug :match
  plug :dispatch

  get "/hello", do: send_resp(conn, 200, "Hello!")
  post "/users", do: send_resp(conn, 201, "Created")
  match _ , do: send_resp(conn, 404, "Not found")
end
`
	ents := extract(t, "custom_elixir_plug", fi("router.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Component", "MyApp.Router") {
		t.Error("expected MyApp.Router router component")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "plug::match") {
		t.Error("expected plug::match middleware pattern")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /hello") {
		t.Error("expected GET /hello route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestPlugBuilder(t *testing.T) {
	src := `
defmodule MyApp.Pipeline do
  use Plug.Builder

  plug Plug.Logger
  plug :authenticate
  plug MyApp.AuthPlug
end
`
	ents := extract(t, "custom_elixir_plug", fi("pipeline.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Component", "MyApp.Pipeline") {
		t.Error("expected MyApp.Pipeline builder component")
	}
}

func TestPlugCallImpl(t *testing.T) {
	src := `
defmodule MyApp.AuthPlug do
  @behaviour Plug

  def init(opts), do: opts

  def call(conn, opts) do
    case get_session(conn, :user_id) do
      nil -> conn |> send_resp(401, "Unauthorized") |> halt()
      _   -> conn
    end
  end
end
`
	ents := extract(t, "custom_elixir_plug", fi("auth_plug.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "call") {
		t.Error("expected Plug.call/2 operation")
	}
}

func TestPlugForward(t *testing.T) {
	src := `
defmodule MyApp.Router do
  use Plug.Router

  forward "/api", to: MyApp.API.Router
  forward "/admin", to: MyApp.Admin.Router
end
`
	ents := extract(t, "custom_elixir_plug", fi("router.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "forward:/api") {
		t.Error("expected forward:/api entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "forward:/admin") {
		t.Error("expected forward:/admin entity")
	}
}

func TestPlugNoMatch(t *testing.T) {
	src := `defmodule MyApp.Helper do\n  def add(a, b), do: a + b\nend`
	ents := extract(t, "custom_elixir_plug", fi("helper.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities from plain module, got %d", len(ents))
	}
}
