<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.jaxrs` — JAX-RS / Jakarta REST

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 48

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/jakarta_ee.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/java_annotation_routes.go` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-28` | — | `internal/engine/java_auth_policy.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go` | — |
| Request validation | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_params.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-06-01` | — | `internal/custom/java/jaxrs_filters.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/junit5.go` | JUnit 5 tests in JAX-RS projects; @Test/@ParameterizedTest/@RepeatedTest extracted; OWNS edge; TestJakartaEE_TestsLinkage_Issue2996 value-asserting (same JUnit 5 extractor gates on jaxrs) |

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
| DI binding extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/jakarta_ee.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/java_edge_carrier_reverify_test.go` | #3605 (epic #3584): CDI/EJB bean entities emit live AND the @EJB/@Inject DEPENDS_ON injection edge now materialises — patternResultToRecords synthesises the injecting-class carrier (SCOPE.Class) for the previously carrier-less SourceRef. TestReverifyJaxrsInjectionPointCarrier asserts the DEPENDS_ON UserResource->UserService (kind=ejb_inject) edge live. |
| DI injection point | ✅ `full` | `2026-06-02` | — | `internal/custom/java/jakarta_ee.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/java_edge_carrier_reverify_test.go` | #3605 (epic #3584): the @Inject/@EJB injection DEPENDS_ON edge now reaches the graph — patternResultToRecords synthesises a SCOPE.Class carrier for its SourceRef (scope:dependency:jakarta:...) instead of dropping it, and surfaces edge-only PatternResults. TestReverifyJaxrsInjectionPointCarrier asserts the materialised carrier + DEPENDS_ON edge live. |
| DI scope resolution | ✅ `full` | `2026-06-02` | — | `internal/custom/java/jakarta_ee.go`<br>`internal/custom/java/java_di_scope_deepen.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/java_edge_carrier_reverify_test.go` | #3605 (epic #3584): CDI scope components emit live AND the DEPENDS_ON injection wiring into them now materialises via the synthesised injecting-class carrier (no longer dropped). TestReverifyJaxrsInjectionPointCarrier proves the injection edge reaches the graph. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/transactional.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | @Transactional class/method boundaries; jaxrs in txFrameworks; OWNS edge; TestTransactional_FrameworkGating_Issue3003 verifies jaxrs |
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/java/java.go`<br>`internal/extractors/java/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: @Transactional (Spring + Jakarta/JTA) on a method stamps transactional=true + tx_propagation/tx_isolation/tx_read_only on that method entity; class-level @Transactional propagates to all enclosing methods (method-level annotation wins on specificity). No transitive propagation across calls. |
| Transaction propagation | ✅ `full` | `2026-06-01` | — | `internal/custom/java/transactional.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | propagation/TxType; jaxrs in txFrameworks; TestTransactional_FrameworkGating_Issue3003 |
| Transaction rollback rules | ✅ `full` | `2026-06-01` | — | `internal/custom/java/transactional.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | rollbackFor/noRollbackFor; jaxrs in txFrameworks; TestTransactional_FrameworkGating_Issue3003 |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | ✅ `full` | `2026-06-01` | — | `internal/custom/java/cdi_interceptors.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | CDI @Interceptor + @AroundInvoke/@AroundConstruct methods extracted as SCOPE.Pattern(subtype=advice) with advice_type (around_invoke/around_construct) + aspect + framework properties; OWNS edge; value-asserting TestCDI_JAXRS_InterceptorClass_Issue3082 |
| Aspect extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/cdi_interceptors.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | @Interceptor-annotated classes detected as SCOPE.Pattern(subtype=aspect, kind=cdi_interceptor) with framework=jaxrs; TestCDI_JAXRS_InterceptorClass_Issue3082 value-asserting |
| Pointcut resolution | 🟢 `partial` | `2026-06-01` | [link](https://github.com/cajasmota/archigraph/issues/3589) | `internal/custom/java/cdi_interceptors.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Pointcut entities emit only via the @InterceptorBinding selector path; a plain @Interceptor+@AroundInvoke pair emits aspect+advice but no separate pointcut entity. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | Same extractor as spring-boot; jaxrs in obsFrameworks gate; SLF4J/@Slf4j, Log4j, JUL + log statement call surface; TestObservability_FrameworkGating_Issue3006 verifies jaxrs |
| Metric extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | Micrometer + @Timed + @Counted/@Metered/@Gauge; jaxrs in obsFrameworks |
| Trace extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | OTel @WithSpan + spanBuilder(); Micrometer @Observed + nextSpan(); jaxrs in obsFrameworks |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3093) | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Framework-blind substrate: constant_propagation, effect_propagation, and taint_flow passes emit per-binding/per-finding Confidence values on Java entities via java.go sniffers. EntityRecord.Confidence not yet stamped by the Java extractor directly; MCP min_confidence filtering applies. Partial pending a dedicated confidence-scoring pass writing top-level EntityRecord.Confidence. |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/java/config_consumer.go`<br>`internal/extractors/java/config_consumer_test.go` | @Value, @ConfigurationProperties, env.getProperty, @ConfigProperty -> config:<key> (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.jaxrs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
