package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	// Register the Elixir route-hit extractor so the test_suite (carrying
	// e2e_route_calls) comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/elixir"
)

// Issue #4688 LIVE-REPRO (resolve side) — Phoenix ConnTest tests.
//
// Proves end-to-end that an Elixir/Phoenix ExUnit test calling a route by
// string — direct `get(conn, "/api/v1/...")` and piped `conn |> get("/...")` —
// links to the http_endpoint_definition it exercises. The Elixir slice of the
// all-language program (#4615), generalizing #4351 / #4369 / #4370 / #4371 /
// #4684 / #4685 / #4686 / #4687. The shared linkE2ERouteTestsToEndpoints pass
// is language-agnostic; only the Elixir route capture is new. Elixir is
// functional (no OO receiver objects) so receiver typing does not apply; the
// route-string → endpoint linkage is the coverage mechanism.

const elxPipedTestSrc4688 = `defmodule MyAppWeb.ProposalControllerTest do
  use MyAppWeb.ConnCase

  test "lists counts", %{conn: conn} do
    conn = conn |> get("/api/v1/proposals/get_counts")
    assert json_response(conn, 200)
  end

  test "creates a proposal", %{conn: conn} do
    conn = conn |> post("/api/v1/proposals", %{name: "x"})
    assert json_response(conn, 201)
  end
end
`

const elxDirectTestSrc4688 = `defmodule MyAppWeb.XControllerTest do
  use MyAppWeb.ConnCase

  test "gets counts", %{conn: conn} do
    conn = get(conn, "/api/v1/proposals/get_counts")
    assert json_response(conn, 200)
  end

  test "creates", %{conn: conn} do
    conn = post(conn, "/api/v1/proposals", %{name: "x"})
    assert json_response(conn, 201)
  end
end
`

func TestIssue4688_ElixirPhoenixPipedE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("GET", "/api/v1/proposals/get_counts"),
		def("POST", "/api/v1/proposals"),
	}
	suite := realSuite(t, "custom_elixir_tests_route_e2e",
		"test/my_app_web/controllers/proposal_controller_test.exs", "elixir", elxPipedTestSrc4688)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 2 {
		t.Fatalf("expected >=2 e2e route TESTS edges (GET + POST), got %d", edges)
	}
	assertElxRouteEdges(t, edgeTargets(afterOut))
}

func TestIssue4688_ElixirPhoenixDirectE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("GET", "/api/v1/proposals/get_counts"),
		def("POST", "/api/v1/proposals"),
	}
	suite := realSuite(t, "custom_elixir_tests_route_e2e",
		"test/my_app_web/controllers/x_controller_test.exs", "elixir", elxDirectTestSrc4688)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 2 {
		t.Fatalf("expected >=2 e2e route TESTS edges (GET + POST), got %d", edges)
	}
	assertElxRouteEdges(t, edgeTargets(afterOut))
}

func assertElxRouteEdges(t *testing.T, targets map[string]bool) {
	t.Helper()
	wantGet, wantPost := false, false
	for to := range targets {
		if strings.Contains(to, "GET:/api/v1/proposals/get_counts") {
			wantGet = true
		}
		if strings.Contains(to, "POST:/api/v1/proposals") {
			wantPost = true
		}
	}
	if !wantGet {
		t.Errorf("expected a TESTS edge to GET /api/v1/proposals/get_counts; targets=%v", targets)
	}
	if !wantPost {
		t.Errorf("expected a TESTS edge to POST /api/v1/proposals; targets=%v", targets)
	}
}
