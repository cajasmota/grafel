<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.helidon` — Helidon

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 45

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ⚠️ `partial` | `2026-05-29` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/microprofile.yaml` | MicroProfile JAX-RS @Path + verb annotations covered by java_annotation_routes.go; partial (no class-level @Path composition with vert.x-style mounts) |
| Handler attribution | ⚠️ `partial` | `2026-05-29` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/microprofile.yaml` | JAX-RS method-level handler attribution via SCOPE.Operation entity; same pass as Quarkus/Jakarta EE |
| Route extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/microprofile.yaml` | JAX-RS @Path annotation route extraction; class+method composition; MicroProfile flavor same as Quarkus |

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

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |
| Type alias extraction | — `not_applicable` | — | — | — | Java has no type alias syntax |
| Type extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction propagation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction rollback rules | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Aspect extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pointcut resolution | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3006) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |
| Metric extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3006) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |
| Trace extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3006) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.helidon ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
