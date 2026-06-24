package erlang_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/erlang"
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

// TestErlangExtractor_ArityIdentity verifies that name/arity is the function
// identity: foo/1 and foo/2 are distinct entities, each carrying its arity in
// Signature ("foo/1") and Properties["arity"]. Per-arity export precision is
// also checked (lookup/1 exported, lookup/2 private).
func TestErlangExtractor_ArityIdentity(t *testing.T) {
	src := `-module(store).
-export([lookup/1]).

lookup(Key) ->
    lookup(Key, default).

lookup(Key, Default) ->
    get(Key, Default).

start() -> ok.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "store.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	type opKey struct{ name, arity string }
	ops := make(map[opKey]struct {
		subtype string
		sig     string
	})
	lookupCount := 0
	for _, rec := range got {
		if rec.Kind != "SCOPE.Operation" {
			continue
		}
		if rec.Name == "lookup" {
			lookupCount++
		}
		ops[opKey{rec.Name, rec.Properties["arity"]}] = struct {
			subtype string
			sig     string
		}{rec.Subtype, rec.Signature}
	}

	// Two distinct lookup entities — lookup/1 and lookup/2 — not one.
	if lookupCount != 2 {
		t.Fatalf("expected 2 distinct lookup entities (lookup/1, lookup/2), got %d", lookupCount)
	}

	l1, ok := ops[opKey{"lookup", "1"}]
	if !ok {
		t.Fatalf("missing lookup/1 entity")
	}
	if l1.sig != "lookup/1" {
		t.Errorf("lookup/1 signature = %q, want lookup/1", l1.sig)
	}
	if l1.subtype != "exported_function" {
		t.Errorf("lookup/1 should be exported_function (it is in -export), got %q", l1.subtype)
	}

	l2, ok := ops[opKey{"lookup", "2"}]
	if !ok {
		t.Fatalf("missing lookup/2 entity")
	}
	if l2.sig != "lookup/2" {
		t.Errorf("lookup/2 signature = %q, want lookup/2", l2.sig)
	}
	if l2.subtype != "function" {
		t.Errorf("lookup/2 is NOT exported (only lookup/1 is), should be function, got %q", l2.subtype)
	}

	// start/0 — arity 0 from empty parens.
	s0, ok := ops[opKey{"start", "0"}]
	if !ok {
		t.Fatalf("missing start/0 entity")
	}
	if s0.sig != "start/0" {
		t.Errorf("start/0 signature = %q, want start/0", s0.sig)
	}
}

// TestErlangExtractor_ArityNestedArgs verifies countArity respects nested
// tuples/lists/maps/binaries and strings so commas inside them don't inflate
// the arity.
func TestErlangExtractor_ArityNestedArgs(t *testing.T) {
	src := `-module(arity_nested).
-export([handle/3]).

handle({get, Key}, [A, B], <<X:8, Y:8>>) ->
    ok.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "arity_nested.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rec := range got {
		if rec.Kind == "SCOPE.Operation" && rec.Name == "handle" {
			if rec.Properties["arity"] != "3" {
				t.Errorf("handle arity = %q, want 3 (nested commas must not split args)", rec.Properties["arity"])
			}
			if rec.Signature != "handle/3" {
				t.Errorf("handle signature = %q, want handle/3", rec.Signature)
			}
			return
		}
	}
	t.Fatalf("handle/3 entity not found")
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

// TestErlangExtractor_OTPBehaviour verifies that -behaviour(gen_server). is
// detected: the module entity is refined to gen_server_module, stamped with
// Properties["otp_behaviour"]="gen_server" and tagged "otp"/"otp:gen_server".
func TestErlangExtractor_OTPBehaviour(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "cache_server.erl",
		Content:  []byte(genServerFixture),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var mod bool
	for _, rec := range got {
		if rec.Name == "cache_server" && rec.Kind == "SCOPE.Component" {
			mod = true
			if rec.Subtype != "gen_server_module" {
				t.Errorf("expected module subtype gen_server_module, got %q", rec.Subtype)
			}
			if rec.Properties["otp_behaviour"] != "gen_server" {
				t.Errorf("expected otp_behaviour=gen_server, got %q", rec.Properties["otp_behaviour"])
			}
			if !hasTag(rec.Tags, "otp") || !hasTag(rec.Tags, "otp:gen_server") {
				t.Errorf("expected otp tags, got %v", rec.Tags)
			}
		}
	}
	if !mod {
		t.Fatal("module entity cache_server not found")
	}
}

// TestErlangExtractor_OTPCallbacks verifies gen_server callbacks are tagged.
func TestErlangExtractor_OTPCallbacks(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "cache_server.erl",
		Content:  []byte(genServerFixture),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCallbacks := map[string]bool{
		"init": false, "handle_call": false, "handle_cast": false,
		"handle_info": false, "terminate": false, "code_change": false,
	}
	for _, rec := range got {
		if rec.Kind != "SCOPE.Operation" {
			continue
		}
		if _, want := wantCallbacks[rec.Name]; want {
			if rec.Subtype != "otp_callback" {
				t.Errorf("%s: expected subtype otp_callback, got %q", rec.Name, rec.Subtype)
			}
			if rec.Properties["otp_callback_of"] != "gen_server" {
				t.Errorf("%s: expected otp_callback_of=gen_server, got %q", rec.Name, rec.Properties["otp_callback_of"])
			}
			if !hasTag(rec.Tags, "otp_callback") {
				t.Errorf("%s: expected otp_callback tag, got %v", rec.Name, rec.Tags)
			}
			wantCallbacks[rec.Name] = true
		}
	}
	for cb, seen := range wantCallbacks {
		if !seen {
			t.Errorf("callback %q not found / not tagged", cb)
		}
	}
}

// TestErlangExtractor_SupervisorBehaviour verifies the supervisor role + that
// a non-OTP module keeps the plain "module" subtype.
func TestErlangExtractor_SupervisorBehaviour(t *testing.T) {
	src := `-module(my_sup).
-behavior(supervisor).
-export([start_link/0, init/1]).

start_link() ->
    supervisor:start_link({local, ?MODULE}, ?MODULE, []).

init([]) ->
    {ok, {{one_for_one, 5, 10}, []}}.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "my_sup.erl",
		Content:  []byte(src),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rec := range got {
		if rec.Name == "my_sup" && rec.Kind == "SCOPE.Component" {
			if rec.Subtype != "supervisor_module" {
				t.Errorf("expected supervisor_module (American spelling -behavior), got %q", rec.Subtype)
			}
		}
		if rec.Name == "init" && rec.Properties["otp_callback_of"] != "supervisor" {
			t.Errorf("init should be a supervisor callback, got %q", rec.Properties["otp_callback_of"])
		}
	}
}

// supTreeFixture is a supervisor whose init/1 returns a child spec list using
// both the modern map form and the legacy tuple form, so the SUPERVISES edge
// extraction is exercised against both shapes.
const supTreeFixture = `-module(top_sup).
-behaviour(supervisor).
-export([start_link/0, init/1]).

start_link() ->
    supervisor:start_link({local, ?MODULE}, ?MODULE, []).

init([]) ->
    SupFlags = #{strategy => one_for_one, intensity => 5, period => 10},
    Worker = #{id => cache_server,
               start => {cache_server, start_link, []},
               restart => permanent,
               type => worker},
    Logger = #{id => logger_srv,
               start => {logger_srv, start_link, []}},
    Legacy = {db_pool, {db_pool, start_link, []}, permanent, 5000, worker, [db_pool]},
    {ok, {SupFlags, [Worker, Logger, Legacy]}}.
`

// TestErlangExtractor_SupervisionTreeEdges verifies SUPERVISES edges are
// emitted from the supervisor module to every child module declared in its
// init/1 child spec list, across both map and tuple spec forms.
func TestErlangExtractor_SupervisionTreeEdges(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "top_sup.erl",
		Content:  []byte(supTreeFixture),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sup bool
	supervised := map[string]string{} // module -> child_id
	for _, rec := range got {
		if rec.Name == "top_sup" && rec.Kind == "SCOPE.Component" {
			sup = true
			for _, rel := range rec.Relationships {
				if rel.Kind == "SUPERVISES" {
					supervised[rel.ToID] = rel.Properties["child_id"]
					if rel.Properties["provenance"] != "otp_child_spec" {
						t.Errorf("SUPERVISES %s: expected provenance otp_child_spec, got %q",
							rel.ToID, rel.Properties["provenance"])
					}
				}
			}
		}
	}
	if !sup {
		t.Fatal("supervisor module top_sup not found")
	}
	want := map[string]string{
		"cache_server": "cache_server",
		"logger_srv":   "logger_srv",
		"db_pool":      "db_pool",
	}
	for mod, id := range want {
		gotID, ok := supervised[mod]
		if !ok {
			t.Errorf("expected SUPERVISES edge to child module %q, missing", mod)
			continue
		}
		if gotID != id {
			t.Errorf("child %q: expected child_id %q, got %q", mod, id, gotID)
		}
	}
	if len(supervised) != len(want) {
		t.Errorf("expected exactly %d SUPERVISES edges, got %d: %v",
			len(want), len(supervised), supervised)
	}
}

// TestErlangExtractor_MessageTagDispatch verifies per-message-tag dispatch is
// recovered on the gen_server handle_call/handle_cast callbacks.
func TestErlangExtractor_MessageTagDispatch(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "cache_server.erl",
		Content:  []byte(genServerFixture),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTags := map[string]string{
		"handle_call": "get",              // {get, Key} → get; catch-all skipped
		"handle_cast": "put,delete,flush", // {put,..},{delete,..},flush in clause order
	}
	seen := map[string]bool{}
	for _, rec := range got {
		if rec.Kind != "SCOPE.Operation" {
			continue
		}
		want, ok := wantTags[rec.Name]
		if !ok {
			continue
		}
		seen[rec.Name] = true
		if rec.Properties["otp_dispatch_tags"] != want {
			t.Errorf("%s: expected otp_dispatch_tags=%q, got %q",
				rec.Name, want, rec.Properties["otp_dispatch_tags"])
		}
		// Each tag should also surface as an otp_msg:<tag> tag.
		for _, tg := range splitComma(want) {
			if !hasTag(rec.Tags, "otp_msg:"+tg) {
				t.Errorf("%s: expected tag otp_msg:%s, got %v", rec.Name, tg, rec.Tags)
			}
		}
	}
	for cb := range wantTags {
		if !seen[cb] {
			t.Errorf("dispatch callback %q not found", cb)
		}
	}
}

// typeSystemFixture exercises -spec/-type/-opaque/-callback + -import + -define.
const typeSystemFixture = `-module(typed).
-behaviour(my_behaviour).

-import(lists, [reverse/1, map/2]).

-export([encode/1, encode/2, decode/1]).

-define(VERSION, 3).
-define(TAG, encode).

-type result(T) :: {ok, T} | {error, term()}.
-opaque handle() :: reference().

-callback init(Args :: list()) -> {ok, state()}.
-callback handle(Req :: term(), state()) -> {reply, term(), state()}.

-spec encode(binary()) -> result(binary()).
encode(Data) ->
    reverse(Data).

-spec encode(binary(), Opts :: list()) -> result(binary()).
encode(Data, _Opts) ->
    map(fun(X) -> X end, Data).

-spec decode(binary()) -> {ok, term()}.
decode(Bin) ->
    Bin.
`

func TestErlangExtractor_TypeDefinitions(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "typed.erl", Content: []byte(typeSystemFixture), Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	subtypeByName := map[string]string{}
	for _, rec := range got {
		if rec.Kind == "SCOPE.Component" && (rec.Subtype == "type" || rec.Subtype == "opaque_type") {
			subtypeByName[rec.Name] = rec.Subtype
		}
	}
	if subtypeByName["result"] != "type" {
		t.Errorf("expected result → type, got %q", subtypeByName["result"])
	}
	if subtypeByName["handle"] != "opaque_type" {
		t.Errorf("expected handle → opaque_type, got %q", subtypeByName["handle"])
	}
}

func TestErlangExtractor_CallbackContracts(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "typed.erl", Content: []byte(typeSystemFixture), Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cbs := map[string]string{} // name/arity → present
	for _, rec := range got {
		if rec.Kind == "SCOPE.Operation" && rec.Subtype == "callback_spec" {
			cbs[rec.Signature] = rec.Properties["callback_spec"]
			if !hasTag(rec.Tags, "otp_callback_contract") {
				t.Errorf("callback %s missing otp_callback_contract tag", rec.Signature)
			}
		}
	}
	if _, ok := cbs["init/1"]; !ok {
		t.Errorf("expected callback init/1, got %v", cbs)
	}
	if _, ok := cbs["handle/2"]; !ok {
		t.Errorf("expected callback handle/2, got %v", cbs)
	}
}

func TestErlangExtractor_SpecAttachment(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "typed.erl", Content: []byte(typeSystemFixture), Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// -spec binds arity-precisely: encode/1 and encode/2 carry distinct specs.
	specBySig := map[string]string{}
	for _, rec := range got {
		if rec.Kind == "SCOPE.Operation" && rec.Subtype != "callback_spec" {
			if s := rec.Properties["spec"]; s != "" {
				specBySig[rec.Signature] = s
			}
		}
	}
	if !strings.Contains(specBySig["encode/1"], "encode(binary()) -> result(binary())") {
		t.Errorf("encode/1 spec wrong: %q", specBySig["encode/1"])
	}
	if !strings.Contains(specBySig["encode/2"], "encode(binary(), Opts") {
		t.Errorf("encode/2 spec wrong: %q", specBySig["encode/2"])
	}
	if !strings.Contains(specBySig["decode/1"], "decode(binary()) -> {ok, term()}") {
		t.Errorf("decode/1 spec wrong: %q", specBySig["decode/1"])
	}
}

func TestErlangExtractor_ImportResolution(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "typed.erl", Content: []byte(typeSystemFixture), Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// IMPORTS edges for the function imports.
	var importTargets []string
	for _, rec := range got {
		for _, rel := range rec.Relationships {
			if rel.Kind == "IMPORTS" && rel.Properties["import_kind"] == "function" {
				importTargets = append(importTargets, rel.ToID)
			}
		}
	}
	wantImp := map[string]bool{"lists:reverse": false, "lists:map": false}
	for _, it := range importTargets {
		if _, ok := wantImp[it]; ok {
			wantImp[it] = true
		}
	}
	for k, seen := range wantImp {
		if !seen {
			t.Errorf("missing function IMPORTS edge %q (got %v)", k, importTargets)
		}
	}
	// Bare calls to imported funcs resolve to "lists:reverse"/"lists:map".
	var encodeCalls []string
	for _, rec := range got {
		if rec.Name == "encode" && rec.Properties["arity"] == "1" {
			for _, rel := range rec.Relationships {
				if rel.Kind == "CALLS" {
					encodeCalls = append(encodeCalls, rel.ToID)
				}
			}
		}
	}
	found := false
	for _, c := range encodeCalls {
		if c == "lists:reverse" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected encode/1 bare call reverse(...) resolved to lists:reverse, got %v", encodeCalls)
	}
}

func TestErlangExtractor_MacroExpansion(t *testing.T) {
	src := `-module(srv).
-export([go/0]).
-define(SERVER, ?MODULE).

go() ->
    gen_server:call(?SERVER, ping).
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "srv.erl", Content: []byte(src), Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ?SERVER → ?MODULE → srv; the qualified call gen_server:call is still
	// recovered (macro expansion must not break the call scan).
	var found bool
	for _, rec := range got {
		if rec.Name == "go" {
			for _, rel := range rec.Relationships {
				if rel.Kind == "CALLS" && rel.ToID == "gen_server:call" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected gen_server:call CALLS edge after macro expansion")
	}
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// #5363 — OTP/BEAM deepening: client message edges, restart strategy, tables.
// ---------------------------------------------------------------------------

// TestErlangExtractor_ClientMessageEdges verifies that gen_server client send
// primitives (gen_server:call/cast) enrich the CALLS edge with the message
// kind/server/tag, recovering the CLIENT side of the gen_server protocol.
func TestErlangExtractor_ClientMessageEdges(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "cache_server.erl",
		Content:  []byte(genServerFixture),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// get/1 → gen_server:call(?SERVER, {get, Key}) → kind=call, tag=get.
	// put/2 → gen_server:cast(?SERVER, {put, Key, Value}) → kind=cast, tag=put.
	// flush/0 → gen_server:cast(?SERVER, flush) → kind=cast, tag=flush.
	type want struct{ kind, tag, server string }
	wants := map[string]want{
		"get":   {"call", "get", "cache_server"}, // ?SERVER → ?MODULE → cache_server
		"put":   {"cast", "put", "cache_server"},
		"flush": {"cast", "flush", "cache_server"},
	}
	found := map[string]bool{}
	for _, rec := range got {
		w, ok := wants[rec.Name]
		if !ok || rec.Kind != "SCOPE.Operation" {
			continue
		}
		var matched bool
		for _, rel := range rec.Relationships {
			if rel.Kind != "CALLS" || rel.Properties["otp_msg_kind"] == "" {
				continue
			}
			matched = true
			if rel.Properties["otp_msg_kind"] != w.kind {
				t.Errorf("%s: otp_msg_kind=%q, want %q", rec.Name, rel.Properties["otp_msg_kind"], w.kind)
			}
			if rel.Properties["otp_msg_tag"] != w.tag {
				t.Errorf("%s: otp_msg_tag=%q, want %q", rec.Name, rel.Properties["otp_msg_tag"], w.tag)
			}
			if rel.Properties["otp_msg_server"] != w.server {
				t.Errorf("%s: otp_msg_server=%q, want %q", rec.Name, rel.Properties["otp_msg_server"], w.server)
			}
			if rel.Properties["provenance"] != "otp_client_send" {
				t.Errorf("%s: provenance=%q, want otp_client_send", rec.Name, rel.Properties["provenance"])
			}
		}
		if !matched {
			t.Errorf("%s: no enriched OTP-message CALLS edge found", rec.Name)
		}
		// The entity should carry an otp_send:<kind> tag.
		if !hasTag(rec.Tags, "otp_send:"+w.kind) {
			t.Errorf("%s: missing tag otp_send:%s (tags=%v)", rec.Name, w.kind, rec.Tags)
		}
		found[rec.Name] = true
	}
	for name := range wants {
		if !found[name] {
			t.Errorf("client-send function %q not found", name)
		}
	}
}

// TestErlangExtractor_SupervisorRestartStrategy verifies the supervisor's
// SupFlags restart strategy is recovered and stamped on the module entity.
func TestErlangExtractor_SupervisorRestartStrategy(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "top_sup.erl",
		Content:  []byte(supTreeFixture),
		Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, rec := range got {
		if rec.Name != "top_sup" || rec.Kind != "SCOPE.Component" {
			continue
		}
		found = true
		if rec.Properties["otp_sup_strategy"] != "one_for_one" {
			t.Errorf("otp_sup_strategy=%q, want one_for_one", rec.Properties["otp_sup_strategy"])
		}
		if rec.Properties["otp_sup_intensity"] != "5" {
			t.Errorf("otp_sup_intensity=%q, want 5", rec.Properties["otp_sup_intensity"])
		}
		if rec.Properties["otp_sup_period"] != "10" {
			t.Errorf("otp_sup_period=%q, want 10", rec.Properties["otp_sup_period"])
		}
		if !hasTag(rec.Tags, "otp_sup_strategy:one_for_one") {
			t.Errorf("missing tag otp_sup_strategy:one_for_one (tags=%v)", rec.Tags)
		}
	}
	if !found {
		t.Fatal("supervisor module top_sup not found")
	}
}

// TestErlangExtractor_SupervisorLegacyStrategy verifies the legacy tuple-form
// SupFlags {one_for_all, 3, 60} is recovered.
func TestErlangExtractor_SupervisorLegacyStrategy(t *testing.T) {
	const fixture = `-module(legacy_sup).
-behaviour(supervisor).
-export([start_link/0, init/1]).

start_link() -> supervisor:start_link({local, ?MODULE}, ?MODULE, []).

init([]) ->
    Child = {worker, {worker, start_link, []}, permanent, 5000, worker, [worker]},
    {ok, {{one_for_all, 3, 60}, [Child]}}.
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "legacy_sup.erl", Content: []byte(fixture), Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, rec := range got {
		if rec.Name == "legacy_sup" && rec.Kind == "SCOPE.Component" {
			found = true
			if rec.Properties["otp_sup_strategy"] != "one_for_all" {
				t.Errorf("otp_sup_strategy=%q, want one_for_all", rec.Properties["otp_sup_strategy"])
			}
			if rec.Properties["otp_sup_intensity"] != "3" || rec.Properties["otp_sup_period"] != "60" {
				t.Errorf("intensity/period = %q/%q, want 3/60",
					rec.Properties["otp_sup_intensity"], rec.Properties["otp_sup_period"])
			}
		}
	}
	if !found {
		t.Fatal("supervisor module legacy_sup not found")
	}
}

// TestErlangExtractor_MnesiaEtsTables verifies mnesia:create_table / ets:new
// declarations become SCOPE.Datastore entities and table traffic emits
// ACCESSES_TABLE edges to them.
func TestErlangExtractor_MnesiaEtsTables(t *testing.T) {
	const fixture = `-module(store).
-export([setup/0, put/2, get/1, cache_put/2, cache_get/1]).

setup() ->
    mnesia:create_table(person, [{attributes, [id, name]}]),
    ets:new(session_cache, [named_table, set, public]).

put(Id, Name) ->
    mnesia:write({person, Id, Name}).

get(Id) ->
    mnesia:read(person, Id).

cache_put(K, V) ->
    ets:insert(session_cache, {K, V}).

cache_get(K) ->
    ets:lookup(session_cache, K).
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "store.erl", Content: []byte(fixture), Language: "erlang",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two datastore entities: person (mnesia), session_cache (ets).
	tables := map[string]string{} // name -> engine
	for _, rec := range got {
		if rec.Kind == "SCOPE.Datastore" {
			tables[rec.Name] = rec.Properties["engine"]
			if rec.Properties["table_ref"] == "" {
				t.Errorf("datastore %q missing table_ref", rec.Name)
			}
		}
	}
	if tables["person"] != "mnesia" {
		t.Errorf("person engine=%q, want mnesia", tables["person"])
	}
	if tables["session_cache"] != "ets" {
		t.Errorf("session_cache engine=%q, want ets", tables["session_cache"])
	}

	// ACCESSES_TABLE edges: put→write(person), get→read(person),
	// cache_put→write(session_cache), cache_get→read(session_cache).
	type acc struct{ table, op string }
	accesses := map[string][]acc{} // fn -> accesses
	for _, rec := range got {
		if rec.Kind != "SCOPE.Operation" {
			continue
		}
		for _, rel := range rec.Relationships {
			if rel.Kind == "ACCESSES_TABLE" {
				accesses[rec.Name] = append(accesses[rec.Name],
					acc{rel.Properties["table"], rel.Properties["op"]})
			}
		}
	}
	wantAcc := map[string]acc{
		"put":       {"person", "write"},
		"get":       {"person", "read"},
		"cache_put": {"session_cache", "write"},
		"cache_get": {"session_cache", "read"},
	}
	for fn, w := range wantAcc {
		var ok bool
		for _, a := range accesses[fn] {
			if a == w {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s: expected ACCESSES_TABLE %+v, got %v", fn, w, accesses[fn])
		}
	}
}
