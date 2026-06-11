package erlang

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// extract is a small helper that runs the route-hit extractor and returns the
// captured e2e_route_calls lines (or empty when no suite is emitted).
func extractRouteCalls(t *testing.T, path, src string) []string {
	t.Helper()
	e := &erlangTestRouteE2EExtractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Language: "erlang",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(out) == 0 {
		return nil
	}
	if len(out) != 1 {
		t.Fatalf("expected one suite, got %d", len(out))
	}
	calls := out[0].Properties["e2e_route_calls"]
	if calls == "" {
		return nil
	}
	return strings.Split(calls, "\n")
}

func hasCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// TestErlang_HttpcVerbTuple covers the canonical eunit httpc:request verb-tuple
// form against a test Cowboy server, with the host stripped to a path.
func TestErlang_HttpcVerbTuple(t *testing.T) {
	src := `
-module(todo_handler_tests).
-include_lib("eunit/include/eunit.hrl").

list_test() ->
    {ok, {{_, 200, _}, _, _}} =
        httpc:request(get, {"http://localhost:8080/todos", []}, [], []).

create_test() ->
    {ok, _} =
        httpc:request(post, {"http://localhost:8080/todos", [], "application/json", "{}"}, [], []).
`
	calls := extractRouteCalls(t, "test/todo_handler_tests.erl", src)
	if !hasCall(calls, "GET /todos") {
		t.Errorf("missing GET /todos; got %v", calls)
	}
	if !hasCall(calls, "POST /todos") {
		t.Errorf("missing POST /todos; got %v", calls)
	}
}

// TestErlang_HttpcBareGet covers the 1-arity httpc:request("url") GET form and
// must not also double-count via the verb-tuple regex.
func TestErlang_HttpcBareGet(t *testing.T) {
	src := `
-module(health_SUITE).
health(_Config) ->
    {ok, _} = httpc:request("http://localhost:8080/health/live").
`
	calls := extractRouteCalls(t, "test/health_SUITE.erl", src)
	if !hasCall(calls, "GET /health/live") {
		t.Errorf("missing GET /health/live; got %v", calls)
	}
	if len(calls) != 1 {
		t.Errorf("expected exactly one call (no double-count), got %v", calls)
	}
}

// TestErlang_GunAndHackney covers gun:verb and hackney:verb forms, including a
// hackney binary URL and a path param.
func TestErlang_GunAndHackney(t *testing.T) {
	src := `
-module(api_tests).
get_one_test() ->
    gun:get(Conn, "/users/:id").
post_test() ->
    gun:post(Conn, "/users", [], <<"{}">>).
health_test() ->
    hackney:get(<<"http://localhost:8080/health">>, [], <<>>, []).
del_test() ->
    hackney:request(delete, <<"http://localhost:8080/users/1">>, [], <<>>, []).
`
	calls := extractRouteCalls(t, "test/api_tests.erl", src)
	for _, want := range []string{"GET /users/:id", "POST /users", "GET /health", "DELETE /users/1"} {
		if !hasCall(calls, want) {
			t.Errorf("missing %q; got %v", want, calls)
		}
	}
}

// TestErlang_BuiltURLExcluded is the honest-exclusion guard: a `++`-built URL
// (the common eunit/CT ephemeral-port shape) is NOT statically recoverable and
// must not forge a route hit.
func TestErlang_BuiltURLExcluded(t *testing.T) {
	src := `
-module(built_tests).
go_test() ->
    Url = "http://localhost:" ++ integer_to_list(Port) ++ "/users",
    httpc:request(get, {Url, []}, [], []).
`
	calls := extractRouteCalls(t, "test/built_tests.erl", src)
	if len(calls) != 0 {
		t.Errorf("built URL should yield no route hits, got %v", calls)
	}
}

// TestErlang_NonTestFileIgnored ensures production .erl handlers do not emit a
// route-hit suite even if they mention an HTTP client call.
func TestErlang_NonTestFileIgnored(t *testing.T) {
	src := `
-module(user_handler).
init(Req, State) ->
    httpc:request("http://example.com/upstream"),
    {ok, Req, State}.
`
	calls := extractRouteCalls(t, "src/user_handler.erl", src)
	if calls != nil {
		t.Errorf("non-test file should emit no suite, got %v", calls)
	}
}
