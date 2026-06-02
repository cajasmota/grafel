<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.spring-boot` — Spring Boot / Spring MVC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 58

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_deprecation.go`<br>`internal/engine/http_endpoint_deprecation_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628 epic: api_version + deprecation on http_endpoint_definition. Java/Spring: @Deprecated on @GetMapping/@RequestMapping handler (since= attr + @deprecated Javadoc replacement); non-route @Deprecated does not leak. |
| Endpoint pagination posture | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: paginated/pagination_style(offset|page|cursor)/pagination_params/pagination_source on http_endpoint_definition via applyEndpointPagination. Direct signals: DRF pagination_class + DEFAULT_PAGINATION_CLASS, Django Paginator, FastAPI/fastapi-pagination, Spring Pageable/Page<>, Express req.query, Sequelize/Prisma take/skip/.cursor(). Honest-partial: lone limit not stamped. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | 3818 | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3818: response_codes + success_code + response_codes_source via applyEndpointResponseCodes. Spring signals: @ResponseStatus(HttpStatus.X|NNN), ResponseEntity.status(HttpStatus.X|NNN), ResponseEntity factory helpers (ok->200/created->201/accepted->202/noContent->204/notFound->404/badRequest->400/unprocessableEntity->422/internalServerError->500), and ResponseStatusException(HttpStatus.X). Honest-partial: dynamic status skipped. |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/java/frameworks/spring_boot.yaml`<br>`internal/engine/rules/java/frameworks/spring_mvc.yaml`<br>`internal/engine/spring_routes.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/spring_routes.go` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go`<br>`internal/engine/spring_routes.go` | Annotation-driven route composition scanned (@RequestMapping/@GetMapping/etc.); path-variable expression resolution not implemented |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/template_render.go`<br>`internal/extractors/java/template_render.go`<br>`internal/extractors/java/template_render_test.go` | @Controller method String/ModelAndView view name -> RENDERS SCOPE.Template; @RestController/@ResponseBody skipped (REST not MVC); dynamic names dropped (#3628) |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/java_auth_policy.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_request_response.go` | spring_request_response.go emits traversable endpoint→DTO edges: a @GetMapping/@PostMapping/... controller method carries ACCEPTS_INPUT → @RequestBody parameter type and RETURNS → method return type (ResponseEntity<T>/Mono<T>/Flux<T>/Optional<T> unwrapped via unwrapReturnType), SourceRef = the controller-method endpoint, TargetRef = the DTO schema. Generic collections (List/Map/Set) and framework types skipped via srrSkipTypes. This was the #3589 template the FastAPI/Spring parity work builds on — already present, re-verified during the #3607 multi-stack DTO-edge restoration (NestJS/Express/Go). Value-asserting tests: @PostMapping ResponseEntity<OrderDTO> create(@RequestBody OrderDTO dto) → ACCEPTS_INPUT + RETURNS assertions in extractors_test.go. |
| Request validation | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_params.go` | Bean Validation annotations (@NotNull, @NotBlank, @NotEmpty, @Valid, @Min, @Max, @Size, @Pattern, @Email) captured per handler parameter; required flag set; no field-level constraint recursion |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-06-02` | — | `internal/engine/http_endpoint_java_middleware.go`<br>`internal/engine/java_annotation_params.go` | @Bean SecurityFilterChain + OncePerRequestFilter/HandlerInterceptor/WebMvcConfigurer.addInterceptors/WebFilter captured by name. #3628: applyJavaMiddlewareCoverage now BINDS an ordered middleware_chain to Spring ENDPOINTS — Servlet FilterRegistrationBean urlPatterns (outermost, filter scope) + HandlerInterceptor addPathPatterns/excludePathPatterns (interceptor scope) statically matched against each route path and stamped per the cross-stack {name,expr,scope,order,auth_kind?} contract (auth filters/interceptors tagged auth_kind). Honest-partial: only path patterns that statically resolve to a known same-file route bind; broad /** includes and filters without resolvable urlPatterns are skipped (no fabricated order). Tests: TestMiddleware_SpringInterceptorPathMatch, TestMiddleware_SpringFilterAndInterceptorOrder, TestMiddleware_SpringExcludePathPatterns, TestMiddleware_SpringFilterNoUrlPatternsSkipped. |
| Rate limit stamping | 🟢 `partial` | `2026-06-02` | — | `internal/engine/http_endpoint_java_ratelimit.go`<br>`internal/engine/http_endpoint_java_ratelimit_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Resilience4j @RateLimiter(name=...) / bucket4j @RateLimiting(capacity=N) method annotations (matched by mapping path) and Spring Cloud Gateway RequestRateLimiter filters (matched by Path= predicate, replenishRate→rate) stamp rate_limited/rate_limit/rate_limit_scope(route|gateway)/rate_limit_source on the endpoint op. Bare Resilience4j @RateLimiter rate lives in config → honest-partial (rate omitted). Negative: a non-throttle annotation does not stamp. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-02` | — | `internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_java_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | Java JUnit (4/5) deep test->SUT linkage via the shared cross/testmap extractor (#3855), same path that credits Kotlin JVM (#3437). detectJUnit fires on @Test/@ParameterizedTest/@RepeatedTest in *Test.java/*Tests.java/*IT.java (org.junit/junit.jupiter import hints); resolver emits high-confidence TESTS edges for direct SUT calls (new UserService(); userService.create()), medium for class-name subject (UserServiceTest->UserService) when the body has no prod call, and suppresses MockMvc/REST-assured/WebTestClient/AssertJ/Hamcrest/Mockito test-harness noise. Value-asserted in frameworks_java_test.go (TestJUnit_DirectCall_HighConfidence/_MethodCallOnInjectedSUT/_ClassNameSubject/_ParameterizedTest/_MockMvc_NoHTTPClientNoise/_RestAssured_NoDSLNoise). Scope: unit-level test->SUT; framework-handler attribution from HTTP integration tests (MockMvc/REST-assured -> controller endpoint) is out of scope. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-02` | — | `internal/extractor/enum_valueset.go`<br>`internal/extractors/java/enum_valueset.go`<br>`internal/extractors/java/java.go` | enum_declaration -> SCOPE.Component/enum AND a value-carrying SCOPE.Enum value-set node (buildJavaEnumValueSet, kind_hint=java_enum): bare constants recorded as members (Status: ACTIVE, INACTIVE), constructor-arg enums capture the first literal argument as the member value (Color: RED=#f00; Planet: MERCURY=1). Non-literal constructor args recorded value-less (honest-partial). |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |
| Type alias extraction | — `not_applicable` | — | — | — | Java has no type alias syntax |
| Type extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/custom/java/spring_boot.go` | #3699: ExtractSpringDIGraph emits the DI binding GRAPH: @Bean factory methods in @Configuration classes emit BINDS (return-type token -> the @Bean method, lifetime from @Scope). Value-asserted in di_graph_test.go TestSpringDI_BeanMethod_Binds (DataSource BINDS dataSource, lifetime=singleton). Plus prior stereotype-bean SCOPE.Component emission (spring_boot.go via #3586). |
| DI injection point | ✅ `full` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go`<br>`internal/custom/java/spring_di_deepen.go` | #3699: ExtractSpringDIGraph emits the injection GRAPH: constructor params and @Autowired/@Inject fields of @Service/@Repository/@Component/@Controller beans emit INJECTED_INTO (provider type -> consumer class), @Qualifier on the edge, primitives rejected. Value-asserted in di_graph_test.go (UserRepo INJECTED_INTO UserService via=spring_constructor; DataSource INJECTED_INTO OrderRepo via=spring_field qualifier=primaryDataSource; negative: int param yields no edge). Prior @Autowired/@Value/@Qualifier DEPENDS_ON remain (spring_di_deepen.go). |
| DI scope resolution | ✅ `full` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/spring_di_deepen.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3699: @Bean BINDS edges carry lifetime from @Scope (default singleton). Plus prior @Scope/@RequestScope/@ConditionalOnMissingBean handling (spring_di_deepen.go). |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/transactional.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | @Transactional on class/method detected; class-level boundary + method-level SCOPE.Pattern(subtype=transaction_boundary) with declaring_class, framework; OWNS edges from class to method boundaries; Jakarta/JTA TxType positional form handled; value-asserting fixture TestTransactional_Boundary_Propagation_Rollback_Issue3003 |
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/java/java.go`<br>`internal/extractors/java/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: @Transactional (Spring + Jakarta/JTA) on a method stamps transactional=true + tx_propagation/tx_isolation/tx_read_only on that method entity; class-level @Transactional propagates to all enclosing methods (method-level annotation wins on specificity). No transitive propagation across calls. |
| Transaction propagation | ✅ `full` | `2026-06-01` | — | `internal/custom/java/transactional.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | propagation=Propagation.<MODE> (Spring) and TxType.<MODE> (JTA) extracted into propagation property; isolation + readOnly also captured; all Spring propagation modes + JTA TxType modes covered; value-asserting test TestTransactional_Boundary_Propagation_Rollback_Issue3003 |
| Transaction rollback rules | ✅ `full` | `2026-06-01` | — | `internal/custom/java/transactional.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | rollbackFor/noRollbackFor X.class single + {A.class,B.class} list captured as rollback_for/no_rollback_for properties; value-asserting test covers single + multi-class rollback + noRollbackFor |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | ✅ `full` | `2026-06-01` | — | `internal/custom/java/spring_aop.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Spring AOP: @Aspect classes, @Before/@After/@Around/@AfterReturning/@AfterThrowing advice methods extracted as SCOPE.Pattern(subtype=advice) with advice_type+pointcut_expression+aspect properties; OWNS edge from aspect; REFERENCES edge for named pointcuts; all 5 advice types covered; value-asserting tests in TestSpringAOP_AdviceAttribution_Issue3004 |
| Aspect extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/spring_aop.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | @Aspect-annotated classes detected as SCOPE.Pattern(subtype=aspect) with kind=aspect + framework properties; Spring Boot + Spring WebFlux gated; value-asserting tests in TestSpringAOP_Fixture_Issue3004 |
| Pointcut resolution | ✅ `full` | `2026-06-01` | — | `internal/custom/java/spring_aop.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | @Pointcut declarations detected as SCOPE.Pattern(subtype=pointcut) with pointcut_expression; named pointcut references resolved via REFERENCES edges from advice; inline AspectJ execution() excluded from named-reference resolution; value-asserting tests |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | SLF4J @Slf4j logger + LoggerFactory.getLogger, Log4j LogManager.getLogger, JUL Logger.getLogger; log statement calls (log.info/debug/warn/error/trace) per call site; library + framework + log_level properties; value-asserting tests TestObservability_Slf4jLogging_Issue3006 + TestObservability_LoggerFactoryVariants_Issue3006 |
| Metric extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | Micrometer Counter/Timer/Gauge/DistributionSummary.builder(), MeterRegistry usage, @Timed annotation; MicroProfile @Counted/@Metered/@Gauge; metric_type + metric_name + library + framework properties; value-asserting tests TestObservability_MicrometerMetrics_Issue3006 + TestObservability_MicroProfileMetrics_Issue3006 |
| Trace extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | OTel @WithSpan + tracer.spanBuilder(); Micrometer Tracing @Observed + tracer.nextSpan(); span_kind (annotation/programmatic) + span_name + library + framework; value-asserting tests TestObservability_OtelTracing_Issue3006 + TestObservability_MicrometerTracing_Issue3006 |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Substrate passes emit per-binding/per-finding confidence (Binding.Confidence, TaintMatch.Confidence, EffectMatch.Confidence); constant_propagation stamps substrate_confidence property on entities. Top-level EntityRecord.Confidence field not yet stamped by the Java extractor directly; confidence data not exposed via MCP min_confidence filtering. Full requires a confidence-scoring pass that writes EntityRecord.Confidence. |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/java/config_consumer.go`<br>`internal/extractors/java/config_consumer_test.go` | @Value, @ConfigurationProperties, env.getProperty, @ConfigProperty -> config:<key> (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ✅ `full` | `2026-05-29` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | Covers JPA/Hibernate (EntityManager.find/persist/merge/remove/flush), Spring Data JPA (JpaRepository.findById/findAll/findBy*/save/saveAll/delete/count/exists), JdbcTemplate/NamedJdbcTemplate (queryFor*/query/queryForList/update/batchUpdate/execute), raw Statement (executeQuery/executeUpdate), Spring Data MongoDB (findBy* patterns). Read and write sides both covered with per-match confidence. Dominant Spring Boot DB access patterns are fully represented. |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-28` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | ✅ `full` | `2026-06-02` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY edge (LaunchDarkly/Unleash/Unleash-React/OpenFeature/Flipper/Flagsmith/Split.io/generic) |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3958 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_java.go`<br>`internal/substrate/dataflow_java_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22, epic #3872): new Java sniffer (internal/substrate/dataflow_java.go) registered on the "java" slug and dispatched by file extension through LanguageForPath (internal/links/dataflow_pass.go), mirroring the python/jsts/go/ruby sniffers. Sources: Spring MVC/WebFlux controller-method params annotated @RequestBody/@RequestParam/@PathVariable/@RequestHeader/@RequestPart/@ModelAttribute/@CookieValue — each bound param is a request-derived root; field = annotation literal (@RequestParam("q")->q), else param name for scalar binders, else "" for @RequestBody whole-object (recovered from dto.getEmail()->email getter or dto.email member). Intra-method typed-decl + reassignment taint tracking + multi-hop (<=DataFlowMaxHops=3) local same-file call propagation by exact positional index, AND cross-file boundary emission continued by the links pass (continueDataFlowJava). Sinks: JPA/Spring Data/JDBC write (repo.save/saveAll/delete*/insert, entityManager.persist/merge/remove, jdbcTemplate.update/batchUpdate/execute), response (ResponseEntity.ok/status().body/new ResponseEntity, ServerResponse.bodyValue, return <tainted>), outbound HTTP (restTemplate.postForObject/exchange, webClient.bodyValue). HONEST-PARTIAL: drops static/constant values, non-request params (@Autowired), reassignment, embedded-arg expressions, varargs, recursion/cycle, the 4th hop, external/unresolved imports; whole-object @RequestBody flows with field="". DEPLOY-DEFERRED (daemon not rebuilt). PHP request_sink_dataflow remains the last follow-up. |
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
