<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.http4s` — http4s

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-03` | 4141 | `internal/custom/scala/endpoint_deprecation.go`<br>`internal/custom/scala/endpoint_deprecation_test.go` | #4141 (child of #3628) Scala port: deprecated/deprecation_source(+deprecated_since/deprecated_replacement)+path-derived api_version stamped at the SOURCE on a SCOPE.Pattern/deprecation marker. http4s endpoints are SCOPE.Operation custom-extractor entities the engine resolveEndpointDeprecation pass (gated on http_endpoint_definition) cannot reach, so the contract is stamped in the custom-extractor stage (PHP/Kotlin precedent). The Scala stdlib @deprecated(message, since) annotation on a `case GET -> Root / ...` route branch (NOTE: Scala arg order is message-FIRST since-SECOND, the reverse of Java) credits deprecated=true+deprecated_since+deprecated_replacement; a Scaladoc @deprecated tag, a // DEPRECATED banner, and a Sunset/Deprecation response header (RFC 8594, e.g. Header.Raw(ci"Sunset", ...)) also fire. api_version is path-derived from the http4s path-DSL (Root / "api" / "v2") prefering the DSL form over a /api/vN literal in the deprecation message. Identical property contract to the flagship. Value-asserted TestScalaDep_Http4sAnnotation (since=2.0, replacement=/api/v2/users, api_version=1), TestScalaDep_Http4sDSLVersion (api_version=2), TestScalaDep_ScaladocTag, TestScalaDep_BannerComment, TestScalaDep_SunsetHeader; negatives TestScalaDep_NonDeprecatedNone + TestScalaDep_VersionlessNoApiVersion. |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/frameworks/http4s.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/frameworks/http4s.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/routing.go` | custom_scala_frameworks extractor: framework-specific route DSL patterns. File-local. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: http4s AuthMiddleware(authUser) stamps authenticator name + auth_method (basic/bearer/jwt) from window; AuthedRoutes.of detected. Value-asserting tests. File-local (route-binding cross-file unresolved). |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/validation.go` | Field-level DTO extraction: case class primary-constructor fields (name+declared type), Option[T] nullability, circe (@JsonCodec/deriveDecoder)/play-json (Json.format[T])/zio-json codec attribution, and @JsonKey/@jsonField/@key wire-name overrides. Emits one SCOPE.Type/dto (fields summary + nullable_fields + wire_overrides + codec) plus one SCOPE.Type/dto_field per field. File-local. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/validation.go` | Field-level request validation: refined predicate types (String Refined NonEmpty, Int Refined Positive, MatchesRegex[...], Refined[T,P]) captured as field+constraint; cats Validated/ValidatedNel validators (validator fn + inferred field); accord (validator[T]{ p.field is notEmpty }) per-clause field+predicate; octopus .rule(_.field,...). Each request_validation entity records the specific field + constraint. Refined constraints are field-co-located. Coarse framework directive signal (entity(as[T])/jsonOf[T]/decode[T]) retained. File-local: validators in a separate file from the DTO are not linked. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: http4s named middleware CORS/GZip/Logger/Timeout/AutoSlash/CSRF/HSTS + composition order mw1(mw2(routes)) recorded as outer>inner(target). Value-asserting tests. File-local. |
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | — | `internal/custom/scala/rate_limit.go` | #4105 greenfield: custom_scala_rate_limit stamps the flat contract (rate_limited/rate_limit/rate_limit_scope/rate_limit_source/limit/period) on the http4s Throttle middleware — Throttle(amount, per)(app), Throttle.httpApp[F](amount, per)(app), Throttle.httpRoutes(amount, per)(routes) from org.http4s.server.middleware. amount (Int) + per (FiniteDuration literal: 1.minute/10.seconds/100.millis) resolved to a human rate (100/60s; sub-second windows rendered as 200/100ms) when literal; scope=app (Throttle wraps the whole HttpApp/HttpRoutes — app-wide, not a named route, so none fabricated); source=http4s_throttle. Gated on an http4s file signal. Honest-partial (rate omitted) when amount/per is config/expression-driven. Value-asserting tests pin the exact amount/per; negatives (plain route, CORS/GZip non-throttle middleware, non-http4s Throttle). File-local marker (SCOPE.Pattern/rate_limit). |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/scala/tests_route_e2e.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/scala_subject_affinity_4360_test.go`<br>`internal/extractors/scala/localvar_receiver_4749_test.go`<br>`internal/extractors/scala/scala.go` | Deep testmap Scala TESTS linkage (testmap/frameworks.go detectScalaTest): scalatest (AnyFunSuite/AnyFlatSpec/AnyWordSpec/AnyFunSpec), specs2, MUnit, ZIO Test leaf cases with subject-from-spec-name (UserServiceSpec->UserService) + body call resolution; anonymous spec leaf blocks ('x' should 'y' in {...} / test(...){...}) get the subject-aware TESTS edge; Scala assertion/matcher stopwords (assert/assertResult/assertTrue/shouldBe/mustBe/specs2 matchers). #4749 (epic #4615/#4749 test->endpoint coverage linkage, Scala slice): (a) local-variable receiver typing in extractors/scala/scala.go (collectScalaLocalVarTypes) resolves a unit test's val c = new FooController(...); c.method() to FooController.method with receiver_type so the handler+endpoint credit lands; explicit-annotation locals seed too, untyped factory locals stay bare (honest exclusion); (b) route-string hits — Play route(app, FakeRequest(GET,'/p')), Akka/Pekko HTTP Get('/p') ~> route, http4s Request[IO](method=GET, uri=uri'/p') / GET(uri'/p') — stamp e2e_route_calls via custom/scala/tests_route_e2e.go and the language-agnostic engine pass (linkE2ERouteTestsToEndpoints, #4351/#4369) emits the endpoint TESTS edge; bare outbound Get('/x') without ~> and production (non-test) files are excluded. Value-asserted in extractors/scala/localvar_receiver_4749_test.go, custom/scala/tests_route_e2e_test.go. Closes #3457. #4360 (ScalaTest/specs2 subject-affinity tiers + scaffolding-orphan drop): detectScalaTest now resolves the SUT via three honest tiers — Tier 1 spec-class stem (OrderServiceSpec->OrderService), Tier 2 leading string-literal subject ("OrderService" should), Tier 3 locally-constructed SUT (new OrderService(...) / mock[OrderService]) — so a leaf with no direct production call still links to the named/instantiated subject; per-leaf body SUT overrides the file-level fallback; unresolved => honest no subject edge. Scaffolding-orphan drop: the redundant per-`in {` WordSpec/specs2 leaf that duplicated a FlatSpec leaf is suppressed (one entity per test), and the edgeless per-file SCOPE.Test/test_suite orphan formerly emitted by custom/scala/frameworks.go (provenance SCALA_TEST_SUITE, no consumer) is no longer emitted. Value-asserted in extractors/cross/testmap/scala_subject_affinity_4360_test.go. Closes #4360. |

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
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/scala/exception_flow.go`<br>`internal/extractors/scala/exception_flow_test.go` | throw new X -> THROWS; pattern-match catch { case e: X } + .recover/.recoverWith { case e: X } -> CATCHES; qualified normalized to bare; catch-all/NonFatal/re-throw dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by per-language extractors; Scala import edges are emitted by the Scala extractor pipeline. |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags functions with no effect properties; Scala is a functional language with many pure functions (cats-effect IO, ZIO effects, case class methods). Especially apt for cats-effect, http4s, zio-http. |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Request sink dataflow | ✅ `full` | — | 3991 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_scala.go`<br>`internal/substrate/dataflow_scala_test.go` | SCOPED request-input -> sink DATA_FLOWS_TO (#3628 area #22, epic #3872, audit #3887), added via #3991: Scala is now the 8th language with a connected source->sink dataflow pass (py/jsts/go/ruby/java/php + scala). dataflow_scala.go registers a sniffer on the "scala" slug, dispatched by .scala/.sc through LanguageForPath. Sources (aligned with taint_sites_scala.go): Play request.body/queryString/getQueryString("k"), Akka/Pekko entity(as[T]){dto=>} and parameter("q"){q=>}, http4s req.as[T]/req.params. Sinks: Slick q+= / .insertOrUpdate / .update / em.persist (db_write), Play Ok(...)/Akka-Pekko complete(...) (response), sttp basicRequest.post(...).body(...) (http_call). Intra-fn val/var assignment tracking, member-field lift (dto.email->email), bounded multi-hop (<=3) + cross-file boundaries continued by the links pass. Value-asserting tests connect the specific source field to the specific sink (both ends named), incl. negatives (logged-not-sunk, constant-fed sink, reassignment, embedded-expr). Full: both source and sink idioms resolve first-class for this framework. |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_scala.go` | Scala template-pattern sniffer recognises i18n (Messages/messagesApi), log-format (logger.info/warn/error), and SQL literal patterns in Scala source files. |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.http4s ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
