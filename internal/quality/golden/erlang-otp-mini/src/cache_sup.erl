%% cache_sup.erl — OTP supervisor for the cache application.
%%
%% Supervises the cache_server gen_server child.
-module(cache_sup).

-behaviour(supervisor).

%% API
-export([start_link/0]).

%% supervisor callbacks
-export([init/1]).

-define(SERVER, ?MODULE).

%%====================================================================
%% API
%%====================================================================

start_link() ->
    supervisor:start_link({local, ?SERVER}, ?MODULE, []).

%%====================================================================
%% supervisor callbacks
%%====================================================================

init([]) ->
    SupFlags = #{strategy => one_for_one,
                 intensity => 5,
                 period => 10},
    ChildSpecs = [
        #{id => cache_server,
          start => {cache_server, start_link, []},
          restart => permanent,
          shutdown => 5000,
          type => worker,
          modules => [cache_server]}
    ],
    {ok, {SupFlags, ChildSpecs}}.
