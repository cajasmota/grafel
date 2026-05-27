package engine

import (
	"testing"
)

// TestPhoenix_VerbInScope covers `scope "/api", MyAppWeb do ... get "/users", UserController, :index end`.
func TestPhoenix_VerbInScope(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use Phoenix.Router

  scope "/api", MyAppWeb do
    get "/users", UserController, :index
    post "/users", UserController, :create
    get "/users/:id", UserController, :show
  end
end
`
	ids, _ := runDetect(t, "elixir", "router.ex", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
		"http:GET:/api/users/{id}",
	}
	requireContains(t, ids, want, "phoenix-verb-in-scope")
}

// TestPhoenix_Resources covers `resources "/widgets", WidgetController` →
// 7 standard CRUD endpoints.
func TestPhoenix_Resources(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use Phoenix.Router
  scope "/api", MyAppWeb do
    resources "/widgets", WidgetController
  end
end
`
	ids, _ := runDetect(t, "elixir", "router.ex", src)
	want := []string{
		"http:GET:/api/widgets",
		"http:GET:/api/widgets/new",
		"http:POST:/api/widgets",
		"http:GET:/api/widgets/{id}",
		"http:GET:/api/widgets/{id}/edit",
		"http:PUT:/api/widgets/{id}",
		"http:DELETE:/api/widgets/{id}",
	}
	requireContains(t, ids, want, "phoenix-resources")
}

// TestPhoenix_ResourcesOnly verifies `only: [:index, :show]` restricts the
// emitted CRUD set.
func TestPhoenix_ResourcesOnly(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use Phoenix.Router
  scope "/api", MyAppWeb do
    resources "/widgets", WidgetController, only: [:index, :show]
  end
end
`
	ids, _ := runDetect(t, "elixir", "router.ex", src)
	want := []string{
		"http:GET:/api/widgets",
		"http:GET:/api/widgets/{id}",
	}
	requireContains(t, ids, want, "phoenix-resources-only")
	// Spot-check that the create/update/delete variants are NOT emitted.
	for _, bad := range []string{
		"http:POST:/api/widgets",
		"http:PUT:/api/widgets/{id}",
		"http:DELETE:/api/widgets/{id}",
	} {
		for _, g := range ids {
			if g == bad {
				t.Errorf("phoenix-resources-only: unexpected endpoint %s emitted (only: filter not respected)", bad)
			}
		}
	}
}

// TestPhoenix_NestedScope covers `scope "/api" do; scope "/v1" do; ... end; end`.
func TestPhoenix_NestedScope(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use Phoenix.Router
  scope "/api", MyAppWeb do
    scope "/v1" do
      get "/health", HealthController, :index
    end
  end
end
`
	ids, _ := runDetect(t, "elixir", "router.ex", src)
	want := []string{"http:GET:/api/v1/health"}
	requireContains(t, ids, want, "phoenix-nested-scope")
}

// TestPhoenix_ControllerHandlerRef verifies the synthesizer stamps the
// controller-module snake_case basename as the `handler_file` property
// (used by the resolver for cross-file substring disambiguation) and
// keeps the source_handler ref bare (no `@<hint>` encoding leaks).
func TestPhoenix_ControllerHandlerRef(t *testing.T) {
	src := `
defmodule MyAppWeb.Router do
  use Phoenix.Router
  scope "/api", MyAppWeb do
    get "/users", UserController, :index
  end
end
`
	_, res := runDetect(t, "elixir", "router.ex", src)
	found := false
	for _, e := range res.Entities {
		if e.ID != "http:GET:/api/users" {
			continue
		}
		if e.Properties["source_handler"] != "SCOPE.Operation:index" {
			t.Errorf("phoenix-handler-ref: source_handler=%q, want SCOPE.Operation:index", e.Properties["source_handler"])
		}
		if e.Properties["handler_file"] != "user_controller" {
			t.Errorf("phoenix-handler-ref: handler_file=%q, want user_controller", e.Properties["handler_file"])
		}
		found = true
	}
	if !found {
		t.Errorf("phoenix-handler-ref: missing http:GET:/api/users synthetic")
	}
}
