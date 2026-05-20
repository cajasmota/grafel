%% cache_server.erl — OTP gen_server-based in-memory cache.
%%
%% This module implements a simple key-value cache using a gen_server.
%% It demonstrates the canonical OTP gen_server pattern.
-module(cache_server).

-behaviour(gen_server).

-include("cache.hrl").

%% Public API
-export([start_link/0, get/1, put/2, delete/1, flush/0]).

%% gen_server callbacks
-export([init/1, handle_call/3, handle_cast/2, handle_info/2,
         terminate/2, code_change/3]).

-define(SERVER, ?MODULE).

%%====================================================================
%% Public API
%%====================================================================

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

%%====================================================================
%% gen_server callbacks
%%====================================================================

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
