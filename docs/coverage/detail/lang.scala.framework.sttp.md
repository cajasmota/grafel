<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.sttp` — sttp (HTTP client)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
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
| Endpoint synthesis | ✅ `full` | `2026-05-31` | — | `internal/engine/http_endpoint_scala_client.go`<br>`internal/engine/http_endpoint_scala_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | sttp basicRequest/quickRequest/emptyRequest verb combinators (.get/.post/.put/.patch/.delete/.head/.options) with uri"..." literals -> one outbound http_endpoint_call (consumer) per call site + FETCHES from the enclosing def; host stripped, $id/${...} interpolation -> {param}. Value-asserted (GET/POST /v1/users, PUT /v1/users/{param}). |
| Handler attribution | ✅ `full` | `2026-05-31` | — | `internal/engine/http_endpoint_scala_client.go`<br>`internal/engine/http_endpoint_scala_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | FETCHES edge attributes each outbound sttp call to the enclosing def (nearest preceding def <name>). Value-asserted via requireFetches in http_endpoint_scala_client_test.go. |
| Route extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap Scala TESTS linkage (testmap/frameworks.go): scalatest/specs2/MUnit/ZIO leaf cases with subject-from-spec-name + body-call resolution; assertion/matcher stopwords. Framework-agnostic (operates on test source). Value-asserting test pins a specific test->target edge for this framework. Fixture: TestScalaTrailing_Sttp_TestsLinkage. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go): sealed trait/abstract class + Scala 3 enum -> SCOPE.Type ADT with enum_cases. Framework-agnostic. |
| Interface extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go): trait -> SCOPE.Interface/trait, abstract class -> abstract_class. Framework-agnostic. |
| Type alias extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go): type Alias = T and opaque type -> SCOPE.Type/type_alias. Framework-agnostic. |
| Type extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go, gated only on language==scala): case class/class/object -> SCOPE.Type. Framework-agnostic. |

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
| Log extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks shared Observability block (frameworks.go, outside the framework switch) detects log statements (SLF4J/scala-logging/cats-effect/ZIO). HONEST PARTIAL: logger identity + message<->logger binding need cross-file dataflow; same limit as the other Scala frameworks. Fixture: TestTrailing_Sttp_Observability. |
| Metric extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks shared Observability block (frameworks.go): reScalaMetricNamed captures the LITERAL metric name per call site (Kamon/Micrometer/Dropwizard). Fires on any .scala file regardless of framework. Fixture: TestTrailing_Sttp_Observability. |
| Trace extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks shared Observability block (frameworks.go): reScalaTraceNamed captures the LITERAL span name per call site (Kamon/OTel/natchez). Fires on any .scala file regardless of framework. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go, RegisterEffectSniffer('scala')) recognises Slick/Doobie/Quill/JPA read+write primitives; framework-agnostic, fires on any .scala file. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability/dead-code pass over Scala entry points (entry_points_scala.go) + IMPORTS/CALLS edges; framework-agnostic, fires on any .scala file. |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_scala.go` | Scala def-use sniffer (RegisterDefUseSniffer('scala'), def_use_scala.go) fires on any .scala file via LanguageForPath; def_use_pass.go invokes it for all scala entities. File-local val/var/for-generator def->use pairs. Fixture: TestScalaTrailing_Sttp_DefUse. |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises Source.fromFile/Files/os-lib read+write primitives; framework-agnostic. |
| HTTP effect | ✅ `full` | `2026-05-31` | — | `internal/engine/http_endpoint_scala_client.go`<br>`internal/engine/http_endpoint_scala_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Each sttp call recorded as an outbound HTTP effect (verb + path) so the cross-repo linker pairs it with a producer-side definition. Value-asserted. |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by the Scala extractor pipeline; framework-agnostic. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises this.<field>= mutation; framework-agnostic. |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | — | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags Scala functions with no effect properties; framework-agnostic (esp. apt for effectful/functional Scala idioms). |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability pass seeded from Scala entry points (entry_points_scala.go); framework-agnostic. |
| Request shape extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | Scala payload-shape sniffer (payload_shapes_scala.go, RegisterPayloadShapeSniffer('scala')) extracts request shapes from case-class bodies / req.as[T]; framework-agnostic. Fixture: TestScalaTrailing_Sttp_ConsumerShape. |
| Request sink dataflow | 🟢 `partial` | — | 3991 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_scala.go`<br>`internal/substrate/dataflow_scala_test.go` | SCOPED request-input -> sink DATA_FLOWS_TO (#3628 area #22, epic #3872, audit #3887), added via #3991: Scala is now the 8th language with a connected source->sink dataflow pass. dataflow_scala.go registers a sniffer on the "scala" slug, dispatched by .scala/.sc through LanguageForPath. PARTIAL here: the sink side resolves first-class (Slick q+= / .update / em.persist db_write; complete(...)/Ok(...) response; sttp basicRequest.post(...).body(...) http_call), but this framework's request-source binding is recognised only via the shared heuristic surface (request.body / request.params("k") / req.as[T]) rather than a framework-specific decoder, so one end is heuristic (precision-over-recall: unrecognised source shapes are dropped, never fabricated). Intra-fn val/var tracking, member-field lift (dto.email->email), bounded multi-hop (<=3) + cross-file boundaries via the links pass. Value-asserting tests connect the specific source field to the specific sink (both ends named) plus negatives. |
| Response shape extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | Scala payload-shape sniffer (payload_shapes_scala.go) extracts response shapes from Json.obj / case-class bodies; framework-agnostic. |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises parameterised-SQL/HTML-escape/Form-mapping sanitizers; framework-agnostic. |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises SQL-splice/command/path/XSS/ReDoS sinks; framework-agnostic. |
| Taint source detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go, RegisterTaintSniffer('scala')) recognises request/param/sys.env/decode sources; framework-agnostic, fires on any .scala file. |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint flow (taint_flow.go over taint_sites_scala.go) reports source->sink findings; framework-agnostic. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.sttp ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
