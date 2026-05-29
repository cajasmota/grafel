<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.polka` — Polka

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2851) | `internal/engine/http_endpoint_jsts_backend.go`<br>`internal/engine/rules/javascript_typescript/frameworks/polka.yaml`<br>`testdata/fixtures/typescript/polka_routes.ts` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2851) | `internal/engine/http_endpoint_jsts_backend.go`<br>`internal/engine/rules/javascript_typescript/frameworks/polka.yaml`<br>`testdata/fixtures/typescript/polka_routes.ts` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | 3062 | `internal/engine/http_endpoint_jsts_backend.go`<br>`internal/engine/http_endpoint_jsts_backend_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_jsts_route_3062_test.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`testdata/fixtures/typescript/polka_auth.ts` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | — | 3073 | `internal/extractors/javascript/issue3073_dto_extraction_test.go`<br>`internal/extractors/javascript/validation_linkage.go`<br>`testdata/fixtures/typescript/express_dto.ts`<br>`testdata/fixtures/typescript/fastify_dto.ts`<br>`testdata/fixtures/typescript/feathers_dto.ts`<br>`testdata/fixtures/typescript/hapi_dto.ts`<br>`testdata/fixtures/typescript/hono_dto.ts`<br>`testdata/fixtures/typescript/koa_dto.ts`<br>`testdata/fixtures/typescript/marblejs_dto.ts`<br>`testdata/fixtures/typescript/polka_dto.ts`<br>`testdata/fixtures/typescript/restify_dto.ts`<br>`testdata/fixtures/typescript/sails_dto.ts` | — |
| Request validation | ✅ `full` | — | 2904 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go`<br>`testdata/fixtures/typescript/polka_validation.ts` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/engine/http_endpoint_jsts_middleware.go`<br>`internal/engine/http_endpoint_jsts_middleware_test.go`<br>`testdata/fixtures/typescript/polka_middleware.ts` | — |

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
| Log extraction | ✅ `full` | — | 2905 | `internal/extractors/javascript/testdata/substrate_backend_observability/polka.ts`<br>`internal/patterns/observability_jsts_extractor.go` | — |
| Metric extraction | 🟢 `partial` | `2026-05-29` | 3050 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | Heuristic import-pattern matching (prom-client, OTel metrics): fires when the app imports these specific libraries. Framework-agnostic but not comprehensive. |
| Trace extraction | 🟢 `partial` | `2026-05-29` | 3050 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | Heuristic import-pattern matching (OTel tracing, Sentry): fires when the app imports these specific libraries. Framework-agnostic but not comprehensive. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ✅ `full` | — | 2903 | `internal/extractors/javascript/testdata/substrate_backend_db/polka.ts`<br>`internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | 2932 | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3048 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3048 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3048 | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3048 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3048 | `internal/substrate/template_pattern_jsts.go`<br>`internal/substrate/template_pattern_test.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3048 | `internal/links/taint_flow.go`<br>`internal/links/taint_flow_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.polka ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
