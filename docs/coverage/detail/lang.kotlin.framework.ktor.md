<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.ktor` — Ktor

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 47

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/ktor.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/kotlin/frameworks/ktor.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/ktor_routes.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/kotlin/ktor_auth_middleware.go` | install(Authentication){jwt/basic/oauth/bearer/session/digest} provider-method detection + authenticate("name") guards — value-asserted; cross-block provider-to-guard binding is honest file-local |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/validation.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/kotlin/ktor_auth_middleware.go` | ordered install(Plugin) pipeline (CORS/CallLogging/ContentNegotiation/...) + custom intercept(ApplicationCallPipeline.Phase) interceptors — value-asserted names+order, file-local |

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
| DI binding extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/ktor_di_transactions.go`<br>`internal/custom/kotlin/ktor_di_transactions_test.go` | Koin module{ single/factory/scoped<T> }; each binding emitted named after its bound type, asserted by TestKtorDI_BindingTypeNames_3435 (UserService, UserRepository, CacheService) |
| DI injection point | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/ktor_di_transactions.go`<br>`internal/custom/kotlin/ktor_di_transactions_test.go` | Koin 'val repo: T by inject()' injection point captured as field:type; asserted by TestKtorDI_BindingTypeNames_3435 (repo:UserRepository) |
| DI scope resolution | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/ktor_di_transactions.go` | Koin scope keyword (single=singleton, factory, scoped) extraction — file-local |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/ktor_di_transactions.go`<br>`internal/custom/kotlin/ktor_di_transactions_test.go` | Exposed transaction { } / newSuspendedTransaction { } boundaries + isolation level; SERIALIZABLE captured, asserted by TestKtorTransactions_BoundaryAndIsolation_3435 |
| Transaction propagation | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/ktor_di_transactions.go` | Exposed transaction propagation defaults to REQUIRED; newSuspendedTransaction is coroutine-aware — file-local |
| Transaction rollback rules | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/ktor_di_transactions.go` | Exposed isolation level hints (Connection.TRANSACTION_*) extracted as rollback context — file-local |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | — `not_applicable` | — | — | — | Ktor has no Spring AOP / AspectJ proxy model. AOP is not used in Ktor applications. |
| Aspect extraction | — `not_applicable` | — | — | — | Ktor has no aspect-oriented programming construct. @Aspect/@Pointcut patterns are Spring-only. |
| Pointcut resolution | — `not_applicable` | — | — | — | Ktor has no pointcut concept. The framework uses suspend functions and Pipelines, not AOP proxies. |

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
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_kotlin.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_kotlin.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.ktor ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
