<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.microprofile` — Eclipse MicroProfile

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 45

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Endpoint synthesis | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/java/frameworks/microprofile.yaml` | — |
| Handler attribution | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/java/frameworks/microprofile.yaml` | — |
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
| Enum extraction | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/java/java.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/java/java.go` | — |
| Type alias extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Type extraction | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/java/java.go` | — |

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
| DB effect | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Def use chain extraction | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| HTTP effect | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Mutation effect | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Pure function tagging | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Reachability analysis | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Taint source detection | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Template pattern catalog | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |
| Vulnerability finding | ❌ `missing` | — | — | [link](backfill:dictionary-completeness) | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.microprofile ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
