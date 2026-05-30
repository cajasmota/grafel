<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.spring-boot` — Spring Boot / Spring MVC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 48

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/java/frameworks/spring_boot.yaml`<br>`internal/engine/rules/java/frameworks/spring_mvc.yaml`<br>`internal/engine/spring_routes.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/spring_routes.go` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go`<br>`internal/engine/spring_routes.go` | Annotation-driven route composition scanned (@RequestMapping/@GetMapping/etc.); path-variable expression resolution not implemented |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/java_auth_policy.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/spring_request_response.go` | SCOPE.Schema(kind=dto) entities emitted for @RequestBody parameter types and ResponseEntity<T>/Mono<T>/Flux<T> return types; generic collections (List/Map/Set) skipped via srrSkipTypes |
| Request validation | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_params.go` | Bean Validation annotations (@NotNull, @NotBlank, @NotEmpty, @Valid, @Min, @Max, @Size, @Pattern, @Email) captured per handler parameter; required flag set; no field-level constraint recursion |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-05-28` | — | `internal/engine/java_annotation_params.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/junit5.go` | @Test/@ParameterizedTest/@RepeatedTest methods extracted; @BeforeEach/@AfterEach lifecycle methods captured; @Nested classes emitted; @ExtendWith extensions; OWNS edge from class to test method; @SpringBootTest/@WebMvcTest class-level annotations recognised; value-asserting test TestSpringBoot_TestsLinkage_Issue2991 |

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
| DI binding extraction | 🟢 `partial` | `2026-05-28` | backfill:dictionary-completeness | `internal/custom/java/spring_boot.go` | — |
| DI injection point | 🟢 `partial` | `2026-05-28` | backfill:dictionary-completeness | `internal/custom/java/spring_boot.go` | — |
| DI scope resolution | 🟢 `partial` | `2026-05-29` | 3081 | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_boot.go` | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/transactional.go` | @Transactional on class/method detected; class-level boundary + method-level SCOPE.Pattern(subtype=transaction_boundary) with declaring_class, framework; OWNS edges from class to method boundaries; Jakarta/JTA TxType positional form handled; value-asserting fixture TestTransactional_Boundary_Propagation_Rollback_Issue3003 |
| Transaction propagation | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/transactional.go` | propagation=Propagation.<MODE> (Spring) and TxType.<MODE> (JTA) extracted into propagation property; isolation + readOnly also captured; all Spring propagation modes + JTA TxType modes covered; value-asserting test TestTransactional_Boundary_Propagation_Rollback_Issue3003 |
| Transaction rollback rules | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/transactional.go` | rollbackFor/noRollbackFor X.class single + {A.class,B.class} list captured as rollback_for/no_rollback_for properties; value-asserting test covers single + multi-class rollback + noRollbackFor |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_aop.go` | Spring AOP: @Aspect classes, @Before/@After/@Around/@AfterReturning/@AfterThrowing advice methods extracted as SCOPE.Pattern(subtype=advice) with advice_type+pointcut_expression+aspect properties; OWNS edge from aspect; REFERENCES edge for named pointcuts; all 5 advice types covered; value-asserting tests in TestSpringAOP_AdviceAttribution_Issue3004 |
| Aspect extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_aop.go` | @Aspect-annotated classes detected as SCOPE.Pattern(subtype=aspect) with kind=aspect + framework properties; Spring Boot + Spring WebFlux gated; value-asserting tests in TestSpringAOP_Fixture_Issue3004 |
| Pointcut resolution | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_aop.go` | @Pointcut declarations detected as SCOPE.Pattern(subtype=pointcut) with pointcut_expression; named pointcut references resolved via REFERENCES edges from advice; inline AspectJ execution() excluded from named-reference resolution; value-asserting tests |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | SLF4J @Slf4j logger + LoggerFactory.getLogger, Log4j LogManager.getLogger, JUL Logger.getLogger; log statement calls (log.info/debug/warn/error/trace) per call site; library + framework + log_level properties; value-asserting tests TestObservability_Slf4jLogging_Issue3006 + TestObservability_LoggerFactoryVariants_Issue3006 |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | Micrometer Counter/Timer/Gauge/DistributionSummary.builder(), MeterRegistry usage, @Timed annotation; MicroProfile @Counted/@Metered/@Gauge; metric_type + metric_name + library + framework properties; value-asserting tests TestObservability_MicrometerMetrics_Issue3006 + TestObservability_MicroProfileMetrics_Issue3006 |
| Trace extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | OTel @WithSpan + tracer.spanBuilder(); Micrometer Tracing @Observed + tracer.nextSpan(); span_kind (annotation/programmatic) + span_name + library + framework; value-asserting tests TestObservability_OtelTracing_Issue3006 + TestObservability_MicrometerTracing_Issue3006 |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Substrate passes emit per-binding/per-finding confidence (Binding.Confidence, TaintMatch.Confidence, EffectMatch.Confidence); constant_propagation stamps substrate_confidence property on entities. Top-level EntityRecord.Confidence field not yet stamped by the Java extractor directly; confidence data not exposed via MCP min_confidence filtering. Full requires a confidence-scoring pass that writes EntityRecord.Confidence. |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ✅ `full` | `2026-05-29` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | Covers JPA/Hibernate (EntityManager.find/persist/merge/remove/flush), Spring Data JPA (JpaRepository.findById/findAll/findBy*/save/saveAll/delete/count/exists), JdbcTemplate/NamedJdbcTemplate (queryFor*/query/queryForList/update/batchUpdate/execute), raw Statement (executeQuery/executeUpdate), Spring Data MongoDB (findBy* patterns). Read and write sides both covered with per-match confidence. Dominant Spring Boot DB access patterns are fully represented. |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-28` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-28` | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |

## Framework-specific

### Spring Boot Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Actuator detection | 🟢 `partial` | `2026-05-29` | 3081 | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_boot.go` | — |
| Autoconfiguration detection | 🟢 `partial` | `2026-05-29` | 3081 | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_ecosystem.go` | — |
| Profile detection | 🟢 `partial` | `2026-05-29` | 3081 | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_ecosystem.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.spring-boot ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
