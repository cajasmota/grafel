<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.quarkus` — Quarkus (Kotlin)

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
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/quarkus_kotlin.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/quarkus_kotlin.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/routing.go`<br>`internal/custom/kotlin/routing_test.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | — | `internal/custom/kotlin/micronaut_quarkus.go` | @RolesAllowed, @PermitAll, @DenyAll (JAX-RS), @Authenticated (SmallRye JWT) — Kotlin Quarkus annotation-based security |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | — | — | `internal/custom/kotlin/micronaut_quarkus.go` | @Provider ContainerRequestFilter / ContainerResponseFilter implementation detection — file-local |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
| DI binding extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/micronaut_quarkus.go` | CDI scope annotations (@ApplicationScoped, @RequestScoped, @Singleton, @Dependent) and @Produces method detection — file-local |
| DI injection point | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/micronaut_quarkus.go` | @Inject property and lateinit var injection detection — file-local |
| DI scope resolution | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/micronaut_quarkus.go` | CDI @ApplicationScoped/@RequestScoped/@Singleton/@Dependent scope extraction — file-local |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | 🟢 `partial` | `2026-06-03` | 4016 | `internal/custom/kotlin/micronaut_quarkus_transactions_test.go`<br>`internal/custom/kotlin/spring_transactions.go` | #4016: native LIVE Kotlin @Transactional extractor (custom_kotlin_spring_transactions) resolves framework from the import set (io.quarkus.* -> quarkus) so the shared jakarta JTA @Transactional is credited to quarkus, AND credits the Quarkus reactive code boundary Panache.withTransaction { } / Panache.withSession { } (import-gated on a Quarkus/Panache import). The prior cite (custom/java/transactional.go) never fired on .kt — custom_java_patterns hard-skips non-java files. OrderService.create/risky method boundaries (framework=quarkus) asserted by TestKotlinQkTx_FrameworkAttribution_4016; the Panache.withTransaction code boundary (transaction_boundary=code, tx_api=panache_withTransaction) by TestKotlinQkTx_PanacheBoundary_4016; un-annotated untracked() yields none; a non-Quarkus Panache symbol is NOT claimed (TestKotlinQkTx_PanacheGatedOnQuarkus_4016). Honest-partial: regex/file-local, cross-file propagation not resolved. |
| Transaction function stamping | 🟢 `partial` | `2026-06-03` | 4016 | `internal/custom/kotlin/micronaut_quarkus_transactions_test.go`<br>`internal/custom/kotlin/spring_transactions.go` | #4016: each Quarkus jakarta @Transactional fun is stamped transactional=true; non-readOnly write bodies carry db_write=true. The Panache.withTransaction lambda is stamped db_write=true when its body writes (repo.persist) while the read-scope Panache.withSession never does (TestKotlinQkTx_PanacheBoundary_4016). Honest-partial: file-local lexical stamping, no transitive propagation. |
| Transaction propagation | 🟢 `partial` | `2026-06-03` | 4016 | `internal/custom/kotlin/micronaut_quarkus_transactions_test.go`<br>`internal/custom/kotlin/spring_transactions.go` | #4016: JTA TxType.<MODE> propagation captured on the LIVE Kotlin path for Quarkus jakarta @Transactional; a method with no explicit propagation gets NO fabricated default. Honest-partial: regex/file-local; Panache reactive boundaries carry no propagation attribute (none in the API). |
| Transaction rollback rules | 🟢 `partial` | `2026-06-03` | 4016 | `internal/custom/kotlin/micronaut_quarkus_transactions_test.go`<br>`internal/custom/kotlin/spring_transactions.go` | #4016: JTA rollbackOn=[X::class]/dontRollbackOn=[Y::class] (Kotlin ::class list form, JTA spelling) captured on the LIVE Kotlin path; @Transactional(rollbackOn = [IOException::class]) -> rollback_for=IOException on risky asserted by TestKotlinQkTx_FrameworkAttribution_4016. Honest-partial: regex/file-local. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/cdi_interceptors.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin (cdiFrameworks["quarkus"]=true); @Interceptor/@AroundInvoke on Kotlin proven by TestKotlinCDIInterceptors_Issue3274 |
| Aspect extraction | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/cdi_interceptors.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; CDI interceptor aspect extraction proven by TestKotlinCDIInterceptors_Issue3274 |
| Pointcut resolution | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/cdi_interceptors.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; proven by TestKotlinCDIInterceptors_Issue3274 |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/kotlin/observability.go`<br>`internal/custom/kotlin/observability_test.go` | #4015: custom_kotlin_observability fires on Quarkus .kt (Java pass is dead for kotlin — patterns_dispatch skips non-java). SLF4J LoggerFactory.getLogger + log.info partial; TestKotlinObservability_Quarkus_Issue4015. Partial: regex file-local, no cross-file logger correlation, interpolated messages. |
| Metric extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/kotlin/observability.go`<br>`internal/custom/kotlin/observability_test.go` | #4015: Quarkus Micrometer/MicroProfile literal names asserted — @Counted("checkout.attempts"), @Timed("checkout.duration"); TestKotlinObservability_Quarkus_Issue4015. metric_name_source=literal. |
| Trace extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/kotlin/observability.go`<br>`internal/custom/kotlin/observability_test.go` | #4015: Quarkus OpenTelemetry @WithSpan span name asserted (defaults to fun name when no literal arg); TestKotlinObservability_Quarkus_Issue4015. |

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
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow_test.go` | throw X() -> THROWS; try/catch (e: X) -> CATCHES; @ExceptionHandler(X::class) (@ControllerAdvice) + Ktor StatusPages exception<X> -> CATCHES; converges on shared exception node (#3628) |
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
(or use `go run ./tools/coverage update lang.kotlin.framework.quarkus ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
