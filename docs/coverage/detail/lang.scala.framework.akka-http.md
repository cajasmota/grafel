<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.akka-http` — Akka HTTP / Pekko HTTP

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
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/frameworks/akka_http_pekko_http.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/frameworks/akka_http_pekko_http.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/routing.go` | custom_scala_frameworks extractor: framework-specific route DSL patterns. File-local. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: akka-http security directives authenticateBasic/authenticateOAuth2 (+Async/PF variants) stamp auth_method (basic/oauth2/jwt) and realm (quoted-string capture, paren-safe); authorize/authorizeAsync stamped as authorize. Value-asserting tests. File-local. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/validation.go` | Field-level DTO extraction: case class primary-constructor fields (name+declared type), Option[T] nullability, circe (@JsonCodec/deriveDecoder)/play-json (Json.format[T])/zio-json codec attribution, and @JsonKey/@jsonField/@key wire-name overrides. Emits one SCOPE.Type/dto (fields summary + nullable_fields + wire_overrides + codec) plus one SCOPE.Type/dto_field per field. File-local. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/validation.go` | Field-level request validation: refined predicate types (String Refined NonEmpty, Int Refined Positive, MatchesRegex[...], Refined[T,P]) captured as field+constraint; cats Validated/ValidatedNel validators (validator fn + inferred field); accord (validator[T]{ p.field is notEmpty }) per-clause field+predicate; octopus .rule(_.field,...). Each request_validation entity records the specific field + constraint. Refined constraints are field-co-located. Coarse framework directive signal (entity(as[T])/jsonOf[T]/decode[T]) retained. File-local: validators in a separate file from the DTO are not linked. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: akka-http handleRejections/handleExceptions with named handler, cors(), and transform directives (mapRequest/mapResponse/encodeResponse/decodeRequest/logRequestResult/...) stamped by middleware_name. Value-asserting tests. File-local. |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap Scala TESTS linkage: scalatest (AnyFunSuite/AnyFlatSpec/AnyWordSpec/AnyFunSpec), specs2, MUnit, ZIO Test leaf cases with subject-from-spec-name (UserServiceSpec->UserService) + body call resolution; Scala assertion/matcher stopwords (assert/assertResult/assertTrue/shouldBe/mustBe/must_==/specs2 matchers). Value-asserting tests in extractor_test.go assert specific test->target edges per framework. Closes #3457. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: sealed trait → ADT, sealed abstract class, Scala 3 enum → SCOPE.Type/sealed_trait|enum. Captures Scala 2+3 ADT discriminant patterns. |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: trait → SCOPE.Interface/trait, abstract class → SCOPE.Interface/abstract_class. Scala traits are the primary interface mechanism. |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: type Alias = T → SCOPE.Type/type_alias; opaque type (Scala 3) → SCOPE.Type/opaque_type. Scala type aliases are pervasive in functional libraries. |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: case class, class, object → SCOPE.Type. File-local; cross-file type hierarchies not resolved. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | — `not_applicable` | — | — | — | This framework has no built-in DI container. Akka-HTTP uses manual constructor injection; Cask is a minimalist library with no DI support; http4s uses functional effect composition (cats-effect Resource) not a DI container; Scalatra is a Sinatra-style servlet with no DI model. |
| DI injection point | — `not_applicable` | — | — | — | This framework has no built-in DI container. Akka-HTTP uses manual constructor injection; Cask is a minimalist library with no DI support; http4s uses functional effect composition (cats-effect Resource) not a DI container; Scalatra is a Sinatra-style servlet with no DI model. |
| DI scope resolution | — `not_applicable` | — | — | — | This framework has no built-in DI container. Akka-HTTP uses manual constructor injection; Cask is a minimalist library with no DI support; http4s uses functional effect composition (cats-effect Resource) not a DI container; Scalatra is a Sinatra-style servlet with no DI model. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | — `not_applicable` | — | — | — | Scala HTTP framework layer does not define transaction boundaries. Transactions are managed by the ORM/DB layer (Slick, Doobie, quill, ZIO-jdbc). These frameworks do not provide @Transactional annotations or equivalent transaction interceptors. The orm.* records cover transaction tracking for Scala persistence libraries. |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | — `not_applicable` | — | — | — | Scala HTTP framework layer does not define transaction boundaries. Transactions are managed by the ORM/DB layer (Slick, Doobie, quill, ZIO-jdbc). These frameworks do not provide @Transactional annotations or equivalent transaction interceptors. The orm.* records cover transaction tracking for Scala persistence libraries. |
| Transaction rollback rules | — `not_applicable` | — | — | — | Scala HTTP framework layer does not define transaction boundaries. Transactions are managed by the ORM/DB layer (Slick, Doobie, quill, ZIO-jdbc). These frameworks do not provide @Transactional annotations or equivalent transaction interceptors. The orm.* records cover transaction tracking for Scala persistence libraries. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | — `not_applicable` | — | — | — | Scala HTTP frameworks do not use Spring AOP / AspectJ proxy model. AOP is a Java/Spring-specific concept. These frameworks use functional composition (Kleisli, ZIO layers, Akka behaviors) instead of aspect weaving. |
| Aspect extraction | — `not_applicable` | — | — | — | Scala HTTP frameworks do not use Spring AOP / AspectJ proxy model. AOP is a Java/Spring-specific concept. These frameworks use functional composition (Kleisli, ZIO layers, Akka behaviors) instead of aspect weaving. |
| Pointcut resolution | — `not_applicable` | — | — | — | Scala HTTP frameworks do not use Spring AOP / AspectJ proxy model. AOP is a Java/Spring-specific concept. These frameworks use functional composition (Kleisli, ZIO layers, Akka behaviors) instead of aspect weaving. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks: log statement call sites detected (SLF4J LoggerFactory.getLogger, scala-logging Logger/LazyLogging, Play Logger, Akka actor logging, Cats Effect/ZIO log; logger.info/warn/error/debug). HONEST PARTIAL: logger identity + message<->logger binding need cross-file dataflow (logger field decl -> call site); same limit as Java/PHP/Rust/Kotlin log_extraction. |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks: reScalaMetricNamed captures LITERAL metric name per call site — Kamon counter/gauge/histogram/timer/rangeSampler, Micrometer Counter/Timer/Gauge/DistributionSummary.builder + registry.counter/timer/gauge/summary, Dropwizard metrics.meter/counter/timer/histogram. metric_name in props. Value-asserting test TestFrameworksObservabilityMetricNames{Micrometer,KamonDropwizard}. Dynamic names fall back to file-local entity. |
| Trace extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks: reScalaTraceNamed captures LITERAL span name per call site — Kamon span/spanBuilder/serverSpanBuilder/clientSpanBuilder, OTel tracer.spanBuilder/startSpan, natchez Trace[F].span. span_name in props. Value-asserting test TestFrameworksObservabilityTraceNames. Dynamic names fall back to file-local entity. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_scala.go` | Scala def-use sniffer (RegisterDefUseSniffer('scala')) is registered in def_use_scala.go; def_use_pass.go invokes it for all scala entities. File-local val/var/for-generator patterns. |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by per-language extractors; Scala import edges are emitted by the Scala extractor pipeline. |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags functions with no effect properties; Scala is a functional language with many pure functions (cats-effect IO, ZIO effects, case class methods). Especially apt for cats-effect, http4s, zio-http. |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Request sink dataflow | ✅ `full` | — | 3991 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_scala.go`<br>`internal/substrate/dataflow_scala_test.go` | SCOPED request-input -> sink DATA_FLOWS_TO (#3628 area #22, epic #3872, audit #3887), added via #3991: Scala is now the 8th language with a connected source->sink dataflow pass (py/jsts/go/ruby/java/php + scala). dataflow_scala.go registers a sniffer on the "scala" slug, dispatched by .scala/.sc through LanguageForPath. Sources (aligned with taint_sites_scala.go): Play request.body/queryString/getQueryString("k"), Akka/Pekko entity(as[T]){dto=>} and parameter("q"){q=>}, http4s req.as[T]/req.params. Sinks: Slick q+= / .insertOrUpdate / .update / em.persist (db_write), Play Ok(...)/Akka-Pekko complete(...) (response), sttp basicRequest.post(...).body(...) (http_call). Intra-fn val/var assignment tracking, member-field lift (dto.email->email), bounded multi-hop (<=3) + cross-file boundaries continued by the links pass. Value-asserting tests connect the specific source field to the specific sink (both ends named), incl. negatives (logged-not-sunk, constant-fed sink, reassignment, embedded-expr). Full: both source and sink idioms resolve first-class for this framework. |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_scala.go` | Scala template-pattern sniffer recognises i18n (Messages/messagesApi), log-format (logger.info/warn/error), and SQL literal patterns in Scala source files. |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.akka-http ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
