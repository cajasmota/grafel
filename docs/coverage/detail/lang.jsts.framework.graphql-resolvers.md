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
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.graphql-resolvers ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
