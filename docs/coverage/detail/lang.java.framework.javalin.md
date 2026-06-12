<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.javalin` — Javalin

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_deprecation.go`<br>`internal/engine/http_endpoint_reactive_posture.go`<br>`internal/engine/http_endpoint_reactive_posture_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3858: deprecated/deprecation_source + path-derived api_version on Javalin endpoints. @OpenApi(deprecated = true) -> deprecated=true via reactiveDeprecationVerdict (merged into the java branch of resolveEndpointDeprecation); standard @Deprecated also credits via shared javaDeprecationVerdict; api_version from path-prefix versioning (/v2/..). Value-asserted TestDeprecation_Javalin_OpenApiDeprecated (+ negative non-deprecated @OpenApi) and TestAPIVersion_Javalin_PathPrefixV2. |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_reactive_posture.go`<br>`internal/engine/http_endpoint_reactive_posture_test.go`<br>`internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3858: response_codes/success_code/response_codes_source on Javalin endpoints via javalinResponseCodes (reactiveResponseCodes -> java branch of applyEndpointResponseCodes). Signals: ctx.status(NNN), ctx.status(HttpStatus.X) (shared enum), @OpenApi @OpenApiResponse(status). Value-asserted TestResponseCodes_Javalin_CtxStatusNumeric/_CtxStatusEnum/_OpenApiResponse; negative _DynamicStatusNotStamped (dynamic ctx.status(var) skipped, honest-partial). |
| Endpoint synthesis | 🟢 `partial` | — | 3085 | `internal/engine/http_endpoint_synthesis.go` | — |
| Handler attribution | ✅ `full` | `2026-06-01` | — | `internal/custom/java/javalin_routes.go` | — |
| Route extraction | 🟢 `partial` | — | 3085 | `internal/engine/http_endpoint_synthesis.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3862) | `internal/custom/java/framework_auth.go`<br>`internal/custom/java/framework_auth_test.go`<br>`internal/custom/java/javalin_routes.go` | #3862: framework_auth.go stamps the flat Spring-compatible auth contract (auth_required/auth_method/auth_confidence/auth_guard/auth_roles) directly on Javalin route entities. Per-route roles(Role.ADMIN) inline guard → auth_required=true + auth_roles (high confidence); app/config.accessManager wired → every route auth_required (low confidence). Value-asserting tests: roles(Role.ADMIN) on /admin → auth_required=true auth_roles=ADMIN; roles(Role.ADMIN, Role.STAFF) → ADMIN,STAFF; unprotected route with no accessManager → auth_required absent. Honest-partial: bare app.get(path, handler) is unprotected. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/javalin_routes.go` | — |
| Request validation | ✅ `full` | `2026-06-01` | — | `internal/custom/java/javalin_routes.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-06-03` | 3859 | `internal/engine/http_endpoint_java_middleware.go`<br>`internal/engine/http_endpoint_java_middleware_test.go`<br>`internal/engine/http_endpoint_java_middleware_xframework.go`<br>`internal/engine/http_endpoint_middleware_chain.go` | #3859: Javalin app.before("/glob", h)/after(h)/beforeMatched handlers bound to endpoints via indexJavalinFilters — a path-glob before("/api/*") binds the routes matching the glob (javalinGlobMatches) and a bare before/after binds every route in the file; stamped on the cross-stack endpoint contract (middleware_chain/middleware_count/middleware_names/middleware_scope=filter) by applyJavaMiddlewareCoverage. File-signal gated on javalin/Javalin. Value-asserted TestMiddleware_JavalinBeforeAfter (before(/api/*)+after bound to /api/ping; before(/admin/*) NOT bound — negative). |
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
| DI binding extraction | — `not_applicable` | — | 3085 | — | Javalin is a micro-framework with no built-in DI container; dependency injection is not part of the framework and must be provided externally (e.g. Guice, Koin). DI binding is therefore not_applicable for lang.java.framework.javalin. |
| DI injection point | — `not_applicable` | — | 3085 | — | Javalin has no built-in DI container; @Inject is not a Javalin concept. DI injection points are not_applicable. |
| DI scope resolution | — `not_applicable` | — | 3085 | — | Javalin has no DI scopes (@Singleton, @RequestScoped, etc.); scope resolution is not_applicable. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3863) | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863 (partial): programmatic transaction boundary detected — UserTransaction.begin()/commit(), Hibernate session.beginTransaction(), JPA em.getTransaction().begin() in a method body emit a SCOPE.Pattern(subtype=transaction_boundary, transaction_boundary=programmatic, tx_api=...). No @Transactional annotation surface for this framework. Honest-partial: boundary credited only where a begin/open call is lexically present. |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | — `not_applicable` | — | 3085 | — | Javalin has no transaction propagation model; not_applicable. |
| Transaction rollback rules | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3863) | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863 (partial): programmatic rollback detected — setRollbackOnly() / tx.rollback() / userTransaction.rollback() mark rollback=programmatic on the method. No declarative rollbackFor/rollbackOn surface for this framework. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | — `not_applicable` | — | 3085 | — | Javalin has no AOP support; @Aspect / @Before / @Around are Spring/AspectJ concepts not present in Javalin. Advice attribution is not_applicable. |
| Aspect extraction | — `not_applicable` | — | 3085 | — | Javalin has no AOP aspect system; aspect extraction is not_applicable. |
| Pointcut resolution | — `not_applicable` | — | 3085 | — | Javalin has no AOP pointcut system; pointcut resolution is not_applicable. |

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
(or use `go run ./tools/coverage update lang.java.framework.javalin ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
