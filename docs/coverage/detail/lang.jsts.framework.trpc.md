<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.trpc` — tRPC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 25

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Procedure extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/rules/javascript_typescript/frameworks/trpc.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-28` | 2865 | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/http_endpoint_trpc_schema.go`<br>`internal/engine/http_endpoint_trpc_schema_test.go`<br>`testdata/fixtures/typescript/trpc_input_schema.ts` | — |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | ✅ `full` | — | 2865 | `internal/engine/rules/javascript_typescript/frameworks/trpc.yaml`<br>`internal/engine/trpc_client_codegen_test.go`<br>`testdata/fixtures/typescript/trpc_client_codegen.ts` | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | ✅ `full` | `2026-05-28` | 2906 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_transport_binding.go`<br>`internal/engine/http_endpoint_transport_binding_test.go`<br>`testdata/fixtures/typescript/trpc_transport_http.ts`<br>`testdata/fixtures/typescript/trpc_transport_http_ws.ts`<br>`testdata/fixtures/typescript/trpc_transport_none.ts`<br>`testdata/fixtures/typescript/trpc_transport_ws.ts` | — |

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
(or use `go run ./tools/coverage update lang.jsts.framework.trpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
