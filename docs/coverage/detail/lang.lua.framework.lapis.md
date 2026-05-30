<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.lua.framework.lapis` — Lapis

Auto-generated. Back to [summary](../summary.md).

- **Language:** [lua](../by-language/lua.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | 🟢 `partial` | — | — | `internal/custom/lua/routing.go` | Regex extractor for Lapis app:get/post/put/delete/patch/match() routes and respond_to() verb tables. Partial: no AST; handler identity is syntactic only. |
| Handler attribution | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/routing.go` | Regex extractor for Lapis app:get/post/put/delete/patch/match() routes and respond_to() verb tables. Partial: no AST; handler identity is syntactic only. |
| Route extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/routing.go` | Regex extractor for Lapis app:get/post/put/delete/patch/match() routes and respond_to() verb tables. Partial: no AST; handler identity is syntactic only. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/auth.go` | Regex extractor for resty.jwt verify/decode, ngx.req.get_headers Authorization, access_by_lua_block gates, Lapis session/before_filter auth, and Kong :access() handlers. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/validation.go` | Regex extractor for ngx.req.get_post/uri_args, cjson.decode DTOs, lapis.validate/check_params/capture_errors, and resty.jsonschema schema validation. |
| Request validation | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/validation.go` | Regex extractor for ngx.req.get_post/uri_args, cjson.decode DTOs, lapis.validate/check_params/capture_errors, and resty.jsonschema schema validation. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/middleware.go` | Regex extractor for Lapis before_filter/app:before/app:after/error_handler patterns and lapis.flow pipeline. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static enum_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |
| Interface extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static interface_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |
| Type alias extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static type_alias_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |
| Type extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static type_extraction. LuaLS/EmmyLua annotations (---@type, ---@class) are optional and not common in Lapis/OpenResty codebases. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/testing.go` | Regex extractor for busted describe/it/hooks/assertions/spies, luaunit testXxx methods, lapis.spec integration test patterns, and Kong spec.helpers. Partial: full TESTS-edge linkage requires multi-hop HTTP engine pass. |

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
| Constant propagation | ✅ `full` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/lua.go` | Lua constant-binding sniffer (luaLiteralRe, luaEnvOrRe, luaRequireRe) registered in substrate/lua.go. Feeds the language-agnostic constant propagation pass. |
| DB effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/OpenResty/Lapis effect sinks via resty.mysql/redis/pgmoon/lapis-db read+write sniffer |
| Dead code detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points_lua.go` | Dead-code detection via the reachability pass using Lua entry points. Partial: depends on quality of entry-point detection; global functions always marked as exports. |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_lua.go` | Lua def-use sniffer (local/bare assignments, function attribution via nearest function header) feeds the intra-procedural reaching-definitions pass. Partial: no SSA-phi precision. |
| Env fallback recognition | ✅ `full` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/lua.go` | Lua constant-binding sniffer (luaLiteralRe, luaEnvOrRe, luaRequireRe) registered in substrate/lua.go. Feeds the language-agnostic constant propagation pass. |
| Fs effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/Lapis effect sinks via io.open/io.read/io.write/os.rename/os.remove sniffer |
| HTTP effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/Lapis effect sinks via resty.http request/connect and ngx.location.capture sniffer |
| Import resolution quality | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/lua/lua.go`<br>`internal/links/constant_propagation.go` | Lua extractor emits IMPORTS edges for require() calls with local_name/source_module/import_kind properties. Cross-file resolution via constant propagation pass. Partial: no module path search-path resolution. |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC pass over IMPORTS edges. Lua extractor emits IMPORTS edges for require() calls, feeding the cycle detector. Partial: cross-repo cycles not tracked. |
| Mutation effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/Lapis effect sinks via table-field assignment (t.x=…/t[k]=…) sniffer |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | Language-agnostic pure-function pass: functions with no effects stamped as pure. Lua functions flow through the pass via entity graph. Partial: no Lua-specific effect sinks file yet (effects inferred from CALLS edges only). |
| Reachability analysis | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points_lua.go` | Lua entry-point sniffer (shebang/main/busted/love/openresty-init/kong-init) feeds the language-agnostic BFS reachability pass. Partial: framework-handler reachability via HTTP edges only. |
| Request shape extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; request/response shapes are Lua tables with no static schema declarations. No payload shapes sniffer applicable. |
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
