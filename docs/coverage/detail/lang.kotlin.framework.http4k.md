<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.http4k` — http4k

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
| Endpoint synthesis | ✅ `full` | `2026-06-03` | — | `internal/custom/kotlin/routing.go`<br>`internal/custom/kotlin/routing_test.go`<br>`internal/extractors/custom_dispatch_smoke_test.go` | #4018 (epic #3872): the native custom_kotlin_http4k_routes extractor (routing.go) synthesizes a SCOPE.Operation `endpoint` named '<VERB> <path>' (http_method/path/framework/provenance=INFERRED_FROM_HTTP4K_BIND) for every leaf `"path" bind VERB to handler` of the http4k routing DSL. Nested `"prefix" bind routes( … )` blocks compose their prefix onto inner leaf paths via a paren-balanced prefix stack (http4kComposedBinds), including multi-level nesting. Value-asserted GET /ping, POST/DELETE /users/{id} flat; composed GET /api/v1/users + DELETE /api/v1/users/{id} two-level nested (TestHttp4kRoutes_FlatBind/_NestedBind/_NestedTwoLevels). Negative: a ServerFilters.Cors(...).then(routes(...)) filter chain emits ONLY the wrapped GET /ping route, never the filter token (TestHttp4kRoutes_FilterNotARoute); wrong-language/empty no-op (TestHttp4kRoutes_WrongLanguage/_EmptyContent). Fires live on .kt through RunCustomExtractors (TestSmokeHttp4kHandlerAttributionFiresViaDispatch). |
| Handler attribution | ✅ `full` | `2026-06-03` | — | `internal/custom/kotlin/routing.go`<br>`internal/custom/kotlin/routing_test.go`<br>`internal/extractors/custom_dispatch_smoke_test.go` | #4018 (epic #3872): the handler bound by `to <handler>` is now stamped on the endpoint entity's `handler` property (http4kHandlerAfter, anchored on the `to` keyword after the verb). Method references (`to ::listUsers` -> ::listUsers, `to UserController::getOne` -> UserController::getOne) and bare named-val handlers (`to listHandler` -> listHandler) are attributed verbatim; inline lambdas (`to { req -> … }`) are attributed as 'lambda'. Attribution survives nested prefix composition (the composed leaf keeps its handler) and the full live dispatch pass. Value-asserted in TestHttp4kRoutes_HandlerMethodRef/_HandlerQualifiedRef/_HandlerNamedVal/_HandlerLambda/_HandlerNestedComposed and end-to-end in TestSmokeHttp4kHandlerAttributionFiresViaDispatch. Honest model: the custom-extractor Extract() interface returns entities only (no RelationshipRecord), so attribution is an edge-less `handler` property — the same model the sibling javalin/spring/micronaut Kotlin route extractors use — and a lambda body is not resolved to an enclosing fun. |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/routing.go`<br>`internal/custom/kotlin/routing_test.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/kotlin/http4k_auth_middleware.go` | ServerFilters.BearerAuth/BasicAuth/ApiKey + BearerAuthFilter/OAuthFilter named auth filters — value-asserted by name, file-local |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/kotlin/http4k_auth_middleware.go` | ServerFilters.Cors/RequestTracing/GZip + Filter{next->} lambdas + .then() composition order — value-asserted names+chain order, file-local |
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
| DI binding extraction | — `not_applicable` | — | — | — | http4k has no built-in DI container. The framework is DI-agnostic; projects use Koin, manual wiring, or no DI. |
| DI injection point | — `not_applicable` | — | — | — | http4k has no built-in DI container. No injection-point annotation surface exists in the framework itself. |
| DI scope resolution | — `not_applicable` | — | — | — | http4k has no built-in DI scoping. Not applicable by design. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | — `not_applicable` | — | — | — | http4k has no transaction management layer. Transactions are handled by the persistence library chosen by the user (Exposed, JOOQ, etc.) independently of http4k. |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | — `not_applicable` | — | — | — | http4k has no transaction propagation model. Not applicable by framework design. |
| Transaction rollback rules | — `not_applicable` | — | — | — | http4k has no transaction rollback model. Not applicable by framework design. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | — `not_applicable` | — | — | — | http4k has no AOP / AspectJ proxy model. Cross-cutting concerns are addressed via composable Filter functions. |
| Aspect extraction | — `not_applicable` | — | — | — | http4k has no aspect concept. Not applicable by design. |
| Pointcut resolution | — `not_applicable` | — | — | — | http4k has no pointcut concept. Not applicable by design. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/kotlin/observability.go` | SLF4J LoggerFactory.getLogger, kotlin-logging KotlinLogging.logger {} (logger val name captured), log.info/warn/error call sites and log.info { "..." } lazy-lambda message heads. Kept partial (honest): a logger declared in one file and used in another is not correlated, and message strings are commonly interpolated/dynamic. Same cross-file dataflow gap held partial for Java/PHP/Rust observability. |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/observability.go` | Micrometer Counter/Timer/Gauge/DistributionSummary.builder("name"), <registry>.counter/timer/gauge/summary("name"), @Timed("name")/@Counted("name") with literal metric name captured at the call site (metric_name + metric_name_source provenance; defaults to fun name when annotation arg absent). No cross-file resolution needed to assert the name. Same bar as Java spring-boot. File-local. |
| Trace extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/observability.go` | OTel @WithSpan("name")/tracer.spanBuilder("name"), Spring Sleuth @NewSpan("name"), Micrometer Tracing @Observed(name="name") with literal span name captured at the call site (span_name + span_name_source provenance; defaults to fun/class name when annotation arg absent). No cross-file resolution needed to assert the name. Same bar as Java spring-boot. File-local. |

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
(or use `go run ./tools/coverage update lang.kotlin.framework.http4k ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
