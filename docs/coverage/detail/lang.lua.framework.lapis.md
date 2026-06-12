<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.lua.framework.lapis` — Lapis

Auto-generated. Back to [summary](../summary.md).

- **Language:** [lua](../by-language/lua.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | — | — | `internal/engine/lua_routes.go` | Lapis routes synthesized to canonical http_endpoint via synthesizeLapis (app:get/post/put/patch/delete/options/head verb routes, named + unnamed app:match, respond_to verb tables) with :id->{id} normalization (httproutes.FrameworkLapis) and app/route-name handler attribution; value-asserting tests in lua_routes_test.go. Custom extractor also stamps canonical_path. |
| Handler attribution | ✅ `full` | — | — | `internal/custom/lua/routing.go`<br>`internal/engine/lua_routes.go` | Lapis routes synthesized to canonical http_endpoint via synthesizeLapis (app:get/post/put/patch/delete/options/head verb routes, named + unnamed app:match, respond_to verb tables) with :id->{id} normalization (httproutes.FrameworkLapis) and app/route-name handler attribution; value-asserting tests in lua_routes_test.go. Custom extractor also stamps canonical_path. |
| Route extraction | ✅ `full` | — | — | `internal/custom/lua/routing.go`<br>`internal/engine/lua_routes.go` | Lapis routes synthesized to canonical http_endpoint via synthesizeLapis (app:get/post/put/patch/delete/options/head verb routes, named + unnamed app:match, respond_to verb tables) with :id->{id} normalization (httproutes.FrameworkLapis) and app/route-name handler attribution; value-asserting tests in lua_routes_test.go. Custom extractor also stamps canonical_path. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/lua/auth.go` | Lapis auth coverage: session.current_user/lapis.session (auth_method=session), before_filter + @require_login guards (auth_method=session), Users:find lookups, bcrypt/crypto password hashing. Each guard stamps auth_method; value-asserting tests TestLuaAuthRequireLogin in extractors_test.go. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/validation.go` | Captures Lapis params.<field> access and assert_valid field tuples (field name per tuple) plus cjson.decode DTO ingestion. Partial: no cross-file dataflow or type binding — the DTO's full shape/types are not resolved, only per-call-site field names. |
| Request validation | ✅ `full` | — | — | `internal/custom/lua/validation.go` | Lapis assert_valid/validate.validate(self.params, { {"field", exists=true, min_length=3, matches_pattern=...} }) is walked per-tuple to emit one request_validation entity per (field, rule) pair capturing the SPECIFIC field name and validation rule; lapis.csrf / csrf.validate_token / @csrf emits a csrf_token entity. Also covers lapis.validate import + check_params/capture_errors signals. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/lua/middleware.go` | Lapis middleware chain: before_filter/app:before (phase=before) + app:after filters with chain_index ordering, error_handler/on_error, lapis.flow pipeline. value-asserting tests in extractors_test.go. |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static enum_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |
| Interface extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static interface_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |
| Type alias extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static type_alias_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |
| Type extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static type_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/lua/testing.go`<br>`internal/custom/lua/tests_route_e2e.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/lua_routes.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks_lua.go`<br>`internal/extractors/cross/testmap/resolver.go` | busted (describe/it) + luaunit (TestClass:testXxx) registered in the shared testmap extractor: each test case emits a TESTS edge to the production symbol via direct-call resolution (high), describe-subject / Test<Subject> class fallback (medium), and *_spec.lua/*_test.lua naming convention (low). Lua block-body extractor balances function/if/do...end (quote+long-bracket aware); busted/luaunit assertion DSL stop-worded. custom/lua/testing.go still surfaces standalone test-pattern nodes. ROUTE-HIT e2e linkage (#4749, Lua slice of tail epic; mirrors Crystal/Kemal #4760 + Swift/Vapor #4755): custom/lua/tests_route_e2e.go emits one test_suite per busted/lapis *_spec.lua carrying e2e_route_calls from lapis.spec request helpers (request(app, "/path", { method = "POST" }) / path-only request("/path") / mock_request, GET-default), and the shared language-agnostic engine.linkE2ERouteTestsToEndpoints pass emits the TESTS edge to the Lapis http_endpoint_definition synthesized by synthesizeLapis (#3484, internal/engine/lua_routes.go) — proven by TestIssue4749_LuaSpecLapisE2ERouteTestsLinkToEndpoints + TestLuaRouteE2E_*. busted uses anonymous describe/it function closures (JS/Ruby-like), so the test_suite is the scope-owner carrying the route hits (Lua analog of Ruby #4684 / JS #4680). Local-variable/receiver typing (#4749 part a) is N/A: Lapis handlers are anonymous functions / table methods and the lua base extractor has no class-qualified receiver resolver to consume a receiver_type stamp, so route-string linkage is the coverage mechanism (mirrors functional Elixir #4688 / Crystal #4760). Honest exclusions: fully-dynamic/concatenated routes (no static literal) and non-request unit specs are documented follow-ups. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/observability.go` | ngx.log(ngx.LEVEL, ...) captures the log LEVEL (prop level); print/io.write captured as Lapis log call-sites. PARTIAL by design: message text and logger->sink binding need cross-file dataflow not resolved here. |
| Metric extraction | ✅ `full` | — | — | `internal/custom/lua/observability.go` | prometheus:counter/histogram/gauge("name") capture the metric name from the string literal in-call (prop metric_name); value-asserting tests prove requests_total/request_duration_ms. No cross-file resolution needed. Non-literal names flagged metric_name=<unresolved>. |
| Trace extraction | ✅ `full` | — | — | `internal/custom/lua/observability.go` | tracer:start_span("name") captures the span name from the string literal in-call (prop span_name); value-asserting test proves the literal. No cross-file resolution needed. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/types/confidence.go` | Confidence scores are stamped on Lua entities via the language-agnostic effect propagation pass. Partial: no Lua-specific effect sinks file; confidence derived from CALLS edges and taint sniffer matches. |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/lua.go` | Lua constant-binding sniffer (luaLiteralRe, luaEnvOrRe, luaRequireRe) registered in substrate/lua.go. Feeds the language-agnostic constant propagation pass. |
| DB effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/OpenResty/Lapis effect sinks via resty.mysql/redis/pgmoon/lapis-db read+write sniffer |
| Dead code detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points_lua.go` | Dead-code detection via the reachability pass using Lua entry points. Partial: depends on quality of entry-point detection; global functions always marked as exports. |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_lua.go` | Lua def-use sniffer (local/bare assignments, function attribution via nearest function header) feeds the intra-procedural reaching-definitions pass. Partial: no SSA-phi precision. |
| Env fallback recognition | ✅ `full` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/lua.go` | Lua constant-binding sniffer (luaLiteralRe, luaEnvOrRe, luaRequireRe) registered in substrate/lua.go. Feeds the language-agnostic constant propagation pass. |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/Lapis effect sinks via io.open/io.read/io.write/os.rename/os.remove sniffer |
| HTTP effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/Lapis effect sinks via resty.http request/connect and ngx.location.capture sniffer |
| Import resolution quality | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/lua/lua.go`<br>`internal/links/constant_propagation.go` | Lua extractor emits IMPORTS edges for require() calls with local_name/source_module/import_kind properties. Cross-file resolution via constant propagation pass. Partial: no module path search-path resolution. |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC pass over IMPORTS edges. Lua extractor emits IMPORTS edges for require() calls, feeding the cycle detector. Partial: cross-repo cycles not tracked. |
| Mutation effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/Lapis effect sinks via table-field assignment (t.x=…/t[k]=…) sniffer |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | Language-agnostic pure-function pass: functions with no effects stamped as pure. Lua functions flow through the pass via entity graph. Partial: no Lua-specific effect sinks file yet (effects inferred from CALLS edges only). |
| Reachability analysis | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points_lua.go` | Lua entry-point sniffer (shebang/main/busted/love/openresty-init/kong-init) feeds the language-agnostic BFS reachability pass. Partial: framework-handler reachability via HTTP edges only. |
| Request shape extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; request/response shapes are Lua tables with no static schema declarations. No payload shapes sniffer applicable. |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; request/response shapes are Lua tables with no static schema declarations. No payload shapes sniffer applicable. |
| Sanitizer recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_lua.go` | Lua taint sniffer: sources=ngx.req.get_post/uri_args/body_data/headers/ngx.var.*/cjson.decode/params.*; sinks=db:query-concat/os.execute/io.popen/io.open/ngx.say/load; sanitizers=ngx.quote_sql_str/ngx.escape_uri/lapis.db.escape_literal/cjson.encode. Partial: no cross-function dataflow. |
| Schema drift detection | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; request/response shapes are Lua tables with no static schema declarations. No payload shapes sniffer applicable. |
| Taint sink detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_lua.go` | Lua taint sniffer: sources=ngx.req.get_post/uri_args/body_data/headers/ngx.var.*/cjson.decode/params.*; sinks=db:query-concat/os.execute/io.popen/io.open/ngx.say/load; sanitizers=ngx.quote_sql_str/ngx.escape_uri/lapis.db.escape_literal/cjson.encode. Partial: no cross-function dataflow. |
| Taint source detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_lua.go` | Lua taint sniffer: sources=ngx.req.get_post/uri_args/body_data/headers/ngx.var.*/cjson.decode/params.*; sinks=db:query-concat/os.execute/io.popen/io.open/ngx.say/load; sanitizers=ngx.quote_sql_str/ngx.escape_uri/lapis.db.escape_literal/cjson.encode. Partial: no cross-function dataflow. |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_lua.go` | Lua template-pattern sniffer detects ngx.log literals (log_format), i18n.translate/i18n() keys (i18n), and SQL verb string literals (sql). Feeds the language-agnostic catalog pass. |
| Vulnerability finding | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_lua.go` | Lua taint sniffer: sources=ngx.req.get_post/uri_args/body_data/headers/ngx.var.*/cjson.decode/params.*; sinks=db:query-concat/os.execute/io.popen/io.open/ngx.say/load; sanitizers=ngx.quote_sql_str/ngx.escape_uri/lapis.db.escape_literal/cjson.encode. Partial: no cross-function dataflow. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.lua.framework.lapis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
