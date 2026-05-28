<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.trpc` — tRPC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 4

## Capabilities


### Schema

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `procedure_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/rules/javascript_typescript/frameworks/trpc.yaml` | — |
| `schema_extraction` | ✅ `full` | `2026-05-28` | — | [link](2865) | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/http_endpoint_trpc_schema.go`<br>`internal/engine/http_endpoint_trpc_schema_test.go`<br>`testdata/fixtures/typescript/trpc_input_schema.ts` | — |

### Codegen

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `client_codegen` | ✅ `full` | — | — | [link](2865) | `internal/engine/rules/javascript_typescript/frameworks/trpc.yaml`<br>`internal/engine/trpc_client_codegen_test.go`<br>`testdata/fixtures/typescript/trpc_client_codegen.ts` | — |

### Transport

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `transport_binding` | ✅ `full` | `2026-05-28` | — | [link](2906) | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_transport_binding.go`<br>`internal/engine/http_endpoint_transport_binding_test.go`<br>`testdata/fixtures/typescript/trpc_transport_http.ts`<br>`testdata/fixtures/typescript/trpc_transport_http_ws.ts`<br>`testdata/fixtures/typescript/trpc_transport_none.ts`<br>`testdata/fixtures/typescript/trpc_transport_ws.ts` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.trpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
