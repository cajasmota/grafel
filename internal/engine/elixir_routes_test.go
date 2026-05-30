package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Phoenix LiveView — `live "/path", Mod, :action`  (#3468)
// ---------------------------------------------------------------------------

// TestPhoenixLive_Routes asserts the initial-GET endpoint synthesised for each
// LiveView route, including scope-prefix composition and :id normalisation.
func TestPhoenixLive_Routes(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use Phoenix.Router

  scope "/", MyAppWeb do
    live "/dashboard", DashboardLive, :index
    live "/users/:id", UserLive.Show, :show
  end
end
`
	ids, res := runDetect(t, "elixir", "router.ex", src)
	want := []string{
		"http:GET:/dashboard",
		"http:GET:/users/{id}",
	}
	requireContains(t, ids, want, "phoenix-live-routes")

	// Verify framework + handler attribution on the :id route.
	var found bool
	for _, e := range res.Entities {
		if e.ID != "http:GET:/users/{id}" {
			continue
		}
		found = true
		if e.Properties["framework"] != "phoenix_live" {
			t.Errorf("phoenix-live: framework=%q, want phoenix_live", e.Properties["framework"])
		}
		if e.Properties["source_handler"] != "SCOPE.Operation:show" {
			t.Errorf("phoenix-live: source_handler=%q, want SCOPE.Operation:show", e.Properties["source_handler"])
		}
		if e.Properties["handler_file"] != "show" {
			// phoenixControllerHint("UserLive.Show") → snake_case of "Show" = "show"
			t.Errorf("phoenix-live: handler_file=%q, want show", e.Properties["handler_file"])
		}
	}
	if !found {
		t.Errorf("phoenix-live: missing http:GET:/users/{id}")
	}
}

// TestPhoenixLive_NoAction covers the `live "/path", Mod` form (no :action atom).
func TestPhoenixLive_NoAction(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use Phoenix.Router
  scope "/admin", MyAppWeb do
    live "/panel", AdminLive
  end
end
`
	ids, _ := runDetect(t, "elixir", "router.ex", src)
	requireContains(t, ids, []string{"http:GET:/admin/panel"}, "phoenix-live-no-action")
}

// ---------------------------------------------------------------------------
// Plug.Router — verb routes + forward  (#3468)
// ---------------------------------------------------------------------------

// TestPlugRouter_Verbs asserts each verb route from a Plug.Router module,
// including the :id colon param and the inline `do:` / `do ... end` forms.
func TestPlugRouter_Verbs(t *testing.T) {
	src := `
defmodule MyApp.Router do
  use Plug.Router

  plug :match
  plug :dispatch

  get "/health", do: send_resp(conn, 200, "ok")
  get "/users/:id" do
    send_resp(conn, 200, "user")
  end
  post "/users", do: send_resp(conn, 201, "created")
  delete "/users/:id", do: send_resp(conn, 204, "")
  forward "/admin", to: MyApp.Admin.Router

  match _, do: send_resp(conn, 404, "nope")
end
`
	ids, res := runDetect(t, "elixir", "router.ex", src)
	want := []string{
		"http:GET:/health",
		"http:GET:/users/{id}",
		"http:POST:/users",
		"http:DELETE:/users/{id}",
		"http:ANY:/admin",
	}
	requireContains(t, ids, want, "plug-router-verbs")

	// `match _` is a catch-all, not a path route — must NOT be emitted.
	for _, e := range res.Entities {
		if e.ID == "http:ANY:/_" || e.ID == "http:GET:/_" {
			t.Errorf("plug-router: catch-all match _ wrongly synthesised as %s", e.ID)
		}
	}

	// Verify framework + router-module attribution on a verb route.
	var found bool
	for _, e := range res.Entities {
		if e.ID != "http:POST:/users" {
			continue
		}
		found = true
		if e.Properties["framework"] != "plug" {
			t.Errorf("plug-router: framework=%q, want plug", e.Properties["framework"])
		}
		if e.Properties["source_handler"] != "SCOPE.Component:MyApp.Router" {
			t.Errorf("plug-router: source_handler=%q, want SCOPE.Component:MyApp.Router", e.Properties["source_handler"])
		}
	}
	if !found {
		t.Errorf("plug-router: missing http:POST:/users")
	}
}

// TestPlugRouter_NotPhoenix ensures a non-Plug.Router Elixir file produces no
// Plug endpoints (no false positives from bare verb-like identifiers).
func TestPlugRouter_NotPlug(t *testing.T) {
	src := `
defmodule MyApp.Helper do
  def get(key), do: Map.get(state(), key)
  def post(data), do: data
end
`
	ids, _ := runDetect(t, "elixir", "helper.ex", src)
	for _, id := range ids {
		if id == "http:GET:/key" || id == "http:POST:/data" {
			t.Errorf("plug-router: false positive endpoint %s from plain module", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Cowboy dispatch  (#3468)
// ---------------------------------------------------------------------------

// TestCowboy_Dispatch asserts an ANY endpoint per dispatch-table route, with
// :id normalisation and handler attribution, skipping the `:_` host wildcard.
func TestCowboy_Dispatch(t *testing.T) {
	src := `
defmodule MyApp.Server do
  def dispatch do
    :cowboy_router.compile([
      {:_, [
        {"/", MyApp.IndexHandler, []},
        {"/users/:id", MyApp.UserHandler, []},
        {"/ws", MyApp.SocketHandler, []}
      ]}
    ])
  end
end
`
	ids, res := runDetect(t, "elixir", "server.ex", src)
	want := []string{
		"http:ANY:/",
		"http:ANY:/users/{id}",
		"http:ANY:/ws",
	}
	requireContains(t, ids, want, "cowboy-dispatch")

	var found bool
	for _, e := range res.Entities {
		if e.ID != "http:ANY:/users/{id}" {
			continue
		}
		found = true
		if e.Properties["framework"] != "cowboy" {
			t.Errorf("cowboy: framework=%q, want cowboy", e.Properties["framework"])
		}
		if e.Properties["source_handler"] != "SCOPE.Component:MyApp.UserHandler" {
			t.Errorf("cowboy: source_handler=%q, want SCOPE.Component:MyApp.UserHandler", e.Properties["source_handler"])
		}
	}
	if !found {
		t.Errorf("cowboy: missing http:ANY:/users/{id}")
	}
}

// ---------------------------------------------------------------------------
// Absinthe GraphQL  (#3468)
// ---------------------------------------------------------------------------

// TestAbsinthe_Schema asserts a GraphQL field endpoint per top-level field in
// query/mutation blocks, excluding nested object-type fields.
func TestAbsinthe_Schema(t *testing.T) {
	src := `
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  object :user do
    field :name, :string
    field :email, :string
  end

  query do
    field :users, list_of(:user) do
      resolve &Resolvers.list_users/3
    end

    field :user, :user do
      arg :id, non_null(:id)
      resolve &Resolvers.get_user/3
    end
  end

  mutation do
    field :create_user, :user do
      resolve &Resolvers.create_user/3
    end
  end
end
`
	ids, res := runDetect(t, "elixir", "schema.ex", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/users",
		"http:GRAPHQL:/graphql/Query/user",
		"http:GRAPHQL:/graphql/Mutation/create_user",
	}
	requireContains(t, ids, want, "absinthe-schema")

	// Nested object-type fields (:name, :email) must NOT become endpoints.
	for _, bad := range []string{
		"http:GRAPHQL:/graphql/Query/name",
		"http:GRAPHQL:/graphql/Query/email",
		"http:GRAPHQL:/graphql/Mutation/name",
	} {
		for _, id := range ids {
			if id == bad {
				t.Errorf("absinthe: object-type field wrongly emitted as %s", bad)
			}
		}
	}

	var found bool
	for _, e := range res.Entities {
		if e.ID != "http:GRAPHQL:/graphql/Mutation/create_user" {
			continue
		}
		found = true
		if e.Properties["framework"] != "absinthe" {
			t.Errorf("absinthe: framework=%q, want absinthe", e.Properties["framework"])
		}
		if e.Properties["source_handler"] != "SCOPE.Operation:Mutation.create_user" {
			t.Errorf("absinthe: source_handler=%q, want SCOPE.Operation:Mutation.create_user", e.Properties["source_handler"])
		}
	}
	if !found {
		t.Errorf("absinthe: missing http:GRAPHQL:/graphql/Mutation/create_user")
	}
}
