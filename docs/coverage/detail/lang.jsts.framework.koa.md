<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.koa` — Koa

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/framework_dsl.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/framework_dsl.go` | — |
| Route extraction | ⚠️ `partial` | `2026-05-29` | 3062 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_jsts_route_3062_test.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`testdata/fixtures/typescript/koa_auth.ts` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | ✅ `full` | — | 2904 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go`<br>`testdata/fixtures/typescript/koa_validation.ts` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/engine/http_endpoint_jsts_middleware.go`<br>`internal/engine/http_endpoint_jsts_middleware_test.go`<br>`testdata/fixtures/typescript/koa_middleware.ts` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Interface extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/tests.go`<br>`internal/extractors/javascript/tests_test.go` | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ✅ `full` | — | 2905 | `internal/extractors/javascript/testdata/substrate_backend_observability/koa.ts`<br>`internal/patterns/observability_jsts_extractor.go` | — |
| Metric extraction | ⚠️ `partial` | `2026-05-29` | 3050 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | Heuristic import-pattern matching (prom-client, OTel metrics): fires when the app imports these specific libraries. Framework-agnostic but not comprehensive. |
| Trace extraction | ⚠️ `partial` | `2026-05-29` | 3050 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | Heuristic import-pattern matching (OTel tracing, Sentry): fires when the app imports these specific libraries. Framework-agnostic but not comprehensive. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ✅ `full` | — | 2903 | `internal/extractors/javascript/testdata/substrate_backend_db/koa.ts`<br>`internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | 2932 | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | Framework-blind BFS reachability seeded from entry_points_jsts.go (exports, main, test entries, lifecycle names). Framework-specific entrypoints (Fastify plugin.register, Hono app.get) rely on graph HTTP endpoint entities as supplementary BFS seeds. |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_test.go` | Framework-blind heuristic: sniffDefUseJSTS fires on all JS/TS. Nearest-preceding-header attribution is imprecise for nested closures; common module/class-method case is correct. |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsFSReadRe/jstsFSWriteRe fire on all JS/TS covering Node fs/fs.promises primitives. Syntactic line-based attribution; framework-specific file helpers not covered. |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsHTTPRe detects outbound HTTP clients (fetch/axios/got/ky/superagent/XHR). Inbound route-handler effects not captured; confidence 1.0 for matched call sites. |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; applies equally to all JS/TS including these frameworks. Accuracy depends on import resolution quality of the JS/TS extractor. |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsMutationRe detects this.field= receiver assignments (confidence 0.7). Plain variable mutations and array mutations not covered. |
| Pure function tagging | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_jsts.go` | Derives from effect propagation: functions without detected effects are tagged pure (confidence 0.30). Absence-of-detection is not proof of absence; framework handler closures may be mis-attributed. |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | BFS from export/main/test/lifecycle entry points and HTTP endpoint graph entities across CALLS/IMPORTS/REFERENCES edges. Entry-point detection is regex-based heuristic for framework handler functions. |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers DOMPurify.sanitize, validator.escape, lodash.escape, he.encode, parameterised SQL, and zod/joi/yup schema declarations (per hard rule in issue 2772). |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers SQL injection (db.query), command injection (exec/eval/new Function), path traversal (fs.* non-literal), XSS (innerHTML/res.send/dangerouslySetInnerHTML), ReDoS. Hono c.json()/Koa ctx.body response API not in sink set. |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind via jstsSourceReqRe (req.*/request.*/ctx.request.*). Covers Express/Fastify/Koa req.body/query/params/headers/cookies well. Hapi request.payload and Hono c.req.json()/c.req.param() not matched by current regex. |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/substrate/template_pattern_jsts.go`<br>`internal/substrate/template_pattern_test.go` | Framework-blind: sniffTemplatePatternsJSTS covers i18n t(), log.*(), and SQL string literals across all JS/TS. Framework-specific templating (Koa ctx.render, Hono c.html) not covered. |
| Vulnerability finding | ⚠️ `partial` | `2026-05-29` | 3046 | `internal/links/taint_flow.go`<br>`internal/links/taint_flow_test.go` | Framework-blind: taint_flow.go propagates source-to-sink paths identified by sniffTaintJSTS. Quality inherits from partial taint_source/sink coverage; Hapi/Hono-specific sources are underdetected. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.koa ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
