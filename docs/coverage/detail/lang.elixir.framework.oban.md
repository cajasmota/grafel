<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.oban` — Oban (job queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | — `not_applicable` | — | — | — | — |
| Handler attribution | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/elixir/frameworks/oban.yaml` | — |
| Route extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | — `not_applicable` | — | — | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Def use chain extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Template pattern catalog | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.oban ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
