package resolve

import "regexp"

// erlangDynamicPatterns are per-language patterns for Erlang.
// Registered via init() into dynamicPatternsByLang.
//
// Erlang dynamic-pattern catalog (issue #1037).
//
// The Erlang extractor (internal/extractors/erlang/extractor.go) emits CALLS
// edges in two forms:
//   - Qualified: "module:function" (e.g. "gen_server:start_link")
//   - Bare:      "function"        (e.g. "handle_call")
//
// These stubs are statically unresolvable because:
//  1. OTP/stdlib modules (gen_server, supervisor, application, lists, maps,
//     dict, proplists, io, erlang) are part of the Erlang/OTP distribution,
//     never indexed as in-tree entities.
//  2. OTP behaviour callbacks (init/1, handle_call/3, handle_cast/2, …) are
//     invoked by the runtime via the behaviour mechanism — no static call site
//     the resolver can see.
//
// All patterns are gated to lang=="erlang" (the safer-bias rule) so they
// cannot fire on stubs from Go, Python, Elixir, or any other language.
// Cross-language collision risks that motivate this gate:
//   - "init", "start", "stop", "get", "put", "delete" are common in many langs.
//   - "map", "filter", "lists:map", "maps:get" look like qualified forms
//     that no other archigraph language emits.
var erlangDynamicPatterns = []*regexp.Regexp{
	// ── OTP gen_server BIFs ────────────────────────────────────────────────
	// Qualified gen_server:* calls emitted as "gen_server:<fn>".
	regexp.MustCompile(`^gen_server:start_link$`),
	regexp.MustCompile(`^gen_server:start$`),
	regexp.MustCompile(`^gen_server:stop$`),
	regexp.MustCompile(`^gen_server:call$`),
	regexp.MustCompile(`^gen_server:cast$`),
	regexp.MustCompile(`^gen_server:reply$`),
	regexp.MustCompile(`^gen_server:abcast$`),
	regexp.MustCompile(`^gen_server:multi_call$`),
	regexp.MustCompile(`^gen_server:enter_loop$`),

	// ── OTP gen_statem BIFs ────────────────────────────────────────────────
	regexp.MustCompile(`^gen_statem:start_link$`),
	regexp.MustCompile(`^gen_statem:start$`),
	regexp.MustCompile(`^gen_statem:stop$`),
	regexp.MustCompile(`^gen_statem:call$`),
	regexp.MustCompile(`^gen_statem:cast$`),
	regexp.MustCompile(`^gen_statem:reply$`),

	// ── OTP gen_event BIFs ─────────────────────────────────────────────────
	regexp.MustCompile(`^gen_event:start_link$`),
	regexp.MustCompile(`^gen_event:add_handler$`),
	regexp.MustCompile(`^gen_event:notify$`),
	regexp.MustCompile(`^gen_event:sync_notify$`),
	regexp.MustCompile(`^gen_event:call$`),
	regexp.MustCompile(`^gen_event:delete_handler$`),

	// ── OTP supervisor BIFs ───────────────────────────────────────────────
	regexp.MustCompile(`^supervisor:start_link$`),
	regexp.MustCompile(`^supervisor:start_child$`),
	regexp.MustCompile(`^supervisor:terminate_child$`),
	regexp.MustCompile(`^supervisor:delete_child$`),
	regexp.MustCompile(`^supervisor:restart_child$`),
	regexp.MustCompile(`^supervisor:which_children$`),
	regexp.MustCompile(`^supervisor:count_children$`),

	// ── OTP application BIFs ──────────────────────────────────────────────
	regexp.MustCompile(`^application:start$`),
	regexp.MustCompile(`^application:stop$`),
	regexp.MustCompile(`^application:get_env$`),
	regexp.MustCompile(`^application:get_all_env$`),
	regexp.MustCompile(`^application:set_env$`),
	regexp.MustCompile(`^application:ensure_all_started$`),
	regexp.MustCompile(`^application:load$`),
	regexp.MustCompile(`^application:unload$`),

	// ── OTP behaviour callbacks (bare form) ───────────────────────────────
	// These arrive as bare leaf names because they are defined in the
	// module body without a receiver. The OTP runtime dispatches to them.
	regexp.MustCompile(`^init$`),
	regexp.MustCompile(`^handle_call$`),
	regexp.MustCompile(`^handle_cast$`),
	regexp.MustCompile(`^handle_info$`),
	regexp.MustCompile(`^handle_continue$`),
	regexp.MustCompile(`^handle_event$`),
	regexp.MustCompile(`^terminate$`),
	regexp.MustCompile(`^code_change$`),
	regexp.MustCompile(`^format_status$`),

	// ── Erlang stdlib lists:* ─────────────────────────────────────────────
	// lists:map/2, lists:filter/2, lists:foldl/3, etc.
	regexp.MustCompile(`^lists:map$`),
	regexp.MustCompile(`^lists:filter$`),
	regexp.MustCompile(`^lists:foldl$`),
	regexp.MustCompile(`^lists:foldr$`),
	regexp.MustCompile(`^lists:foreach$`),
	regexp.MustCompile(`^lists:sort$`),
	regexp.MustCompile(`^lists:keysort$`),
	regexp.MustCompile(`^lists:keyfind$`),
	regexp.MustCompile(`^lists:keydelete$`),
	regexp.MustCompile(`^lists:keyreplace$`),
	regexp.MustCompile(`^lists:member$`),
	regexp.MustCompile(`^lists:reverse$`),
	regexp.MustCompile(`^lists:flatten$`),
	regexp.MustCompile(`^lists:append$`),
	regexp.MustCompile(`^lists:concat$`),
	regexp.MustCompile(`^lists:nth$`),
	regexp.MustCompile(`^lists:last$`),
	regexp.MustCompile(`^lists:delete$`),
	regexp.MustCompile(`^lists:usort$`),
	regexp.MustCompile(`^lists:ukeysort$`),
	regexp.MustCompile(`^lists:zip$`),
	regexp.MustCompile(`^lists:unzip$`),
	regexp.MustCompile(`^lists:splitwith$`),
	regexp.MustCompile(`^lists:partition$`),
	regexp.MustCompile(`^lists:dropwhile$`),
	regexp.MustCompile(`^lists:takewhile$`),
	regexp.MustCompile(`^lists:any$`),
	regexp.MustCompile(`^lists:all$`),
	regexp.MustCompile(`^lists:sum$`),
	regexp.MustCompile(`^lists:max$`),
	regexp.MustCompile(`^lists:min$`),
	regexp.MustCompile(`^lists:len$`),
	regexp.MustCompile(`^lists:duplicate$`),
	regexp.MustCompile(`^lists:seq$`),
	regexp.MustCompile(`^lists:sublist$`),
	regexp.MustCompile(`^lists:nthtail$`),
	regexp.MustCompile(`^lists:prefix$`),
	regexp.MustCompile(`^lists:suffix$`),
	regexp.MustCompile(`^lists:subtract$`),
	regexp.MustCompile(`^lists:intersection$`),
	regexp.MustCompile(`^lists:mapfoldl$`),
	regexp.MustCompile(`^lists:mapfoldr$`),
	regexp.MustCompile(`^lists:flatlength$`),
	regexp.MustCompile(`^lists:flatmap$`),

	// ── Erlang stdlib maps:* ──────────────────────────────────────────────
	regexp.MustCompile(`^maps:new$`),
	regexp.MustCompile(`^maps:get$`),
	regexp.MustCompile(`^maps:put$`),
	regexp.MustCompile(`^maps:remove$`),
	regexp.MustCompile(`^maps:find$`),
	regexp.MustCompile(`^maps:is_key$`),
	regexp.MustCompile(`^maps:keys$`),
	regexp.MustCompile(`^maps:values$`),
	regexp.MustCompile(`^maps:to_list$`),
	regexp.MustCompile(`^maps:from_list$`),
	regexp.MustCompile(`^maps:size$`),
	regexp.MustCompile(`^maps:merge$`),
	regexp.MustCompile(`^maps:map$`),
	regexp.MustCompile(`^maps:filter$`),
	regexp.MustCompile(`^maps:fold$`),
	regexp.MustCompile(`^maps:update$`),
	regexp.MustCompile(`^maps:without$`),
	regexp.MustCompile(`^maps:with$`),
	regexp.MustCompile(`^maps:iterator$`),
	regexp.MustCompile(`^maps:next$`),

	// ── Erlang stdlib proplists:* ─────────────────────────────────────────
	regexp.MustCompile(`^proplists:get_value$`),
	regexp.MustCompile(`^proplists:get_bool$`),
	regexp.MustCompile(`^proplists:get_all_values$`),
	regexp.MustCompile(`^proplists:lookup$`),
	regexp.MustCompile(`^proplists:lookup_all$`),
	regexp.MustCompile(`^proplists:is_defined$`),
	regexp.MustCompile(`^proplists:delete$`),
	regexp.MustCompile(`^proplists:compact$`),
	regexp.MustCompile(`^proplists:to_map$`),

	// ── Erlang stdlib dict:* ──────────────────────────────────────────────
	regexp.MustCompile(`^dict:new$`),
	regexp.MustCompile(`^dict:from_list$`),
	regexp.MustCompile(`^dict:to_list$`),
	regexp.MustCompile(`^dict:store$`),
	regexp.MustCompile(`^dict:find$`),
	regexp.MustCompile(`^dict:fetch$`),
	regexp.MustCompile(`^dict:erase$`),
	regexp.MustCompile(`^dict:is_key$`),
	regexp.MustCompile(`^dict:size$`),
	regexp.MustCompile(`^dict:merge$`),
	regexp.MustCompile(`^dict:map$`),
	regexp.MustCompile(`^dict:filter$`),
	regexp.MustCompile(`^dict:fold$`),
	regexp.MustCompile(`^dict:update$`),
	regexp.MustCompile(`^dict:append$`),
	regexp.MustCompile(`^dict:append_list$`),
	regexp.MustCompile(`^dict:fetch_keys$`),

	// ── Erlang stdlib io:* ────────────────────────────────────────────────
	regexp.MustCompile(`^io:format$`),
	regexp.MustCompile(`^io:write$`),
	regexp.MustCompile(`^io:read$`),
	regexp.MustCompile(`^io:fread$`),
	regexp.MustCompile(`^io:fwrite$`),
	regexp.MustCompile(`^io:get_line$`),
	regexp.MustCompile(`^io:put_chars$`),
	regexp.MustCompile(`^io:nl$`),
	regexp.MustCompile(`^io:printable_range$`),

	// ── Erlang stdlib string:* ────────────────────────────────────────────
	regexp.MustCompile(`^string:split$`),
	regexp.MustCompile(`^string:join$`),
	regexp.MustCompile(`^string:trim$`),
	regexp.MustCompile(`^string:to_lower$`),
	regexp.MustCompile(`^string:to_upper$`),
	regexp.MustCompile(`^string:length$`),
	regexp.MustCompile(`^string:substr$`),
	regexp.MustCompile(`^string:str$`),
	regexp.MustCompile(`^string:rstr$`),
	regexp.MustCompile(`^string:prefix$`),
	regexp.MustCompile(`^string:suffix$`),
	regexp.MustCompile(`^string:find$`),
	regexp.MustCompile(`^string:replace$`),
	regexp.MustCompile(`^string:lexemes$`),
	regexp.MustCompile(`^string:tokens$`),
	regexp.MustCompile(`^string:to_integer$`),
	regexp.MustCompile(`^string:to_float$`),

	// ── Erlang stdlib erlang:* (BIFs) ────────────────────────────────────
	regexp.MustCompile(`^erlang:self$`),
	regexp.MustCompile(`^erlang:spawn$`),
	regexp.MustCompile(`^erlang:spawn_link$`),
	regexp.MustCompile(`^erlang:spawn_monitor$`),
	regexp.MustCompile(`^erlang:send$`),
	regexp.MustCompile(`^erlang:exit$`),
	regexp.MustCompile(`^erlang:throw$`),
	regexp.MustCompile(`^erlang:error$`),
	regexp.MustCompile(`^erlang:make_ref$`),
	regexp.MustCompile(`^erlang:is_process_alive$`),
	regexp.MustCompile(`^erlang:process_info$`),
	regexp.MustCompile(`^erlang:whereis$`),
	regexp.MustCompile(`^erlang:register$`),
	regexp.MustCompile(`^erlang:unregister$`),
	regexp.MustCompile(`^erlang:monitor$`),
	regexp.MustCompile(`^erlang:demonitor$`),
	regexp.MustCompile(`^erlang:system_time$`),
	regexp.MustCompile(`^erlang:monotonic_time$`),
	regexp.MustCompile(`^erlang:now$`),
	regexp.MustCompile(`^erlang:atom_to_list$`),
	regexp.MustCompile(`^erlang:list_to_atom$`),
	regexp.MustCompile(`^erlang:integer_to_list$`),
	regexp.MustCompile(`^erlang:list_to_integer$`),
	regexp.MustCompile(`^erlang:float_to_list$`),
	regexp.MustCompile(`^erlang:term_to_binary$`),
	regexp.MustCompile(`^erlang:binary_to_term$`),

	// ── Bare stdlib BIFs (non-qualified) ─────────────────────────────────
	// These BIFs are auto-imported into every Erlang module — they arrive
	// as bare CALLS targets. All are safe to mark dynamic because they
	// share no name with user-defined archigraph entities in other langs
	// after the erlang language gate is applied.
	regexp.MustCompile(`^self$`),
	regexp.MustCompile(`^spawn$`),
	regexp.MustCompile(`^spawn_link$`),
	regexp.MustCompile(`^spawn_monitor$`),
	regexp.MustCompile(`^make_ref$`),
	regexp.MustCompile(`^whereis$`),
	regexp.MustCompile(`^register$`),
	regexp.MustCompile(`^unregister$`),
	regexp.MustCompile(`^monitor$`),
	regexp.MustCompile(`^demonitor$`),
	regexp.MustCompile(`^send$`),
	regexp.MustCompile(`^halt$`),
	regexp.MustCompile(`^abs$`),
	regexp.MustCompile(`^hd$`),
	regexp.MustCompile(`^tl$`),
	regexp.MustCompile(`^length$`),
	regexp.MustCompile(`^node$`),
	regexp.MustCompile(`^nodes$`),
	regexp.MustCompile(`^atom_to_list$`),
	regexp.MustCompile(`^list_to_atom$`),
	regexp.MustCompile(`^integer_to_list$`),
	regexp.MustCompile(`^list_to_integer$`),
	regexp.MustCompile(`^float_to_list$`),
	regexp.MustCompile(`^list_to_float$`),
	regexp.MustCompile(`^binary_to_list$`),
	regexp.MustCompile(`^list_to_binary$`),
	regexp.MustCompile(`^atom_to_binary$`),
	regexp.MustCompile(`^binary_to_atom$`),
	regexp.MustCompile(`^term_to_binary$`),
	regexp.MustCompile(`^binary_to_term$`),
	regexp.MustCompile(`^size$`),
	regexp.MustCompile(`^tuple_size$`),
	regexp.MustCompile(`^byte_size$`),
	regexp.MustCompile(`^bit_size$`),
	regexp.MustCompile(`^map_size$`),
	regexp.MustCompile(`^element$`),
	regexp.MustCompile(`^setelement$`),
	regexp.MustCompile(`^tuple_to_list$`),
	regexp.MustCompile(`^list_to_tuple$`),
	regexp.MustCompile(`^throw$`),
	regexp.MustCompile(`^exit$`),
	regexp.MustCompile(`^erlang_error$`), // disambiguated form
	regexp.MustCompile(`^apply$`),
	regexp.MustCompile(`^max$`),
	regexp.MustCompile(`^min$`),
	regexp.MustCompile(`^round$`),
	regexp.MustCompile(`^trunc$`),
	regexp.MustCompile(`^floor$`),
	regexp.MustCompile(`^ceil$`),
	regexp.MustCompile(`^float$`),

	// ── Module:function/arity pattern ────────────────────────────────────
	// Match any "module:function" qualified form emitted by the extractor,
	// catching long-tail OTP/stdlib calls not enumerated above.
	// The pattern ^[a-z_]+:[a-z_]+ is conservative: only lower-case atoms,
	// which are valid Erlang module and function names.
	regexp.MustCompile(`^[a-z_][a-z0-9_]*:[a-z_][a-z0-9_!?]*$`),
}

func init() {
	dynamicPatternsByLang["erlang"] = erlangDynamicPatterns
}
