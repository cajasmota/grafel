<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.spring-boot` — Spring Boot (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-03` | 4136 | `internal/custom/kotlin/endpoint_deprecation.go`<br>`internal/custom/kotlin/endpoint_deprecation_test.go` | #4136 (epic #3628): greenfield Kotlin port. Spring-Boot-Kotlin producer endpoints are SCOPE.Operation custom-extractor entities (routing.go), so the flagship engine pass cannot reach them; this custom extractor stamps the IDENTICAL flat contract at the source. A @<Verb>Mapping handler in a @RestController/@Controller carrying @Deprecated("use /api/v2/x", ReplaceWith(...)) or a KDoc @deprecated above the mapping -> deprecated=true + deprecated_replacement (+ deprecated_since) + deprecation_source=@Deprecated|KDoc @deprecated; a response.setHeader("Sunset"/"Deprecation", ...) in the fun body -> deprecation_source=<Header> response header. api_version is path-derived from the composed class @RequestMapping + method path (/api/v{N} or /v{N} segment). Endpoint Name matches routing.go so the stamp merges onto the plain route op. Value-asserted in endpoint_deprecation_test.go (Spring annotated/Deprecation-header + versionless-negative). Honest-partial: a versionless non-deprecated handler is not stamped; a config-/variable-driven marker is not resolved. |
| Endpoint pagination posture | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: paginated/pagination_style(offset|page|cursor)/pagination_params/pagination_source on http_endpoint_definition via applyEndpointPagination. Direct signals: DRF pagination_class + DEFAULT_PAGINATION_CLASS, Django Paginator, FastAPI/fastapi-pagination, Spring Pageable/Page<>, Express req.query, Sequelize/Prisma take/skip/.cursor(). Honest-partial: lone limit not stamped. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/spring_boot_kotlin.yaml`<br>`internal/engine/spring_routes_kotlin.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/spring_routes_kotlin.go` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/routing.go`<br>`internal/custom/kotlin/routing_test.go` | — |

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
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/kotlin/spring_middleware.go` | @Bean SecurityFilterChain + OncePerRequestFilter/HandlerInterceptor/WebMvcConfigurer.addInterceptors/WebFilter captured by name — value-asserted, file-local; cross-file filter chain order is honest gap |
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | 4095 | `internal/custom/kotlin/rate_limit_endpoint.go`<br>`internal/custom/kotlin/rate_limit_endpoint_test.go` | Resilience4j @RateLimiter(name="orders") on a @GetMapping/@PostMapping/... handler inside a @RestController/@Controller stamps the composed endpoint (class @RequestMapping prefix + method path) with rate_limited/rate_limit_scope=route/rate_limit_source=@RateLimiter(<name>)/rate_limit_name. This is the KOTLIN-native .kt path (custom_java_patterns hard-skips .kt per #3584). Value-asserted in rate_limit_endpoint_test.go. Negatives: an un-annotated handler and @RateLimiter on a non-controller @Service method are not stamped. Partial (honest): the numeric limit lives in resilience4j.ratelimiter.<name> config, so the rate is omitted; bucket4j and Spring Cloud Gateway surfaces are not yet covered for Kotlin. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/kotlin/tests_route_e2e.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/rules/kotlin/test_patterns.yaml`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go`<br>`internal/extractors/kotlin/crosspath.go`<br>`internal/extractors/kotlin/tests.go` | Deep Kotlin TESTING linkage (#3437): junit5 @Test/@ParameterizedTest/@RepeatedTest + class-name subject; kotest StringSpec/FunSpec/DescribeSpec/BehaviorSpec/ShouldSpec/Spek DSL leaf cases with body call-scan; MockK mockk<T>() subject association with every{}/verify{} blocks blanked so the mocked call never leaks; Kotlin assertion/mockk stopwords (shouldBe/assertThrows/every/verify/any). Value-asserted in extractor_test.go (TestKotlin_JUnit5_*/Kotest_*/Mockk_*/Spek_*). #4687 (epic #4615 test->endpoint coverage linkage): typed-local/field receiver typing resolves c.method() to the class method (val c = XController(svc) ctor-call, val c: XController annotation, @InjectMockKs field, mockk<T>()) via a kotlin_call_type stamp so a test->CALLS->handler->endpoint credit lands; Kotest spec lambdas (class FooSpec : StringSpec({...})) get a test_scope SCOPE.Operation owner for their otherwise-orphaned CALLS; route-string hits (Spring/Ktor) — Spring MockMvc/WebTestClient + Ktor testApplication client.get("/path")/handleRequest(HttpMethod.Get,"/path") — stamp e2e_route_calls and the shared linkE2ERouteTestsToEndpoints pass emits the endpoint TESTS edge. Factory-/untyped-mockk() receivers and shape-only specs stay bare (honest exclusion). Value-asserted in issue4687_localvar_receiver_test.go, tests_route_e2e_test.go + http_endpoint_e2e_testmap_4687_test.go. |

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
| DI binding extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/spring_boot.go` | Kotlin Spring Boot @Service/@Repository/@Component stereotypes + @Bean methods; value-asserted by TestKotlinSpringBoot_DI_BindingNames_3435 (stereotype=service/repository, bean_method=passwordEncoder/config_class=AppConfig) |
| DI injection point | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/spring_boot.go` | Kotlin primary-constructor injection (class Foo @Autowired constructor(private val x: Bar)) + @Autowired lateinit var props now captured (new Kotlin-gated regexes in spring_boot.go); injected_type+injection_kind asserted by TestKotlinSpringBoot_DI_ConstructorInjection_3435 and _PrimaryCtorNoAnnotation_3435 |
| DI scope resolution | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/spring_boot.go` | @RequestScope/@Scope("prototype") on Kotlin classes; spring_scope value asserted by TestKotlinSpringBoot_DI_ScopeValues_3435 (request, prototype) |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | 🟢 `partial` | `2026-06-03` | 4014 | `internal/custom/kotlin/spring_transactions.go`<br>`internal/custom/kotlin/spring_transactions_test.go` | #4014: native LIVE Kotlin Spring @Transactional extractor (custom_kotlin_spring_transactions). The prior cite (custom/java/transactional.go) never fired on .kt — the custom_java_patterns adapter hard-skips non-java files — so spring-boot got zero credit; this fixes it on the custom_kotlin_* dispatch path. method+class boundaries named by enclosing class, framework=spring-boot, gated on a Spring/Jakarta @Transactional import. transfer/audit/reconcile/lookup boundary names asserted by TestKotlinSpringTx_Boundaries_4014; un-annotated plain() yields none. Honest-partial: regex/file-local, cross-file boundary propagation not resolved. |
| Transaction function stamping | 🟢 `partial` | `2026-06-03` | 4014 | `internal/custom/kotlin/spring_transactions.go`<br>`internal/custom/kotlin/spring_transactions_test.go` | #4014: each @Transactional fun is stamped transactional=true on the named SCOPE.Pattern boundary; non-readOnly bodies with a JPA/Spring-Data write call (save/persist/delete/insert/update/flush) also carry db_write=true, while readOnly=true methods never do (TestKotlinSpringTx_ReadOnlyAndDbWrite_4014: lookup readOnly read → no db_write; transfer/audit → db_write). Honest-partial: file-local lexical stamping, no transitive propagation into callees. |
| Transaction propagation | 🟢 `partial` | `2026-06-03` | 4014 | `internal/custom/kotlin/spring_transactions.go`<br>`internal/custom/kotlin/spring_transactions_test.go` | #4014: propagation=Propagation.<MODE> (and Jakarta TxType.<MODE>) captured on the LIVE Kotlin path; audit propagation=REQUIRES_NEW asserted by TestKotlinSpringTx_Propagation_4014, and a method with no explicit propagation gets NO fabricated default (the ktor extractor previously hardcoded REQUIRED for every @Transactional). Honest-partial: regex/file-local. |
| Transaction rollback rules | 🟢 `partial` | `2026-06-03` | 4014 | `internal/custom/kotlin/spring_transactions.go`<br>`internal/custom/kotlin/spring_transactions_test.go` | #4014: rollbackFor=[X::class]/noRollbackFor=[Y::class] (Kotlin ::class list form) + isolation=Isolation.<LEVEL> + readOnly captured on the LIVE Kotlin path; rollback_for=IOException, no_rollback_for=WarnException, isolation=SERIALIZABLE asserted by TestKotlinSpringTx_RollbackRules_4014. Honest-partial: regex/file-local. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/spring_aop.go` | @Before/@Around advice on Kotlin fun; advice_type (before/around) + method names asserted by TestKotlinSpringAOP_Attributes_3435 |
| Aspect extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/spring_aop.go` | @Aspect on Kotlin class; aspect name=LoggingAspect asserted by TestKotlinSpringAOP_Attributes_3435 |
| Pointcut resolution | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/spring_aop.go` | @Pointcut expression + advice->named-pointcut REFERENCES edge asserted by TestKotlinSpringAOP_Attributes_3435 (execution(* com.example.service.*.*(..))) |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/observability.go` | java extractor language-gated to kotlin; @Slf4j and SLF4J logger on Kotlin proven by TestKotlinObservability_Slf4j_Issue3274 |
| Metric extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/observability.go` | java extractor language-gated to kotlin; @Timed Micrometer on Kotlin proven by TestKotlinObservability_Micrometer_Issue3274 |
| Trace extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/kotlin_port_test.go`<br>`internal/custom/java/observability.go` | java extractor language-gated to kotlin; @WithSpan OTel on Kotlin proven by TestKotlinObservability_OTel_Issue3274 |

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
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_kotlin.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow_test.go` | throw X() -> THROWS; try/catch (e: X) -> CATCHES; @ExceptionHandler(X::class) (@ControllerAdvice) + Ktor StatusPages exception<X> -> CATCHES; converges on shared exception node (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-11` | 4700 | `internal/external/synth.go`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.spring-boot ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
