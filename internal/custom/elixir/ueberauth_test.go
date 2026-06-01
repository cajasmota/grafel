package elixir_test

import "testing"

// TestUeberauthConfig asserts that configured Ueberauth strategies become
// OAuth provider auth entities, each carrying auth=true so auth_coverage
// counts them, with the provider slug derived from the strategy module.
func TestUeberauthConfig(t *testing.T) {
	src := `
config :ueberauth, Ueberauth,
  providers: [
    github: {Ueberauth.Strategy.Github, [default_scope: "user:email"]},
    google: {Ueberauth.Strategy.Google, [default_scope: "email profile"]}
  ]
`
	ents := extract(t, "custom_elixir_ueberauth", fi("config.exs", "elixir", src))

	gh := findEntity(ents, "SCOPE.Component", "Ueberauth.Strategy.Github")
	if gh == nil {
		t.Fatal("expected Ueberauth.Strategy.Github auth entity")
	}
	if got := gh.Props["auth"]; got != "true" {
		t.Error("expected auth=true on Github strategy")
	}
	if got := gh.Props["auth_provider"]; got != "github" {
		t.Errorf("expected auth_provider github, got %q", got)
	}
	if got := gh.Props["auth_method"]; got != "oauth2" {
		t.Errorf("expected auth_method oauth2, got %q", got)
	}
	if got := gh.Props["oauth_provider"]; got != "github" {
		t.Errorf("expected oauth_provider github, got %q", got)
	}

	if findEntity(ents, "SCOPE.Component", "Ueberauth.Strategy.Google") == nil {
		t.Error("expected Ueberauth.Strategy.Google auth entity")
	}
}

// TestUeberauthRouterPlug asserts `plug Ueberauth` in a router becomes the
// OAuth pipeline entrypoint auth entity.
func TestUeberauthRouterPlug(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use MyAppWeb, :router

  pipeline :auth do
    plug Ueberauth
  end
end
`
	ents := extract(t, "custom_elixir_ueberauth", fi("router.ex", "elixir", src))
	plug := findEntity(ents, "SCOPE.Component", "plug:Ueberauth")
	if plug == nil {
		t.Fatal("expected plug:Ueberauth entrypoint entity")
	}
	if got := plug.Props["auth"]; got != "true" {
		t.Error("expected auth=true on plug:Ueberauth")
	}
	if got := plug.Props["auth_method"]; got != "oauth2" {
		t.Errorf("expected auth_method oauth2, got %q", got)
	}
}

// TestUeberauthStrategyCallbacks asserts a custom strategy's request/callback
// handlers are recorded as OAuth auth operations.
func TestUeberauthStrategyCallbacks(t *testing.T) {
	src := `
defmodule Ueberauth.Strategy.Acme do
  use Ueberauth.Strategy

  def handle_request!(conn) do
    redirect!(conn, "https://acme.example/oauth/authorize")
  end

  def handle_callback!(conn) do
    conn
  end
end
`
	ents := extract(t, "custom_elixir_ueberauth", fi("acme.ex", "elixir", src))

	req := findEntity(ents, "SCOPE.Operation", "handle_request!")
	if req == nil {
		t.Fatal("expected handle_request! auth operation")
	}
	if got := req.Props["oauth_phase"]; got != "request" {
		t.Errorf("expected oauth_phase request, got %q", got)
	}
	if got := req.Props["auth"]; got != "true" {
		t.Error("expected auth=true on handle_request!")
	}

	cb := findEntity(ents, "SCOPE.Operation", "handle_callback!")
	if cb == nil {
		t.Fatal("expected handle_callback! auth operation")
	}
	if got := cb.Props["oauth_phase"]; got != "callback" {
		t.Errorf("expected oauth_phase callback, got %q", got)
	}

	// The strategy module itself is detected via the Ueberauth.Strategy.Acme ref.
	if findEntity(ents, "SCOPE.Component", "Ueberauth.Strategy.Acme") == nil {
		t.Error("expected Ueberauth.Strategy.Acme provider entity")
	}
}

func TestUeberauthNoMatch(t *testing.T) {
	src := `
defmodule MyApp.Plain do
  def hello, do: :world
end
`
	ents := extract(t, "custom_elixir_ueberauth", fi("plain.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities from non-Ueberauth module, got %d", len(ents))
	}
}
