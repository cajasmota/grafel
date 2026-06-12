<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.retrofit` — Retrofit (HTTP client)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |
| Endpoint pagination posture | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |
| Endpoint response codes | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |
| Endpoint synthesis | ✅ `full` | — | — | `internal/engine/http_endpoint_kotlin_client.go`<br>`internal/engine/http_endpoint_kotlin_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Retrofit interface verb annotations (@GET/@POST/@PUT/@DELETE/@PATCH/@HEAD/@OPTIONS) → one outbound http_endpoint (consumer) per annotated method + FETCHES from the method; Retrofit.Builder().baseUrl() composed; {param} paths normalized. Value-asserted in http_endpoint_kotlin_client_test.go. |
| Handler attribution | ✅ `full` | — | — | `internal/engine/http_endpoint_kotlin_client.go`<br>`internal/engine/http_endpoint_kotlin_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | FETCHES edge attributes each outbound call to the enclosing Retrofit interface method. Value-asserted in http_endpoint_kotlin_client_test.go. |
| Route extraction | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |
| Rate limit stamping | — `not_applicable` | `2026-06-12` | 4924 | `internal/engine/http_endpoint_kotlin_client.go` | Retrofit is an HTTP CLIENT (outbound), not a server framework — server-side routing/middleware/auth/rate-limit/response-code/pagination concerns do not apply (parity with how Arrow marks server cells N/A). #4924 reclassification: these were inflating Kotlin 'missing' metrics. Retrofit @-annotation interfaces + baseUrl composition are extracted as FETCHES via internal/engine/http_endpoint_kotlin_client.go. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | ✅ `full` | — | — | `internal/engine/http_endpoint_kotlin_client.go`<br>`internal/engine/http_endpoint_kotlin_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Retrofit service is a Kotlin interface; each annotated method head (suspend fun / fun) is attributed as the calling reference for its outbound endpoint. Value-asserted in http_endpoint_kotlin_client_test.go. |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the per-LANGUAGE sniffKotlin sniffer (Register("kotlin")) gates only on file content with zero per-framework branching, so Retrofit .kt files dispatch the SAME const/literal sniffer as flagship siblings. Value-asserting test drives the Retrofit idiom and asserts the EXACT literal value + ProvenanceLiteral + Confidence 1.0. |
| Dead code detection | 🟢 `partial` | `2026-06-04` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the reachability/dead-code pass (internal/links/reachability.go) BFS-roots on the Kotlin entry-point sniffer’s library_export seeds; Retrofit is rooted on the public service interface + factory fun. Unreached Retrofit entities are flagged dead-code candidates. Value-asserting test asserts the EXACT library_export seed. |
| Def use chain extraction | 🟢 `partial` | `2026-06-04` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the per-LANGUAGE Kotlin def-use sniffer (RegisterDefUseSniffer("kotlin")) gates on file content with zero per-framework branching, so Retrofit .kt files dispatch the SAME def-use sniffer as flagship siblings. Value-asserting test drives a @GET/@POST service factory fun body and asserts the EXACT function-attributed local def + matching use. |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the framework-blind kotlin substrate sniffer recognises the env-fallback idiom regardless of framework; Retrofit dispatches it identically. Test asserts the EXACT env-var name + default literal + ProvenanceEnvFallback + Confidence 0.85 on the Retrofit idiom. |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow_test.go` | throw X() -> THROWS; try/catch (e: X) -> CATCHES; @ExceptionHandler(X::class) (@ControllerAdvice) + Ktor StatusPages exception<X> -> CATCHES; converges on shared exception node (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ✅ `full` | — | — | `internal/engine/http_endpoint_kotlin_client.go`<br>`internal/engine/http_endpoint_kotlin_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Each Retrofit annotated method is recorded as an outbound HTTP effect (verb + path) so the cross-repo linker pairs it with a producer. Value-asserted in http_endpoint_kotlin_client_test.go. |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the kotlin cross-file import sniffer is framework-blind; Retrofit dispatches it identically. PARTIAL (mirrors all siblings): single-segment binding, no transitive/re-export graph. Test asserts the EXACT ImportSource + ProvenanceCrossFile + Confidence 0.6 on the Retrofit idiom. |
| Module cycle detection | 🟢 `partial` | `2026-06-04` | — | `internal/extractors/kotlin/references.go`<br>`internal/links/module_cycle_pass.go` | #3872: the module-cycle pass (internal/links/module_cycle_pass.go) is language-agnostic Tarjan over the common IMPORTS edge graph that the Kotlin extractor emits; Retrofit’s multi-file imports feed it identically to siblings. PARTIAL (mirrors siblings): no Retrofit-specific cyclic-import fixture asserted end-to-end yet. |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🟢 `partial` | `2026-06-04` | — | `internal/links/pure_function_pass.go`<br>`internal/substrate/def_use_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the pure-function pass (internal/links/pure_function_pass.go, "zero per-language code") walks every function-like entity and tags those with no stamped effect set. Retrofit functions are tagged identically to siblings; the def-use proof for a @GET/@POST service factory fun body establishes the function entities it walks. PARTIAL (mirrors siblings): no Retrofit-specific memoization fixture asserted end-to-end yet. |
| Reachability analysis | 🟢 `partial` | `2026-06-04` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the framework-blind reachability BFS (internal/links/reachability.go) seeds on the Kotlin entry-point sniffer (internal/substrate/entry_points_kotlin.go); for Retrofit the seed is the public service interface + factory fun. Value-asserting test asserts the EXACT library_export entry-point Ident for this framework’s idiom. |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | #3872: the framework-agnostic payload-drift pass dispatches sniffPayloadShapesKotlin by LANGUAGE slug (LanguageForPath->PayloadShapeSnifferFor), so Retrofit producer/consumer shapes feed the same drift join as siblings. PARTIAL (mirrors siblings): no Retrofit-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | #3872: sniffTemplatePatternsKotlin is registered on the kotlin language slug and gates only on file content (no per-framework branch), so Retrofit dispatches it identically. PARTIAL: mirrors all siblings. |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.retrofit ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
