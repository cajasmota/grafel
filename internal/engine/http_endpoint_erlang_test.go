package engine

import (
	"testing"
)

// #4749 — Erlang Cowboy producer-side route synthesis. Proves an Erlang
// `cowboy_router:compile([{'_', [{"/path", handler, []}]}])` dispatch table in
// an `.erl` file (language="erlang") emits the same canonical
// http_endpoint_definition the Elixir Cowboy producer emits, now that `case
// "erlang"` is wired into the synthesis dispatch and Erlang is allowed through
// synthesisSupportsLanguage. The synthesizer (synthesizeCowboy) is shared with
// Elixir; this guards that it actually fires for Erlang-classified files.

// TestErlang_CowboyDispatch asserts an ANY endpoint per dispatch-table route,
// with `:id` colon-param normalisation and handler attribution, skipping the
// `'_'` host wildcard.
func TestErlang_CowboyDispatch(t *testing.T) {
	src := `-module(myapp_app).
-behaviour(application).
-export([start/2]).

start(_Type, _Args) ->
    Dispatch = cowboy_router:compile([
        {'_', [
            {"/", index_handler, []},
            {"/users/:id", user_handler, []},
            {"/ws", socket_handler, []}
        ]}
    ]),
    {ok, _} = cowboy:start_clear(http, [{port, 8080}],
        #{env => #{dispatch => Dispatch}}).
`
	ids, res := runDetect(t, "erlang", "src/myapp_app.erl", src)
	want := []string{
		"http:ANY:/",
		"http:ANY:/users/{id}",
		"http:ANY:/ws",
	}
	requireContains(t, ids, want, "erlang-cowboy-dispatch")

	var found bool
	for _, e := range res.Entities {
		if e.ID != "http:ANY:/users/{id}" {
			continue
		}
		found = true
		if e.Properties["framework"] != "cowboy" {
			t.Errorf("erlang cowboy: framework=%q, want cowboy", e.Properties["framework"])
		}
	}
	if !found {
		t.Errorf("erlang cowboy: missing http:ANY:/users/{id}")
	}
}

// TestErlang_NonCowboyTupleIgnored is the negative guard: an Erlang tuple of a
// string + atom in a file with NO cowboy_router signal must not forge a route.
func TestErlang_NonCowboyTupleIgnored(t *testing.T) {
	src := `-module(config).
-export([routes/0]).

routes() ->
    [{"/not-a-route", some_atom, []}].
`
	ids, _ := runDetect(t, "erlang", "src/config.erl", src)
	for _, id := range ids {
		if id == "http:ANY:/not-a-route" {
			t.Fatalf("non-cowboy tuple forged an endpoint: %s", id)
		}
	}
}
