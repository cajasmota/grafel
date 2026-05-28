<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.actix` — Actix Web

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 21

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/rust/frameworks/actix_web.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/rust/frameworks/actix_web.yaml` | — |

### Auth

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Auth coverage | ❌ `missing` | — | — | — | — | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Middleware coverage | ❌ `missing` | — | — | — | — | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

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
| Confidence overlay | ✅ `full` | `2026-05-28` | — | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.actix ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
