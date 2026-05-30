<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.micronaut` — Micronaut (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 45

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/micronaut_kotlin.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/micronaut_kotlin.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/routing.go`<br>`internal/custom/kotlin/routing_test.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/kotlin/micronaut_quarkus.go` | @Secured(rule)/@RolesAllowed(roles)/@PermitAll — specific rule + role strings captured by quoted-string match, value-asserted, file-local |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/kotlin/micronaut_quarkus.go` | @Filter/@ServerFilter HttpServerFilter classes captured by name — value-asserted, file-local |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/engine/rules/kotlin/test_patterns.yaml`<br>`internal/substrate/entry_points_kotlin.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/kotlin/kotlin.go` | — |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/kotlin/kotlin.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/kotlin/kotlin.go` | — |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/kotlin/kotlin.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/micronaut_quarkus.go` | @Singleton/@Prototype/@RequestScoped class and @Bean factory method detection — file-local |
| DI injection point | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/micronaut_quarkus.go` | @Inject property injection detection — file-local |
| DI scope resolution | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/micronaut_quarkus.go` | @Singleton/@Prototype/@RequestScoped scope annotation extraction — file-local |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/transactional.go` | java extractor language-gated to kotlin; @Transactional (JTA/Spring) on Kotlin proven by TestKotlinTransactional_Quarkus_Issue3274 (JTA path covers micronaut too) |
| Transaction propagation | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/transactional.go` | java extractor language-gated to kotlin; propagation capture via transactional.go txFrameworks["micronaut"] |
| Transaction rollback rules | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/transactional.go` | java extractor language-gated to kotlin; rollbackFor rules parsed by transactional.go txFrameworks["micronaut"] |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/micronaut_aop.go` | java extractor language-gated to kotlin; Micronaut @Around/@InterceptorBean on Kotlin proven by TestKotlinMicronautAOP_Interceptor_Issue3274 |
| Aspect extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/micronaut_aop.go` | java extractor language-gated to kotlin; Micronaut interceptor/aspect extraction proven by TestKotlinMicronautAOP_Interceptor_Issue3274 |
| Pointcut resolution | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/micronaut_aop.go` | java extractor language-gated to kotlin; proven by TestKotlinMicronautAOP_Interceptor_Issue3274 |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/observability.go` | java extractor language-gated to kotlin; SLF4J/@Slf4j on Kotlin proven by TestKotlinObservability_Slf4j_Issue3274 (obsFrameworks["micronaut"]=true) |
| Metric extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/observability.go` | java extractor language-gated to kotlin; Micrometer @Timed on Kotlin proven by TestKotlinObservability_Micrometer_Issue3274 (obsFrameworks["micronaut"]=true) |
| Trace extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/observability.go` | java extractor language-gated to kotlin; @WithSpan OTel on Kotlin proven by TestKotlinObservability_OTel_Issue3274 |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_kotlin.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.micronaut ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
