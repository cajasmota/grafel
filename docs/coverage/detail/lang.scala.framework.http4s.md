<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.http4s` — http4s

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 45

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Endpoint synthesis | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/scala/frameworks/http4s.yaml` | — |
| Handler attribution | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/scala/frameworks/http4s.yaml` | — |
| Route extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### Auth

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Auth coverage | ❌ `missing` | — | — | — | — | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| DTO extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Request validation | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Middleware coverage | ❌ `missing` | — | — | — | — | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Tests linkage | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Enum extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Interface extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Type alias extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Type extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### DI

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| DI binding extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| DI injection point | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| DI scope resolution | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### Transactions

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Transaction boundary extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Transaction propagation | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Transaction rollback rules | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### AOP

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Advice attribution | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Aspect extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Pointcut resolution | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### Observability

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Log extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Metric extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Trace extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### Data

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Def use chain extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Mutation effect | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Pure function tagging | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Template pattern catalog | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.http4s ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
