<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.strawberry-graphql` — Strawberry GraphQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/graphql/frameworks/strawberry_python.yaml`<br>`internal/engine/rules/python/frameworks/strawberry_graphql.yaml` | — |
| Handler attribution | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/python/frameworks/strawberry_graphql.yaml` | — |
| Route extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ❌ `missing` | — | — | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ❌ `missing` | — | — | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |
| Interface extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |
| Type alias extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |
| Type extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2989) | `internal/extractors/python/types.go` | — |

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
| DB effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/def_use_python.go`<br>`internal/substrate/def_use_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/substrate/template_pattern_python.go`<br>`internal/substrate/template_pattern_test.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-29` | 2972 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.strawberry-graphql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
