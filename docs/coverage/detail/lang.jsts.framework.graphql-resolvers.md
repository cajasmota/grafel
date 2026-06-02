<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.graphql-resolvers` — GraphQL Resolvers (Apollo Server / GraphQL Yoga / etc.)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 26

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
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-29` | 3076 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/reachability.go`<br>`internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3076 | `internal/substrate/def_use_jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | 3076 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Import resolution quality | ✅ `full` | `2026-05-29` | 3076 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/module_cycle_pass.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/pure_function_pass.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-29` | 3076 | `internal/substrate/payload_shapes_graphql.go`<br>`internal/substrate/payload_shapes_jsts.go`<br>`testdata/fixtures/graphql/schema.graphql` | — |
| Response shape extraction | 🟢 `partial` | `2026-05-29` | 3076 | `internal/substrate/payload_shapes_graphql.go`<br>`internal/substrate/payload_shapes_jsts.go`<br>`testdata/fixtures/graphql/schema.graphql` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Schema drift detection | ✅ `full` | `2026-05-29` | 3076 | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_graphql.go`<br>`internal/substrate/payload_shapes_graphql_test.go`<br>`internal/substrate/payload_shapes_jsts.go`<br>`testdata/fixtures/graphql/schema.graphql` | GraphQL SDL sniffing added (#3076 B-part): input types map to request shapes, object types to response shapes, and inline operation args to per-operation request shapes. payload_drift.go picks these up via the generic PayloadShapeSnifferFor dispatch after LanguageForPath returns graphql for .graphql/.gql files. |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3076 | `internal/substrate/template_pattern_jsts.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3076 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_graphql/resolver.ts` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.graphql-resolvers ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
