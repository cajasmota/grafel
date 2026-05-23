package erlang_test

import (
	"context"
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/erlang"
)

// genServerFixture is a minimal OTP gen_server used as a fixture for recall tests.
const genServerFixture = `
-module(cache_server).

-behaviour(gen_server).

-include("cache.hrl").

-export([start_link/0, get/1, put/2, delete/1, flush/0]).
-export([init/1, handle_call/3, handle_cast/2, handle_info/2,
         terminate/2, code_change/3]).

-define(SERVER, ?MODULE).

start_link() ->
    gen_server:start_link({local, ?SERVER}, ?MODULE, [], []).

get(Key) ->
    gen_server:call(?SERVER, {get, Key}).

put(Key, Value) ->
    gen_server:cast(?SERVER, {put, Key, Value}).

delete(Key) ->
    gen_server:cast(?SERVER, {delete, Key}).

flush() ->
    gen_server:cast(?SERVER, flush).

init([]) ->
    State = maps:new(),
    {ok, State}.

handle_call({get, Key}, _From, State) ->
    Value = maps:get(Key, State, undefined),
    {reply, Value, State};
handle_call(_Request, _From, State) ->
    {reply, ok, State}.

handle_cast({put, Key, Value}, State) ->
    NewState = maps:put(Key, Value, State),
    {noreply, NewState};
handle_cast({delete, Key}, State) ->
    NewState = maps:remove(Key, State),
    {noreply, NewState};
handle_cast(flush, _State) ->
    {noreply, maps:new()};
handle_cast(_Msg, State) ->
    {noreply, State}.

handle_info(_Info, State) ->
    {noreply, State}.

terminate(_Reason, _State) ->
    ok.

code_change(_OldVsn, State, _Extra) ->
    {ok, State}.
`

func ext(t *testing.T) extractor.Extractor {
	t.Helper()
	e, ok := extractor.Get("erlang")
	if !ok {
		t.Fatal("erlang extractor not registered")
	}
	return e
}

func TestErlangExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("erlang")
	if !ok {
		t.Fatal("erlang extractor not registered")
	}
}

func TestErlangExtractor_EmptyFile(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.erl",
		Content:  []byte(""),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(got))
	}
}

func TestErlangExtractor_ModuleDeclaration(t *testing.T) {
	src := `-module(my_mod).
-export([hello/0]).

hello() ->
    ok.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "my_mod.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, rec := range got {
		if rec.Name == "my_mod" && rec.Kind == "SCOPE.Component" && rec.Subtype == "module" {
			found = true
			if rec.Language != "erlang" {
				t.Errorf("expected language=erlang, got %q", rec.Language)
			}
			if rec.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", rec.StartLine)
			}
		}
	}
	if !found {
		t.Error("expected entity my_mod with Kind=SCOPE.Component Subtype=module")
	}
}

func TestErlangExtractor_FunctionDeclarations(t *testing.T) {
	src := `-module(greet).
-export([hello/1, goodbye/1]).

hello(Name) ->
    io:format("Hello, ~s!~n", [Name]).

goodbye(Name) ->
    io:format("Goodbye, ~s!~n", [Name]).

helper() ->
    internal.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "greet.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	nameSet := make(map[string]string) // name → subtype
	for _, rec := range got {
		if rec.Kind == "SCOPE.Operation" {
			nameSet[rec.Name] = rec.Subtype
		}
	}
	for _, fn := range []string{"hello", "goodbye", "helper"} {
		if _, ok := nameSet[fn]; !ok {
			t.Errorf("expected function entity %q", fn)
		}
	}
	// hello and goodbye are exported
	if nameSet["hello"] != "exported_function" {
		t.Errorf("hello should be exported_function, got %q", nameSet["hello"])
	}
	if nameSet["goodbye"] != "exported_function" {
		t.Errorf("goodbye should be exported_function, got %q", nameSet["goodbye"])
	}
	// helper is not exported
	if nameSet["helper"] != "function" {
		t.Errorf("helper should be function, got %q", nameSet["helper"])
	}
}

func TestErlangExtractor_RecordDeclarations(t *testing.T) {
	src := `-module(user_store).
-record(user, {id, name, email}).
-record(session, {token, user_id, expires}).

-export([new_user/3]).

new_user(Id, Name, Email) ->
    #user{id = Id, name = Name, email = Email}.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "user_store.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	records := make(map[string]bool)
	for _, rec := range got {
		if rec.Kind == "SCOPE.Component" && rec.Subtype == "record" {
			records[rec.Name] = true
		}
	}
	for _, r := range []string{"user", "session"} {
		if !records[r] {
			t.Errorf("expected record entity %q", r)
		}
	}
}

func TestErlangExtractor_IncludeImports(t *testing.T) {
	src := `-module(worker).
-include("common.hrl").
-include_lib("kernel/include/logger.hrl").

-export([run/0]).

run() ->
    ok.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "worker.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	imports := make(map[string]bool)
	for _, rec := range got {
		for _, rel := range rec.Relationships {
			if rel.Kind == "IMPORTS" {
				imports[rel.ToID] = true
			}
		}
	}
	if !imports["common.hrl"] {
		t.Error("expected IMPORTS edge for common.hrl")
	}
	if !imports["kernel/include/logger.hrl"] {
		t.Error("expected IMPORTS edge for kernel/include/logger.hrl")
	}
}

func TestErlangExtractor_CallsEdge(t *testing.T) {
	src := `-module(pipeline).
-export([process/1]).

process(Data) ->
    Cleaned = validate(Data),
    Result = transform(Cleaned),
    lists:reverse(Result).

validate(D) ->
    D.

transform(D) ->
    D.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "pipeline.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var callTargets []string
	for _, rec := range got {
		if rec.Name == "process" && rec.Kind == "SCOPE.Operation" {
			for _, rel := range rec.Relationships {
				if rel.Kind == "CALLS" {
					callTargets = append(callTargets, rel.ToID)
				}
			}
		}
	}
	sort.Strings(callTargets)
	if len(callTargets) == 0 {
		t.Error("expected at least one CALLS edge from process/1")
	}
	// Should call validate and transform
	var foundValidate, foundTransform bool
	for _, t2 := range callTargets {
		if t2 == "validate" {
			foundValidate = true
		}
		if t2 == "transform" {
			foundTransform = true
		}
	}
	if !foundValidate {
		t.Error("expected CALLS edge to validate")
	}
	if !foundTransform {
		t.Error("expected CALLS edge to transform")
	}
}

func TestErlangExtractor_QualifiedCallsEdge(t *testing.T) {
	src := `-module(server).
-export([start/0]).

start() ->
    gen_server:start_link({local, server}, ?MODULE, [], []).
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "server.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var foundQualified bool
	for _, rec := range got {
		if rec.Name == "start" {
			for _, rel := range rec.Relationships {
				if rel.Kind == "CALLS" && rel.ToID == "gen_server:start_link" {
					foundQualified = true
				}
			}
		}
	}
	if !foundQualified {
		t.Error("expected CALLS edge with ToID=gen_server:start_link")
	}
}

func TestErlangExtractor_ContainsEdge(t *testing.T) {
	src := `-module(api).
-export([create/1, read/1]).

create(Item) ->
    {ok, Item}.

read(Id) ->
    {ok, Id}.

private_helper() ->
    ok.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "api.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var containsCount int
	for _, rec := range got {
		if rec.Name == "api" && rec.Kind == "SCOPE.Component" && rec.Subtype == "module" {
			for _, rel := range rec.Relationships {
				if rel.Kind == "CONTAINS" {
					containsCount++
				}
			}
		}
	}
	// Should contain create and read (exported), but NOT private_helper.
	if containsCount < 2 {
		t.Errorf("expected at least 2 CONTAINS edges from module api, got %d", containsCount)
	}
}

func TestErlangExtractor_MultiClauseFunctions(t *testing.T) {
	src := `-module(fib).
-export([fib/1]).

fib(0) -> 0;
fib(1) -> 1;
fib(N) when N > 1 ->
    fib(N-1) + fib(N-2).
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "fib.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Multi-clause function 'fib' should appear only once.
	count := 0
	for _, rec := range got {
		if rec.Name == "fib" && rec.Kind == "SCOPE.Operation" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entity for multi-clause function fib, got %d", count)
	}
}

func TestErlangExtractor_LanguageTag(t *testing.T) {
	src := `-module(tagged).
-export([run/0]).

run() -> ok.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "tagged.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rec := range got {
		if rec.Language != "" && rec.Language != "erlang" {
			t.Errorf("expected language=erlang, got %q on entity %q", rec.Language, rec.Name)
		}
		for _, rel := range rec.Relationships {
			if lang, ok := rel.Properties["language"]; ok && lang != "erlang" {
				t.Errorf("expected relationship language=erlang, got %q on rel %q→%q", lang, rel.FromID, rel.ToID)
			}
		}
	}
}

// TestErlangExtractor_GenServerRecall verifies ≥80% entity recall against
// the synthetic gen_server fixture defined at the top of this file.
func TestErlangExtractor_GenServerRecall(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "cache_server.erl",
		Content:  []byte(genServerFixture),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantNames := []string{
		"cache_server", // module
		"start_link",   // API function
		"get",
		"put",
		"delete",
		"flush",
		"init", // gen_server callback
		"handle_call",
		"handle_cast",
		"handle_info",
		"terminate",
		"code_change",
	}

	nameSet := make(map[string]bool, len(got))
	for _, rec := range got {
		nameSet[rec.Name] = true
	}

	found := 0
	for _, w := range wantNames {
		if nameSet[w] {
			found++
		}
	}
	recall := float64(found) / float64(len(wantNames))
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% — found %d/%d. Names in graph: %v",
			recall*100, found, len(wantNames), sortedKeys(nameSet))
	}
}

// TestErlangExtractor_HrlFile verifies that .hrl header files are also parsed.
func TestErlangExtractor_HrlFile(t *testing.T) {
	src := `-record(config, {host, port, timeout}).
-record(state, {config, connections = []}).
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "defs.hrl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	records := make(map[string]bool)
	for _, rec := range got {
		if rec.Kind == "SCOPE.Component" && rec.Subtype == "record" {
			records[rec.Name] = true
		}
	}
	for _, r := range []string{"config", "state"} {
		if !records[r] {
			t.Errorf("expected record entity %q from .hrl file", r)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
