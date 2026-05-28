<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.axum` — Axum

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 21

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_axum.go`<br>`internal/engine/rules/rust/frameworks/axum.yaml` | — |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_axum.go` | — |

### Security

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `middleware_coverage` | ❌ `missing` | — | — | — | — | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Observability

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Data

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `confidence_overlay` | ✅ `full` | `2026-05-28` | — | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| `constant_propagation` | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| `db_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| `dead_code_detection` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_rust.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| `fs_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| `http_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| `import_resolution_quality` | ⚠️ `partial` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| `mutation_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| `reachability_analysis` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_rust.go` | — |
| `request_shape_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| `response_shape_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| `sanitizer_recognition` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| `schema_drift_detection` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| `taint_sink_detection` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| `taint_source_detection` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| `vulnerability_finding` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.axum ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
