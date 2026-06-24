<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.lua.framework.openresty` — OpenResty

Auto-generated. Back to [summary](../summary.md).

- **Language:** [lua](../by-language/lua.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | — | — | `internal/engine/lua_routes.go` | OpenResty routes synthesized to canonical http_endpoint via synthesizeOpenResty: nginx location stanzas (content_by_lua-gated, ANY verb) + lua-resty-router r:get/post/... DSL with :id->{id} normalization (httproutes.FrameworkOpenResty); value-asserting tests in lua_routes_test.go. Pure nginx.conf location blocks (non-lua-classified) covered by custom extractor internal/custom/lua/routing.go which stamps canonical_path. |
| Handler attribution | ✅ `full` | — | — | `internal/custom/lua/routing.go`<br>`internal/engine/lua_routes.go` | OpenResty routes synthesized to canonical http_endpoint via synthesizeOpenResty: nginx location stanzas (content_by_lua-gated, ANY verb) + lua-resty-router r:get/post/... DSL with :id->{id} normalization (httproutes.FrameworkOpenResty); value-asserting tests in lua_routes_test.go. Pure nginx.conf location blocks (non-lua-classified) covered by custom extractor internal/custom/lua/routing.go which stamps canonical_path. |
| Route extraction | ✅ `full` | `2026-06-24` | 5365 | `internal/custom/lua/extractors_test.go`<br>`internal/custom/lua/routing.go`<br>`internal/engine/lua_routes.go` | OpenResty routes synthesized to canonical http_endpoint via synthesizeOpenResty: nginx location stanzas (content_by_lua-gated) + lua-resty-router r:get/post/... DSL with :id->{id} normalization (httproutes.FrameworkOpenResty); value-asserting tests in lua_routes_test.go. #5365 routing depth: each location block is now brace-balanced (nginxLocationBlockBody) to attribute its Lua phase handler (rewrite/access/content/header_filter/body_filter/log_by_lua) and per-location HTTP method set (limit_except / $request_method guards) onto the route — replacing the previous ANY-verb assumption — verified in extractors_test.go TestLuaRoutingOpenRestyDepth. |
| Websocket route extraction | 🔴 `missing` | `2026-06-14` | — | — | #4965: dedicated websocket_route_extraction not yet implemented for this framework. The capability key was introduced for the rust axum/actix/warp WS extractor (internal/custom/rust/websocket_routes.go); this framework's WebSocket-upgrade idiom is not yet recognised and is a follow-up gap. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/lua/auth.go` | OpenResty auth coverage: resty.jwt verify/decode/load_jwt (auth_method=jwt), lua-resty-openidc require + openidc.authenticate (auth_method=oidc), Authorization header + cookie/session checks (auth_method=session), access_by_lua gates, Kong :access() handlers. Each guard stamps auth_method (jwt/oidc/session); value-asserting tests TestLuaAuthJWT + TestLuaAuthOIDC in extractors_test.go. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/validation.go` | OpenResty DTO extraction via ngx.req.get_post/uri_args() + ngx.req.read_body()/get_body_data() and cjson.decode JSON ingestion. Partial: no cross-file dataflow or type binding; DTO field set is not resolved from the decoded body. |
| Request validation | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/validation.go` | OpenResty request validation via ngx.exit(400/401/422/HTTP_BAD_REQUEST) guards and resty.jsonschema schema-validation import. Partial: regex guard/import heuristics without the specific field+rule binding the Lapis assert_valid path captures. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/lua/middleware.go` | OpenResty middleware chain: nginx phase directives (init/init_worker/rewrite/access/content/header_filter/body_filter/log _by_lua) emitted with chain_index (textual order) + phase_order (canonical request-lifecycle rank) so the ordered chain is reconstructable; Kong plugin handler phases. value-asserting test TestLuaMiddlewareOrdering asserts specific phase_order ranks. |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/grafel/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static enum_extraction. LuaLS/EmmyLua annotations are not common in OpenResty codebases. |
| Interface extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static interface_extraction. LuaLS/EmmyLua annotations are not common in OpenResty codebases. |
| Type alias extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static type_alias_extraction. LuaLS/EmmyLua annotations are not common in OpenResty codebases. |
| Type extraction | — `not_applicable` | — | — | `internal/extractors/lua/lua.go` | Lua is dynamically typed; no static type_extraction. LuaLS/EmmyLua annotations are not common in OpenResty codebases. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | — | — | `internal/custom/lua/testing.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks_lua.go`<br>`internal/extractors/cross/testmap/resolver.go` | busted (describe/it) + luaunit (TestClass:testXxx) registered in the shared testmap extractor: each test case emits a TESTS edge to the production symbol via direct-call resolution (high), describe-subject / Test<Subject> class fallback (medium), and *_spec.lua/*_test.lua naming convention (low). Lua block-body extractor balances function/if/do...end (quote+long-bracket aware); busted/luaunit assertion DSL stop-worded. custom/lua/testing.go still surfaces standalone test-pattern nodes. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/lua/observability.go` | ngx.log(ngx.LEVEL, ...) captures the log LEVEL (prop level: ERR/WARN/INFO/DEBUG etc.); resty.logger.socket / print / io.write captured as log call-sites. PARTIAL by design: the log message and the logger->sink binding require cross-file dataflow that this regex extractor does not resolve. |
| Metric extraction | ✅ `full` | — | — | `internal/custom/lua/observability.go` | prometheus:counter/histogram/gauge("name") capture the metric name from the string literal in-call (prop metric_name); value-asserting tests prove requests_total/request_duration_ms. No cross-file resolution needed. Non-literal names flagged metric_name=<unresolved>. statsd op-types also captured. |
| Trace extraction | ✅ `full` | — | — | `internal/custom/lua/observability.go` | tracer:start_span("name") and kong.tracing.start_span("name") capture the span name from the string literal in-call (prop span_name); value-asserting test proves handle_request/db.query. No cross-file resolution needed. Non-literal span names fall back to span_op (partial subset). |

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
| Fs effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/OpenResty effect sinks via resty.*/io.*/table-mutation sniffer |
| HTTP effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/OpenResty effect sinks via resty.http request/connect and ngx.location.capture sniffer |
| Import resolution quality | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/lua/lua.go`<br>`internal/links/constant_propagation.go` | Lua extractor emits IMPORTS edges for require() calls with local_name/source_module/import_kind properties. Cross-file resolution via constant propagation pass. Partial: no module path search-path resolution. |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC pass over IMPORTS edges. Lua extractor emits IMPORTS edges for require() calls, feeding the cycle detector. Partial: cross-repo cycles not tracked. |
| Mutation effect | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_lua.go` | Lua/OpenResty effect sinks via table-field assignment (t.x=…/t[k]=…) sniffer |
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
(or use `go run ./tools/coverage update lang.lua.framework.openresty ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
