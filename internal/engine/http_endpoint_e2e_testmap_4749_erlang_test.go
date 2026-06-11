package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	// Register the Erlang route-hit extractor so the test_suite (carrying
	// e2e_route_calls) comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/erlang"
)

// Issue #4749 LIVE-REPRO (resolve side) — Erlang eunit / common_test tests.
//
// Proves end-to-end that an Erlang eunit test calling a route by string via
// httpc / gun / hackney against a test Cowboy server links to the
// http_endpoint_definition it exercises. The Erlang slice of the all-language
// program (#4615 tail #4749), generalizing the shared
// linkE2ERouteTestsToEndpoints pass (#4351). The pass is language-agnostic; only
// the Erlang route capture (custom_erlang_tests_route_e2e) and wiring the
// shared Cowboy producer (synthesizeCowboy) for `case "erlang"` are new. Erlang
// is functional / process-based (no OO receiver objects) so receiver typing does
// not apply; the route-string → endpoint linkage is the coverage mechanism.

const erlHttpcTestSrc4749 = `-module(todo_handler_tests).
-include_lib("eunit/include/eunit.hrl").

list_test() ->
    {ok, {{_, 200, _}, _, _}} =
        httpc:request(get, {"http://localhost:8080/todos", []}, [], []).

create_test() ->
    {ok, _} =
        httpc:request(post, {"http://localhost:8080/todos", [], "application/json", "{}"}, [], []).
`

func TestIssue4749_ErlangHttpcE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("ANY", "/todos"),
	}
	suite := realSuite(t, "custom_erlang_tests_route_e2e",
		"test/todo_handler_tests.erl", "erlang", erlHttpcTestSrc4749)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 1 {
		t.Fatalf("expected >=1 e2e route TESTS edge to ANY /todos, got %d", edges)
	}
	assertErlRouteEdges(t, edgeTargets(afterOut))
}

// Path-param variant: a gun:get(Conn, "/todos/1") hit matches the templated ANY
// /todos/{id} definition via the resolver's concrete-vs-template matcher.
const erlGunParamTestSrc4749 = `-module(todo_handler_tests).
-include_lib("eunit/include/eunit.hrl").

get_one_test() ->
    {ok, _} = gun:get(Conn, "/todos/1").
`

func TestIssue4749_ErlangGunPathParamLinks(t *testing.T) {
	defs := []types.EntityRecord{
		def("ANY", "/todos/{id}"),
	}
	suite := realSuite(t, "custom_erlang_tests_route_e2e",
		"test/todo_handler_tests.erl", "erlang", erlGunParamTestSrc4749)

	_, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 1 {
		t.Fatalf("expected a TESTS edge to ANY /todos/{id}, got %d", edges)
	}
}

func assertErlRouteEdges(t *testing.T, targets map[string]bool) {
	t.Helper()
	for to := range targets {
		if strings.Contains(to, ":/todos") {
			return
		}
	}
	t.Errorf("expected a TESTS edge to /todos; targets=%v", targets)
}
