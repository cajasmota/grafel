<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.bandit` — Bandit

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 21

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Endpoint synthesis | ❌ `missing` | — | — | — | — | — |
| Handler attribution | ❌ `missing` | — | — | — | — | — |

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
| Constant propagation | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.bandit ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
