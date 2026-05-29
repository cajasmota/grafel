<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.struts` — Apache Struts

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 45

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/java/frameworks/apache_struts.yaml` | — |
| Handler attribution | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/java/frameworks/apache_struts.yaml` | — |
| Route extraction | ⚠️ `partial` | `2026-05-29` | 3089 | `internal/custom/java/struts_routes.go`<br>`internal/custom/java/struts_routes_test.go` | Extracts @Action(value=...) annotations and struts.xml <action> elements; @Namespace prefix composition; HANDLED_BY relationships to action classes |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ⚠️ `partial` | `2026-05-29` | 3089 | `internal/custom/java/struts_routes.go`<br>`internal/custom/java/struts_routes_test.go` | Detects JAAS (LoginContext/Subject) and Spring Security (SecurityContextHolder/@PreAuthorize/@Secured) integration markers; Struts has no built-in auth, so detection is heuristic based on common integration patterns |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ⚠️ `partial` | `2026-05-29` | 3089 | `internal/custom/java/struts_routes.go`<br>`internal/custom/java/struts_routes_test.go` | Detects Interceptor/AbstractInterceptor implementors and intercept(ActionInvocation) overrides; the Struts interceptor stack is the primary middleware mechanism |

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
| DI binding extraction | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | Struts 2 has no intrinsic DI; Spring-plugin DI is optional and handled by the Spring DI extractor |
| DI injection point | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | Struts 2 has no intrinsic DI injection points; Spring-plugin injection is optional |
| DI scope resolution | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | Struts 2 has no intrinsic DI scope management |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | Struts has no transaction management; projects use Spring @Transactional or JTA outside the framework |
| Transaction propagation | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | No transaction propagation in Struts core; deferred to Spring/JTA layer |
| Transaction rollback rules | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | No rollback-rule declarations in Struts; deferred to Spring/JTA layer |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | Struts uses interceptor chain for cross-cutting concerns, not AspectJ AOP; Spring AOP extractor must not fire for framework=struts |
| Aspect extraction | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | Struts uses interceptors not AspectJ @Aspect |
| Pointcut resolution | — `not_applicable` | — | 3089 | `internal/custom/java/struts_routes.go` | Struts has no pointcut concept; interceptors cover this role |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3006) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |
| Metric extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3006) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |
| Trace extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3006) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3093) | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Framework-blind substrate: constant_propagation, effect_propagation, and taint_flow passes emit per-binding/per-finding Confidence values on Java entities via java.go sniffers. EntityRecord.Confidence not yet stamped by the Java extractor directly; MCP min_confidence filtering applies. Partial pending a dedicated confidence-scoring pass writing top-level EntityRecord.Confidence. |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.struts ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
