<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.micronaut` — Micronaut (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/micronaut_kotlin.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/micronaut_kotlin.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/routing.go`<br>`internal/custom/kotlin/routing_test.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

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
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/engine/rules/kotlin/test_patterns.yaml`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | Deep Kotlin TESTING linkage (#3437): junit5 @Test/@ParameterizedTest/@RepeatedTest + class-name subject; kotest StringSpec/FunSpec/DescribeSpec/BehaviorSpec/ShouldSpec/Spek DSL leaf cases with body call-scan; MockK mockk<T>() subject association with every{}/verify{} blocks blanked so the mocked call never leaks; Kotlin assertion/mockk stopwords (shouldBe/assertThrows/every/verify/any). Value-asserted in extractor_test.go (TestKotlin_JUnit5_*/Kotest_*/Mockk_*/Spek_*). |

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
| Transaction boundary extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/transactional.go` | java extractor language-gated to kotlin; @Transactional (JTA/Spring) on Kotlin proven by TestKotlinTransactional_Quarkus_Issue3274 (JTA path covers micronaut too) |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/transactional.go` | java extractor language-gated to kotlin; propagation capture via transactional.go txFrameworks["micronaut"] |
| Transaction rollback rules | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/transactional.go` | java extractor language-gated to kotlin; rollbackFor rules parsed by transactional.go txFrameworks["micronaut"] |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/micronaut_aop.go` | java extractor language-gated to kotlin; Micronaut @Around/@InterceptorBean on Kotlin proven by TestKotlinMicronautAOP_Interceptor_Issue3274 |
| Aspect extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/micronaut_aop.go` | java extractor language-gated to kotlin; Micronaut interceptor/aspect extraction proven by TestKotlinMicronautAOP_Interceptor_Issue3274 |
| Pointcut resolution | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/micronaut_aop.go` | java extractor language-gated to kotlin; proven by TestKotlinMicronautAOP_Interceptor_Issue3274 |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/kotlin/observability.go`<br>`internal/custom/kotlin/observability_test.go` | #4015: custom_kotlin_observability fires on Micronaut .kt (Java pass is dead for kotlin — patterns_dispatch skips non-java). SLF4J LoggerFactory.getLogger + log.info partial; TestKotlinObservability_Micronaut_Issue4015. Partial: regex file-local, no cross-file logger correlation, interpolated messages. |
| Metric extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/kotlin/observability.go`<br>`internal/custom/kotlin/observability_test.go` | #4015: Micronaut Micrometer literal names asserted — @Timed("orders.count"), @Counted("orders.created"), meterRegistry.counter("orders.listed"); TestKotlinObservability_Micronaut_Issue4015. metric_name_source=literal. |
| Trace extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/kotlin/observability.go`<br>`internal/custom/kotlin/observability_test.go` | #4015: Micronaut Tracing @NewSpan("load") literal span name asserted; TestKotlinObservability_Micronaut_Issue4015. span_name_source=literal. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_kotlin.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
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
