<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.graphql-resolvers` — GraphQL Resolvers (Apollo Server / GraphQL Yoga / etc.)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 25

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Procedure extraction | ✅ `full` | `2026-05-28` | 2932 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/graphql/frameworks/apollo_server.yaml`<br>`internal/engine/rules/graphql/frameworks/graphql_yoga.yaml`<br>`internal/extractors/graphql/graphql.go` | — |
| Schema extraction | ✅ `full` | `2026-05-28` | 2932 | `internal/engine/rules/graphql/frameworks/graphql_schema.yaml`<br>`internal/extractors/graphql/graphql.go` | — |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | — `not_applicable` | — | 2865 | — | Server-side resolver record: client codegen (graphql-codegen/Apollo) generates a typed CLIENT elsewhere, not in resolver source. |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | ✅ `full` | `2026-05-28` | 2906 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_transport_binding.go`<br>`internal/engine/http_endpoint_transport_binding_test.go`<br>`testdata/fixtures/typescript/graphql_transport_http.ts`<br>`testdata/fixtures/typescript/graphql_transport_http_ws.ts` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| DB effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Dead code detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/def_use_jsts.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Schema drift detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_jsts.go` | GraphQL resolver return values differ structurally from HTTP request/response bodies; payload_drift.go fires on jsts files but misses resolver-specific field patterns (issue notes borderline B) |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.graphql-resolvers ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
