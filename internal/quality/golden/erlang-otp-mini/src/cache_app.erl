%% cache_app.erl — OTP application callback module.
%%
%% Starts and stops the cache application.
-module(cache_app).

-behaviour(application).

%% application callbacks
-export([start/2, stop/1]).

%%====================================================================
%% application callbacks
%%====================================================================

start(_StartType, _StartArgs) ->
    cache_sup:start_link().

stop(_State) ->
    ok.
