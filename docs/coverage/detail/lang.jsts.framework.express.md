<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.express` — Express

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 14

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | [link](2932) | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/express.yaml`<br>`internal/extractors/javascript/framework_dsl.go` | — |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | [link](2932) | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/javascript_typescript/frameworks/express.yaml` | — |

### Auth

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ✅ `full` | `2026-05-28` | — | — | `cmd/archigraph/audit2852_jsauth_test.go`<br>`internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`testdata/fixtures/typescript/express_auth.ts` | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `request_validation` | ✅ `full` | — | — | [link](2904) | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go`<br>`testdata/fixtures/typescript/express_validation.ts` | — |

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `middleware_coverage` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_jsts_middleware.go`<br>`internal/engine/http_endpoint_jsts_middleware_test.go`<br>`testdata/fixtures/typescript/express_middleware.ts` | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Observability

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `log_extraction` | ✅ `full` | — | — | [link](2905) | `internal/extractors/javascript/testdata/substrate_backend_observability/express.ts`<br>`internal/patterns/observability_jsts_extractor.go` | — |

### Data

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `db_effect` | ✅ `full` | — | — | [link](2903) | `internal/extractors/javascript/testdata/substrate_backend_db/express.ts`<br>`internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `confidence_overlay` | ✅ `full` | `2026-05-28` | — | [link](2932) | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `request_shape_extraction` | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| `response_shape_extraction` | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| `schema_drift_detection` | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.express ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
