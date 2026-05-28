<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.sails` — Sails

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 14

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2851) | `internal/engine/http_endpoint_jsts_backend.go`<br>`internal/engine/rules/javascript_typescript/frameworks/sails.yaml`<br>`testdata/fixtures/typescript/sails_routes.ts` | — |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2851) | `internal/engine/http_endpoint_jsts_backend.go`<br>`internal/engine/rules/javascript_typescript/frameworks/sails.yaml`<br>`testdata/fixtures/typescript/sails_routes.ts` | — |

### Auth

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2897) | `internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`internal/engine/http_endpoint_jsts_sails_auth.go`<br>`testdata/fixtures/typescript/sails_policies.ts`<br>`testdata/fixtures/typescript/sails_routes.ts` | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `request_validation` | ✅ `full` | — | — | [link](2904) | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go`<br>`testdata/fixtures/typescript/sails_validation.ts` | — |

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `middleware_coverage` | — `not_applicable` | — | — | — | — | Sails does not attach middleware to individual endpoints; its global middleware pipeline is the declarative `order` array under `middleware` in config/http.js. Covered by the framework_specific Middleware Pipeline / middleware_order_recognition cell (ParseSailsMiddlewareOrder). |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Observability

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `log_extraction` | ✅ `full` | — | — | [link](2905) | `internal/extractors/javascript/testdata/substrate_backend_observability/sails.ts`<br>`internal/patterns/observability_jsts_extractor.go` | — |

### Data

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `db_effect` | ✅ `full` | — | — | [link](2903) | `internal/extractors/javascript/testdata/substrate_backend_db/sails.ts`<br>`internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |

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

## Framework-specific

### Middleware Pipeline

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `middleware_order_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_jsts_middleware.go`<br>`internal/engine/http_endpoint_jsts_middleware_test.go`<br>`testdata/fixtures/typescript/sails_http.ts` | Sails has no per-endpoint middleware chain; its global middleware pipeline is the declarative `order` array under `middleware` in config/http.js. ParseSailsMiddlewareOrder extracts the named pipeline (fixture-proven). This is the framework_specific counterpart to the not_applicable standard middleware_coverage cell. |

### Sails Policies

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `policy_map_recognition` | ✅ `full` | `2026-05-29` | — | — | `internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`internal/engine/http_endpoint_jsts_sails_auth.go`<br>`testdata/fixtures/typescript/sails_policies.ts`<br>`testdata/fixtures/typescript/sails_routes.ts` | Sails gates actions via config/policies.js policy maps (e.g. '*': 'isLoggedIn'), a bespoke auth idiom no generic middleware/guard cell captures. ParseSailsPolicies parses the full map (global '*' default, per-controller object blocks with action-level overrides, controller-level catch-all values). #2897 ApplySailsAuthPolicy is a corpus-wide pass joining this map to the endpoints synthesised from config/routes.js, resolving each route's controller/action with action over controller over global precedence and stamping a config-method medium-confidence auth_policy (fixture-proven, cross-file), lifting the standard Security/auth_coverage cell from partial to full. Prior history: policies.js->routes.js per-endpoint attribution was deferred at #2852 and is now delivered. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.sails ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
