<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.koin` — Koin (Kotlin DI)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |
| Endpoint pagination posture | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |
| Endpoint response codes | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |
| Endpoint synthesis | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |
| Handler attribution | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |
| Route extraction | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |
| Rate limit stamping | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ✅ `full` | — | — | `internal/custom/kotlin/koin.go` | Koin module{} single/factory/viewModel/scoped + *Of(::Type) bindings: binding type, implementation, injected deps via get<T>(). File-local; cross-file module wiring not linked. Value-asserted in koin_test.go. |
| DI injection point | ✅ `full` | — | — | `internal/custom/kotlin/koin.go` | Koin resolution sites: 'by inject()', inject<T>(), get<T>() — field name + injected type + mechanism. Value-asserted in koin_test.go. |
| DI scope resolution | ✅ `full` | — | — | `internal/custom/kotlin/koin.go` | Koin scope resolution: single/factory/viewModel/scoped normalized per binding. Value-asserted in koin_test.go. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction rollback rules | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Aspect extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pointcut resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the per-LANGUAGE sniffKotlin sniffer (Register("kotlin")) gates only on file content with zero per-framework branching, so Koin .kt files dispatch the SAME const/literal sniffer as flagship siblings. Value-asserting test drives the Koin idiom and asserts the EXACT literal value + ProvenanceLiteral + Confidence 1.0. |
| Dead code detection | 🟢 `partial` | `2026-06-04` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the reachability/dead-code pass (internal/links/reachability.go) BFS-roots on the Kotlin entry-point sniffer’s library_export seeds; Koin is rooted on the public top-level module-defining fun. Unreached Koin entities are flagged dead-code candidates. Value-asserting test asserts the EXACT library_export seed. |
| Def use chain extraction | 🟢 `partial` | `2026-06-04` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the per-LANGUAGE Kotlin def-use sniffer (RegisterDefUseSniffer("kotlin")) gates on file content with zero per-framework branching, so Koin .kt files dispatch the SAME def-use sniffer as flagship siblings. Value-asserting test drives a module { single { … } } DSL fun body and asserts the EXACT function-attributed local def + matching use. |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the framework-blind kotlin substrate sniffer recognises the env-fallback idiom regardless of framework; Koin dispatches it identically. Test asserts the EXACT env-var name + default literal + ProvenanceEnvFallback + Confidence 0.85 on the Koin idiom. |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow_test.go` | throw X() -> THROWS; try/catch (e: X) -> CATCHES; @ExceptionHandler(X::class) (@ControllerAdvice) + Ktor StatusPages exception<X> -> CATCHES; converges on shared exception node (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the kotlin cross-file import sniffer is framework-blind; Koin dispatches it identically. PARTIAL (mirrors all siblings): single-segment binding, no transitive/re-export graph. Test asserts the EXACT ImportSource + ProvenanceCrossFile + Confidence 0.6 on the Koin idiom. |
| Module cycle detection | 🟢 `partial` | `2026-06-04` | — | `internal/extractors/kotlin/references.go`<br>`internal/links/module_cycle_pass.go` | #3872: the module-cycle pass (internal/links/module_cycle_pass.go) is language-agnostic Tarjan over the common IMPORTS edge graph that the Kotlin extractor emits; Koin’s multi-file imports feed it identically to siblings. PARTIAL (mirrors siblings): no Koin-specific cyclic-import fixture asserted end-to-end yet. |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🟢 `partial` | `2026-06-04` | — | `internal/links/pure_function_pass.go`<br>`internal/substrate/def_use_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the pure-function pass (internal/links/pure_function_pass.go, "zero per-language code") walks every function-like entity and tags those with no stamped effect set. Koin functions are tagged identically to siblings; the def-use proof for a module { single { … } } DSL fun body establishes the function entities it walks. PARTIAL (mirrors siblings): no Koin-specific memoization fixture asserted end-to-end yet. |
| Reachability analysis | 🟢 `partial` | `2026-06-04` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the framework-blind reachability BFS (internal/links/reachability.go) seeds on the Kotlin entry-point sniffer (internal/substrate/entry_points_kotlin.go); for Koin the seed is the public top-level module-defining fun. Value-asserting test asserts the EXACT library_export entry-point Ident for this framework’s idiom. |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request sink dataflow | — `not_applicable` | `2026-06-12` | 4924 | `internal/custom/kotlin/koin.go` | lang.kotlin.framework.koin is a dependency-injection container, not an HTTP server framework — server-side routing/handler/endpoint/auth/middleware/rate-limit/validation/view-rendering concerns do not apply (parity with Arrow). #4924: these inflated Kotlin 'missing' metrics. DI binding/injection-point/scope extraction is the real surface. |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | #3872: the framework-agnostic payload-drift pass dispatches sniffPayloadShapesKotlin by LANGUAGE slug (LanguageForPath->PayloadShapeSnifferFor), so Koin producer/consumer shapes feed the same drift join as siblings. PARTIAL (mirrors siblings): no Koin-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | #3872: sniffTemplatePatternsKotlin is registered on the kotlin language slug and gates only on file content (no per-framework branch), so Koin dispatches it identically. PARTIAL: mirrors all siblings. |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.koin ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
