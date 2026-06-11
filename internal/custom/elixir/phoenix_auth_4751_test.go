package elixir_test

// phoenix_auth_4751_test.go — #4751 Phoenix scope ▸ pipe_through ▸ pipeline ▸ plug
// resolution: the extractor now stamps auth_pipelines/auth_plugs (+ role literal)
// and router_source onto each route SCOPE.Operation/endpoint, so the authposture
// phoenix resolver decodes the route's effective posture structurally.

import "testing"

func phoenixEndpoint(t *testing.T, ents []entitySummary, name string) map[string]string {
	t.Helper()
	for _, e := range ents {
		if e.Name == name && e.Subtype == "endpoint" {
			return e.Props
		}
	}
	t.Fatalf("no endpoint %q; got %+v", name, ents)
	return nil
}

func TestPhoenixAuth_PipeThroughResolved_4751(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use MyAppWeb, :router

  pipeline :browser do
    plug :fetch_session
  end

  pipeline :auth do
    plug Guardian.Plug.EnsureAuthenticated
  end

  scope "/dashboard", MyAppWeb do
    pipe_through [:browser, :auth]
    get "/", DashboardController, :index
  end

  scope "/", MyAppWeb do
    pipe_through :browser
    get "/about", PageController, :about
  end
end
`
	ents := extract(t, "custom_elixir_phoenix", fi("lib/my_app_web/router.ex", "elixir", src))

	protectedEp := phoenixEndpoint(t, ents, "GET /")
	if protectedEp["auth_pipelines"] != "browser,auth" {
		t.Errorf("auth_pipelines=%q, want browser,auth", protectedEp["auth_pipelines"])
	}
	if protectedEp["auth_plugs"] == "" {
		t.Errorf("auth_plugs not stamped on protected route")
	}
	if protectedEp["router_source"] == "" {
		t.Errorf("router_source not stamped")
	}

	// The /about route pipes only through :browser (no auth pipeline) — it should
	// carry the browser pipeline but no auth plug.
	publicEp := phoenixEndpoint(t, ents, "GET /about")
	if publicEp["auth_plugs"] != "" {
		t.Errorf("public route should have no auth_plugs, got %q", publicEp["auth_plugs"])
	}
}

func TestPhoenixAuth_RolePlugLiteral_4751(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  pipeline :editor do
    plug :require_role, :editor
  end

  scope "/posts", MyAppWeb do
    pipe_through [:editor]
    post "/", PostController, :create
  end
end
`
	ents := extract(t, "custom_elixir_phoenix", fi("lib/my_app_web/router.ex", "elixir", src))
	ep := phoenixEndpoint(t, ents, "POST /")
	if ep["auth_roles"] != "editor" {
		t.Errorf("auth_roles=%q, want editor", ep["auth_roles"])
	}
}
