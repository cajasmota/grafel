<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.jaxrs` — JAX-RS / Jakarta REST

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
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/rules/java/frameworks/jakarta_ee.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/java_annotation_routes.go` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

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
| Middleware coverage | ✅ `full` | `2026-06-03` | — | `internal/custom/java/jaxrs_filters.go`<br>`internal/engine/http_endpoint_java_middleware.go`<br>`internal/engine/http_endpoint_java_middleware_test.go`<br>`internal/engine/http_endpoint_java_middleware_xframework.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3859: in addition to the parallel-node jaxrs_filters extractor, a JAX-RS '@Provider class X implements ContainerRequestFilter/ContainerResponseFilter' is now also bound as a GLOBAL outermost filter on the cross-stack ENDPOINT contract (middleware_chain/middleware_count/middleware_names/middleware_scope=filter) via indexJaxrsProviderFilters + applyJavaMiddlewareCoverage, so JAX-RS middleware is queryable uniformly with the other stacks. Value-asserted TestMiddleware_JaxRsProviderFilter (AuthRequestFilter bound, auth_kind=auth). Honest-partial: a @NameBinding-restricted filter is NOT globally bound (TestMiddleware_JaxRsNameBindingSkipped). |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/java/issue4390_livedrepro_test.go`<br>`internal/custom/java/issue4390_sut_disambiguation_test.go`<br>`internal/custom/java/junit5.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_java_test.go`<br>`internal/extractors/cross/testmap/resolver.go`<br>`internal/extractors/java/issue4682_localvar_receiver_test.go`<br>`internal/extractors/java/java.go` | Java JUnit (4/5) deep test->SUT linkage via the shared cross/testmap extractor (#3855), same path that credits Kotlin JVM (#3437). detectJUnit fires on @Test/@ParameterizedTest/@RepeatedTest in *Test.java/*Tests.java/*IT.java (org.junit/junit.jupiter import hints); resolver emits high-confidence TESTS edges for direct SUT calls (new UserService(); userService.create()), medium for class-name subject (UserServiceTest->UserService) when the body has no prod call, and suppresses MockMvc/REST-assured/WebTestClient/AssertJ/Hamcrest/Mockito test-harness noise. Value-asserted in frameworks_java_test.go (TestJUnit_DirectCall_HighConfidence/_MethodCallOnInjectedSUT/_ClassNameSubject/_ParameterizedTest/_MockMvc_NoHTTPClientNoise/_RestAssured_NoDSLNoise). Scope: unit-level test->SUT; framework-handler attribution from HTTP integration tests (MockMvc/REST-assured -> controller endpoint) is out of scope. Local-variable receiver typing for test->CALLS coverage crediting (#4682, generalising TS/JS #4680 + Python #4716): collectLocalVarTypes in internal/extractors/java/java.go now also types modern `var` locals bound from a direct `new XController(...)` initialiser (declared-type and @InjectMocks/@Autowired field forms were already typed), so `var c = new XController(svc); c.getCounts()` in a @Test resolves to XController.getCounts and ComputeCoverage credits the endpoint; factory/builder/chain receivers stay unresolved (honest exclusion). Route-hit attribution (MockMvc/WebTestClient/TestRestTemplate/REST-assured perform(get("/path")) -> http_endpoint_definition) is now IN scope via the e2e_route_calls path (junit5.go collectSpringTestRouteCalls #4370 -> engine.linkE2ERouteTestsToEndpoints). Fixtures: internal/extractors/java/issue4682_localvar_receiver_test.go. SUT disambiguation when a test class injects MULTIPLE candidate fields (#4390, extending #4359/#4615): junit5.go resolveJavaTestSubjectDetail picks the ONE system-under-test by priority @InjectMocks (Mockito's explicit SUT marker, overrides stem) then stem-match (OrderServiceTest->OrderService against the injected/constructed non-mock field-type set) then single non-mock injected field then none (ambiguous equals -> no edge); @Mock/@MockBean/@Spy/@SpyBean collaborator types are excluded at every tier so a stubbed collaborator is never linked even when its name matches the test-class stem. Fixtures #4390: internal/custom/java/issue4390_sut_disambiguation_test.go, internal/custom/java/issue4390_livedrepro_test.go. |

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
| Dead code detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
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
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3958 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_java.go`<br>`internal/substrate/dataflow_java_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22, epic #3872): new Java sniffer (internal/substrate/dataflow_java.go) registered on the "java" slug and dispatched by file extension through LanguageForPath (internal/links/dataflow_pass.go), mirroring the python/jsts/go/ruby sniffers. Sources: Spring MVC/WebFlux controller-method params annotated @RequestBody/@RequestParam/@PathVariable/@RequestHeader/@RequestPart/@ModelAttribute/@CookieValue — each bound param is a request-derived root; field = annotation literal (@RequestParam("q")->q), else param name for scalar binders, else "" for @RequestBody whole-object (recovered from dto.getEmail()->email getter or dto.email member). Intra-method typed-decl + reassignment taint tracking + multi-hop (<=DataFlowMaxHops=3) local same-file call propagation by exact positional index, AND cross-file boundary emission continued by the links pass (continueDataFlowJava). Sinks: JPA/Spring Data/JDBC write (repo.save/saveAll/delete*/insert, entityManager.persist/merge/remove, jdbcTemplate.update/batchUpdate/execute), response (ResponseEntity.ok/status().body/new ResponseEntity, ServerResponse.bodyValue, return <tainted>), outbound HTTP (restTemplate.postForObject/exchange, webClient.bodyValue). HONEST-PARTIAL: drops static/constant values, non-request params (@Autowired), reassignment, embedded-arg expressions, varargs, recursion/cycle, the 4th hop, external/unresolved imports; whole-object @RequestBody flows with field="". DEPLOY-DEFERRED (daemon not rebuilt). PHP request_sink_dataflow remains the last follow-up. |
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
