<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.helidon` — Helidon

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_deprecation.go`<br>`internal/engine/http_endpoint_jaxrs_posture_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3857: deprecated(+deprecated_since)+api_version on JAX-RS endpoints. @Deprecated(since=) on a @GET/@Path handler -> deprecated=true (shared javaDeprecationVerdict); api_version path-derived (@Path(/api/v2/..)). Value-asserted TestDeprecation_JAXRS_DeprecatedMethod + TestAPIVersion_JAXRS_PathV2; negative TestDeprecation_JAXRS_NonRouteDeprecatedUnaffected. |
| Endpoint pagination posture | 🟢 `partial` | `2026-06-02` | 3857 | `internal/engine/http_endpoint_jaxrs_posture.go`<br>`internal/engine/http_endpoint_jaxrs_posture_test.go`<br>`internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3857: paginated/pagination_style/pagination_params/pagination_source via jaxrsPaginationVerdict in the java branch of applyEndpointPagination. Signals: JAX-RS @QueryParam (Micronaut @QueryValue) limit+offset/page/cursor pairs classified by the shared vocabulary; Micronaut Data Pageable param / Page<>|Slice<> return -> page. Value-asserted TestPagination_JAXRS_LimitOffsetQueryParams + negative TestPagination_JAXRS_LoneLimitNotStamped. Partial: param-shape only, no framework pagination-class signal. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_jaxrs_posture.go`<br>`internal/engine/http_endpoint_jaxrs_posture_test.go`<br>`internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3857: response_codes/success_code/response_codes_source on JAX-RS-synthesised endpoints (synthesizeJAXRS) via jaxrsResponseCodes merged into the java branch of applyEndpointResponseCodes. Signals: Response.status(NNN|Response.Status.X), Response/HttpResponse builder helpers (ok->200/created->201/accepted->202/noContent->204/notFound->404/badRequest->400/unprocessableEntity->422/serverError->500), new WebApplicationException(NNN|Status.X), typed jakarta.ws.rs exceptions (NotFoundException->404 etc), Micronaut @Status. Value-asserted in TestResponseCodes_JAXRS_*. Honest-partial: dynamic status skipped. |
| Endpoint synthesis | 🟢 `partial` | `2026-05-29` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/microprofile.yaml` | MicroProfile JAX-RS @Path + verb annotations covered by java_annotation_routes.go; partial (no class-level @Path composition with vert.x-style mounts) |
| Handler attribution | 🟢 `partial` | `2026-05-29` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/microprofile.yaml` | JAX-RS method-level handler attribution via SCOPE.Operation entity; same pass as Quarkus/Jakarta EE |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/microprofile.yaml` | JAX-RS @Path annotation route extraction; class+method composition; MicroProfile flavor same as Quarkus |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3088) | `internal/engine/java_auth_policy.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/jakarta_jaxrs_dto.go` | — |
| Request validation | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/jakarta_jaxrs_dto.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/helidon_filters.go` | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-02` | — | `internal/custom/java/issue4390_livedrepro_test.go`<br>`internal/custom/java/issue4390_sut_disambiguation_test.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_java_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | Java JUnit (4/5) deep test->SUT linkage via the shared cross/testmap extractor (#3855), same path that credits Kotlin JVM (#3437). detectJUnit fires on @Test/@ParameterizedTest/@RepeatedTest in *Test.java/*Tests.java/*IT.java (org.junit/junit.jupiter import hints); resolver emits high-confidence TESTS edges for direct SUT calls (new UserService(); userService.create()), medium for class-name subject (UserServiceTest->UserService) when the body has no prod call, and suppresses MockMvc/REST-assured/WebTestClient/AssertJ/Hamcrest/Mockito test-harness noise. Value-asserted in frameworks_java_test.go (TestJUnit_DirectCall_HighConfidence/_MethodCallOnInjectedSUT/_ClassNameSubject/_ParameterizedTest/_MockMvc_NoHTTPClientNoise/_RestAssured_NoDSLNoise). Scope: unit-level test->SUT; framework-handler attribution from HTTP integration tests (MockMvc/REST-assured -> controller endpoint) is out of scope. SUT disambiguation when a test class injects MULTIPLE candidate fields (#4390, extending #4359/#4615): junit5.go resolveJavaTestSubjectDetail picks the ONE system-under-test by priority @InjectMocks (Mockito's explicit SUT marker, overrides stem) then stem-match (OrderServiceTest->OrderService against the injected/constructed non-mock field-type set) then single non-mock injected field then none (ambiguous equals -> no edge); @Mock/@MockBean/@Spy/@SpyBean collaborator types are excluded at every tier so a stubbed collaborator is never linked even when its name matches the test-class stem. Fixtures #4390: internal/custom/java/issue4390_sut_disambiguation_test.go, internal/custom/java/issue4390_livedrepro_test.go. |

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
| DI binding extraction | 🟢 `partial` | `2026-06-12` | [link](https://github.com/cajasmota/archigraph/issues/3589) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/jakarta_ee.go`<br>`internal/custom/java/jakarta_ee_advanced.go`<br>`internal/custom/java/java_di_scope_deepen.go`<br>`internal/custom/java/patterns_dispatch.go` | #2996/#3083: CDI di_binding via @Produces producer methods (ExtractJakartaEEAdvanced, jakartaEEAdvFrameworks-gated) -> producer SCOPE.Operation + producer-type binding; same extractor that credits the jaxrs record full. Honest-partial: regex/file-local, no cross-file/@Qualifier-disambiguated binding resolution. Tested TestJakartaEEAdv_CDIProducer. |
| DI injection point | 🟢 `partial` | `2026-06-12` | [link](https://github.com/cajasmota/archigraph/issues/3589) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/jakarta_ee.go`<br>`internal/custom/java/jakarta_ee_advanced.go`<br>`internal/custom/java/java_di_scope_deepen.go`<br>`internal/custom/java/patterns_dispatch.go` | #2996/#3083/#2996: di_injection_point via @Inject (CDI), @EJB and @Resource field injection (jakarta_ee.go) -> DEPENDS_ON (kind=ejb_inject/cdi_inject); ExtractJakartaEE + ExtractJakartaEEAdvanced. Honest-partial: regex/file-local, target type resolved by name. Tested TestJakartaEE_EJB. |
| DI scope resolution | 🟢 `partial` | `2026-06-12` | [link](https://github.com/cajasmota/archigraph/issues/3589) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/jakarta_ee.go`<br>`internal/custom/java/jakarta_ee_advanced.go`<br>`internal/custom/java/java_di_scope_deepen.go`<br>`internal/custom/java/patterns_dispatch.go` | #2996/#3083: di_scope_resolution via CDI scope annotations @ApplicationScoped/@RequestScoped/@SessionScoped/@Dependent/@ConversationScoped (jakarta_ee_advanced.go + java_di_scope_deepen.go) -> scoped SCOPE.Component carrying scope + framework. Honest-partial: file-local annotation scan. Tested TestQuarkus_CDIScopeResolution-style cases. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863: @Transactional class/method boundaries (Spring + Jakarta/JTA); SCOPE.Pattern(subtype=transaction_boundary) with declaring_class + OWNS edge; framework in txFrameworks. Net-new programmatic boundary (UserTransaction/Hibernate session/JPA EntityTransaction) also detected. |
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/java/java.go`<br>`internal/extractors/java/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3863: @Transactional (Spring + Jakarta/JTA) on a method stamps transactional=true + tx_propagation/tx_isolation/tx_read_only via txscope.DetectJava (framework-agnostic). |
| Transaction propagation | ✅ `full` | `2026-06-02` | — | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863: propagation=Propagation.<MODE> (Spring) + TxType.<MODE> (JTA positional) captured; isolation + readOnly too; framework in txFrameworks. |
| Transaction rollback rules | ✅ `full` | `2026-06-02` | — | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863: rollbackFor/noRollbackFor (Spring) AND rollbackOn/dontRollbackOn (Jakarta/JTA) folded into rollback_for/no_rollback_for; programmatic setRollbackOnly()/tx.rollback() also marked. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3088) | — | Helidon SE has no AOP model; Helidon MP CDI interceptors deferred to FW-T-04 (CDI interceptor ticket) |
| Aspect extraction | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3088) | — | Helidon SE has no AOP model; Helidon MP CDI interceptors deferred to FW-T-04 (CDI interceptor ticket) |
| Pointcut resolution | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3088) | — | Helidon SE has no AOP model; Helidon MP CDI interceptors deferred to FW-T-04 (CDI interceptor ticket) |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-06-12` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go`<br>`internal/custom/java/patterns_dispatch.go` | #3006 (epic #2847): log_extraction via ExtractObservability (obsFrameworks-gated). @Slf4j / LoggerFactory.getLogger / LogManager.getLogger / Logger.getLogger -> SCOPE.Pattern(subtype=logger); log.<trace|debug|info|warn|error|fatal>(...) call surface -> SCOPE.Pattern(subtype=log_statement) carrying log_level + framework. Honest-partial: regex/file-local, no cross-file logger correlation. Tested TestObservability_Slf4jLogging_Issue3006 / _LoggerFactoryVariants_Issue3006 / _FrameworkGating_Issue3006. |
| Metric extraction | 🟢 `partial` | `2026-06-12` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go`<br>`internal/custom/java/patterns_dispatch.go` | #3006 (epic #2847): metric_extraction via ExtractObservability. Micrometer (@Timed, Counter/Timer/Gauge.builder, MeterRegistry) + MicroProfile Metrics (@Counted/@Timed/@Metered/@Gauge) -> SCOPE.Pattern(subtype=metric) carrying metric_type + framework. Honest-partial: regex/file-local, non-literal metric-name expressions unresolved. Tested TestObservability_MicrometerMetrics_Issue3006 / _MicroProfileMetrics_Issue3006. |
| Trace extraction | 🟢 `partial` | `2026-06-12` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go`<br>`internal/custom/java/patterns_dispatch.go` | #3006 (epic #2847): trace_extraction via ExtractObservability. OpenTelemetry (@WithSpan, tracer.spanBuilder, span.setAttribute) + Micrometer Tracing (@Observed, Tracer.nextSpan) -> SCOPE.Pattern(subtype=trace_span) carrying span_kind + framework. Honest-partial: regex/file-local, non-literal span-name expressions unresolved. Tested TestObservability_OtelTracing_Issue3006 / _MicrometerTracing_Issue3006. |

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
| Dead code detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic Java engine pass, fires regardless of framework). Java idioms attribute to the enclosing method: LD camelCase boolVariation/stringVariation, Unleash isEnabled, OpenFeature getBooleanValue, FF4j ff4j.check. Honest-partial: Togglz enum keys + dynamic keys miss (no literal). |
| Fs effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.helidon ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
