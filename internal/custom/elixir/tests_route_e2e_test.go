package elixir

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// #4688 — Phoenix ConnTest route-hit capture unit tests (extractor side).

func extractElixirRouteSuite(t *testing.T, path, src string) (types.EntityRecord, bool) {
	t.Helper()
	ex := &elixirTestRouteE2EExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "elixir", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			return e, true
		}
	}
	return types.EntityRecord{}, false
}

func routeCallSet(e types.EntityRecord) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(e.Properties["e2e_route_calls"], "\n") {
		if l != "" {
			out[l] = true
		}
	}
	return out
}

func TestElixirRouteE2E_PipedAndDirect(t *testing.T) {
	src := `defmodule MyAppWeb.XControllerTest do
  use MyAppWeb.ConnCase
  test "a", %{conn: conn} do
    conn = conn |> get("/api/v1/x/get_counts")
    conn = post(conn, "/api/v1/x", %{a: 1})
    conn = conn |> put("/api/v1/x/1")
    conn = patch(conn, "/api/v1/x/1")
    conn = conn |> delete("/api/v1/x/1")
  end
end
`
	e, ok := extractElixirRouteSuite(t, "test/x_controller_test.exs", src)
	if !ok {
		t.Fatal("expected a test_suite")
	}
	got := routeCallSet(e)
	for _, want := range []string{
		"GET /api/v1/x/get_counts",
		"POST /api/v1/x",
		"PUT /api/v1/x/1",
		"PATCH /api/v1/x/1",
		"DELETE /api/v1/x/1",
	} {
		if !got[want] {
			t.Errorf("missing route call %q; got %v", want, got)
		}
	}
	if e.Properties["framework"] != "phoenix" {
		t.Errorf("framework = %q, want phoenix", e.Properties["framework"])
	}
}

func TestElixirRouteE2E_ShapeOnlyNoSuite(t *testing.T) {
	// Asserts on a struct, never hits a route → no suite (honest exclusion).
	src := `defmodule MyApp.UserTest do
  use ExUnit.Case
  test "builds a user struct" do
    user = %User{name: "x"}
    assert user.name == "x"
  end
end
`
	if _, ok := extractElixirRouteSuite(t, "test/user_test.exs", src); ok {
		t.Fatal("shape-only test must NOT emit a route-hit suite")
	}
}

func TestElixirRouteE2E_InterpolatedAndHelperDropped(t *testing.T) {
	// Interpolated route + router-helper form are not statically recoverable.
	src := `defmodule MyAppWeb.XControllerTest do
  use MyAppWeb.ConnCase
  test "dynamic", %{conn: conn} do
    id = 1
    conn = conn |> get("/api/v1/x/#{id}")
    conn = get(conn, Routes.x_path(conn, :show, id))
    conn = conn |> get(path)
  end
  test "static control", %{conn: conn} do
    conn = conn |> get("/api/v1/x/static")
  end
end
`
	e, ok := extractElixirRouteSuite(t, "test/x_controller_test.exs", src)
	if !ok {
		t.Fatal("expected a test_suite from the static control call")
	}
	got := routeCallSet(e)
	if !got["GET /api/v1/x/static"] {
		t.Errorf("expected the static route to survive; got %v", got)
	}
	for bad := range got {
		if strings.Contains(bad, "#{") || strings.Contains(bad, "Routes.") {
			t.Errorf("dynamic/helper route leaked: %q", bad)
		}
	}
	if len(got) != 1 {
		t.Errorf("expected exactly 1 captured route, got %d: %v", len(got), got)
	}
}

func TestElixirRouteE2E_NonTestFileIgnored(t *testing.T) {
	// A production controller that mentions a route string must not be a suite.
	src := `defmodule MyAppWeb.XController do
  def index(conn, _params) do
    get(conn, "/api/v1/x")
  end
end
`
	if _, ok := extractElixirRouteSuite(t, "lib/my_app_web/controllers/x_controller.ex", src); ok {
		t.Fatal("non-test file must not emit a route-hit suite")
	}
}
