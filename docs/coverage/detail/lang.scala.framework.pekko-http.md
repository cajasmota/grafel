<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.pekko-http` — Apache Pekko HTTP

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-03` | 4141 | `internal/custom/scala/endpoint_deprecation.go`<br>`internal/custom/scala/endpoint_deprecation_test.go` | #4141 (child of #3628) Scala port: deprecated/deprecation_source(+deprecated_since/deprecated_replacement)+path-derived api_version stamped at the SOURCE on a SCOPE.Pattern/deprecation marker. Pekko HTTP endpoints are SCOPE.Operation custom-extractor entities the engine resolveEndpointDeprecation pass (gated on http_endpoint_definition) cannot reach, so the contract is stamped in the custom-extractor stage (PHP/Kotlin precedent). The Scala stdlib @deprecated(message, since) annotation on a pekko verb directive (NOTE: Scala arg order is message-FIRST since-SECOND, the reverse of Java) credits deprecated=true+deprecated_since+deprecated_replacement; a Scaladoc @deprecated tag, a // DEPRECATED banner, and a Sunset/Deprecation response header (RFC 8594) also fire. detectScalaFramework labels org.apache.pekko files pekko-http distinctly from akka-http. api_version is path-derived from the pekko path-DSL (path("api" / "v1" / ...)) prefering the DSL form over a /api/vN literal in the deprecation message. Identical property contract to the flagship. Value-asserted TestScalaDep_PekkoAnnotation (framework=pekko-http, since=4.0, api_version=1); negatives TestScalaDep_VersionlessNoApiVersion + TestScalaDep_NonRouteDeprecatedUnaffected. |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks: pekko-http routes synthesized as http_route entities; cross-file route-tree composition not resolved. |
| Handler attribution | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks: pekko-http route directives detected; complete(...) handler binding is file-local and not resolved to a named handler cross-file. |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/routing.go` | custom_scala_frameworks: pekko-http detected as its own framework (org.apache.pekko import) and routed through the shared akka-http branch (case akka-http,pekko-http). path("users"/LongNumber)/pathPrefix + method directives combined via nearest-path positional context, canonicalScalaPathExpr ({id} from LongNumber). Value-asserting test TestPekkoHttpRoute pins GET:/api/users/{id} + POST + framework=pekko-http. File-local. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: pekko-http shares the akka-http auth branch — authenticateBasic/OAuth2(+Async/PF) stamp auth_method+realm (paren-safe quoted capture), authorize/authorizeAsync. Value-asserting test TestPekkoHttpAuthDirective asserts auth_method=basic, realm=secure, framework=pekko-http. File-local. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/validation.go` | custom_scala_frameworks: field-level case-class DTO modeling via extractScalaDTOFields (fields, Option nullability, circe/play-json/zio-json codec, wire-name overrides) runs for pekko-http files. File-local. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/validation.go` | custom_scala_frameworks: field-level request validation via extractScalaValidation (refined/cats Validated/accord/octopus) + entity(as[T]) body directive runs for pekko-http. File-local. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: pekko-http shares the akka-http middleware branch — handleRejections/handleExceptions(+named handler), cors(), transform directives (mapRequest/mapResponse/encodeResponse/...). pekko-http-cors recognised. File-local. |
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | — | `internal/custom/scala/rate_limit.go` | #4105 greenfield: custom_scala_rate_limit stamps the flat contract (rate_limited/rate_limit/rate_limit_scope/rate_limit_source/limit/period) on pekko-http Streams `.throttle(elements, per, ...)` stages (positional AND named-arg). amount + FiniteDuration window resolved to a human rate (5/120s) when literal; scope=route (throttle guards a streamed response inside a route — precise verb+path join is heuristic, so none fabricated); source=pekko_throttle (org.apache.pekko.* disambiguates from akka). Honest-partial (rate omitted) when amount/per is config/expression-driven. Value-asserting tests pin the exact amount/per; negatives. File-local marker (SCOPE.Pattern/rate_limit). |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/scala/tests_route_e2e.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/scala_subject_affinity_4360_test.go`<br>`internal/extractors/scala/localvar_receiver_4749_test.go`<br>`internal/extractors/scala/scala.go` | Deep testmap Scala TESTS linkage (testmap/frameworks.go detectScalaTest): scalatest (AnyFunSuite/AnyFlatSpec/AnyWordSpec/AnyFunSpec), specs2, MUnit, ZIO Test leaf cases with subject-from-spec-name (UserServiceSpec->UserService) + body call resolution; anonymous spec leaf blocks ('x' should 'y' in {...} / test(...){...}) get the subject-aware TESTS edge; Scala assertion/matcher stopwords (assert/assertResult/assertTrue/shouldBe/mustBe/specs2 matchers). #4749 (epic #4615/#4749 test->endpoint coverage linkage, Scala slice): (a) local-variable receiver typing in extractors/scala/scala.go (collectScalaLocalVarTypes) resolves a unit test's val c = new FooController(...); c.method() to FooController.method with receiver_type so the handler+endpoint credit lands; explicit-annotation locals seed too, untyped factory locals stay bare (honest exclusion); (b) route-string hits — Play route(app, FakeRequest(GET,'/p')), Akka/Pekko HTTP Get('/p') ~> route, http4s Request[IO](method=GET, uri=uri'/p') / GET(uri'/p') — stamp e2e_route_calls via custom/scala/tests_route_e2e.go and the language-agnostic engine pass (linkE2ERouteTestsToEndpoints, #4351/#4369) emits the endpoint TESTS edge; bare outbound Get('/x') without ~> and production (non-test) files are excluded. Value-asserted in extractors/scala/localvar_receiver_4749_test.go, custom/scala/tests_route_e2e_test.go. Closes #3457. #4360 (ScalaTest/specs2 subject-affinity tiers + scaffolding-orphan drop): detectScalaTest now resolves the SUT via three honest tiers — Tier 1 spec-class stem (OrderServiceSpec->OrderService), Tier 2 leading string-literal subject ("OrderService" should), Tier 3 locally-constructed SUT (new OrderService(...) / mock[OrderService]) — so a leaf with no direct production call still links to the named/instantiated subject; per-leaf body SUT overrides the file-level fallback; unresolved => honest no subject edge. Scaffolding-orphan drop: the redundant per-`in {` WordSpec/specs2 leaf that duplicated a FlatSpec leaf is suppressed (one entity per test), and the edgeless per-file SCOPE.Test/test_suite orphan formerly emitted by custom/scala/frameworks.go (provenance SCALA_TEST_SUITE, no consumer) is no longer emitted. Value-asserted in extractors/cross/testmap/scala_subject_affinity_4360_test.go. Closes #4360. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go): sealed trait/abstract class + Scala 3 enum -> SCOPE.Type ADT with enum_cases. Framework-agnostic. Fixture: TestTrailing_Pekko_TypeSystem. |
| Interface extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go): trait -> SCOPE.Interface/trait, abstract class -> abstract_class. Framework-agnostic. Fixture: TestTrailing_Pekko_TypeSystem. |
| Type alias extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go): type Alias = T and opaque type -> SCOPE.Type/type_alias. Framework-agnostic. Fixture: TestTrailing_Pekko_TypeSystem. |
| Type extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor (type_system.go, gated only on language==scala): case class/class/object -> SCOPE.Type. Framework-agnostic. Fixture: TestTrailing_Pekko_TypeSystem. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | — `not_applicable` | — | — | — | Apache Pekko is the fork of Akka and has no built-in DI container — it uses the actor model and manual constructor injection, identical to Akka-HTTP (the flagship sibling, also not_applicable). DI wiring (Guice/MacWire/ZLayer), when present in a Pekko app, is detected orthogonally by the framework-agnostic di.go extractor. |
| DI injection point | — `not_applicable` | — | — | — | Apache Pekko is the fork of Akka and has no built-in DI container — it uses the actor model and manual constructor injection, identical to Akka-HTTP (the flagship sibling, also not_applicable). DI wiring (Guice/MacWire/ZLayer), when present in a Pekko app, is detected orthogonally by the framework-agnostic di.go extractor. |
| DI scope resolution | — `not_applicable` | — | — | — | Apache Pekko is the fork of Akka and has no built-in DI container — it uses the actor model and manual constructor injection, identical to Akka-HTTP (the flagship sibling, also not_applicable). DI wiring (Guice/MacWire/ZLayer), when present in a Pekko app, is detected orthogonally by the framework-agnostic di.go extractor. |

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
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks (shared): log statement call sites detected (SLF4J/scala-logging/Play/Akka-Pekko/Cats-ZIO). HONEST PARTIAL: logger identity + message<->logger binding need cross-file dataflow. |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks (shared): reScalaMetricNamed captures literal metric name per call site (Kamon/Micrometer/Dropwizard). Runs for all Scala frameworks. metric_name in props. |
| Trace extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks (shared): reScalaTraceNamed captures literal span name per call site (Kamon/OTel/natchez). Runs for all Scala frameworks. span_name in props. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go, RegisterEffectSniffer('scala')) recognises Slick/Doobie/Quill/JPA read+write primitives; framework-agnostic, fires on any .scala file. Fixture: TestScalaTrailing_Pekko_Effects. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/custom/scala/pekko_confidence_overlay_parity_test.go`<br>`internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | Graph-wide confidence overlay (Phase 1C, #2769): every emitted entity carries a Confidence read via types.EffectiveConfidence; MCP tools filter on min_confidence. Framework-agnostic — pekko-http (org.apache.pekko.* scala) entities flow through it identically to the nine sibling scala frameworks. Value-asserted by TestScalaPekko_ConfidenceOverlay: drives the real pekko-http extractor (emits GET:/api/users), asserts BaseConfidence(SourceRegexPattern)=0.7 and the min_confidence gate (passes 0.5, filtered by 0.85). Mirrors the akka-http sibling cell. |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/scala_pekko_substrate_parity_test.go`<br>`internal/substrate/substrate.go` | Scala constant sniffer (sniffScala, substrate/scala.go, Register("scala")) resolves top-level val/final val string-literal bindings; framework-agnostic, fires on any .scala file via LanguageForPath. Value-asserted by TestScalaPekko_ConstantPropagation: on a pekko-http PekkoConfig object, API_BASE resolves to the EXACT literal https://api.acme.test/v1 (provenance=literal) and SERVICE_NAME to user-service. Mirrors the akka-http sibling cell (full). |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability/dead-code pass over Scala entry points (entry_points_scala.go) + IMPORTS/CALLS edges; framework-agnostic, fires on any .scala file. |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_scala.go` | Scala def-use sniffer (RegisterDefUseSniffer('scala'), def_use_scala.go) fires on any .scala file via LanguageForPath; def_use_pass.go invokes it for all scala entities. File-local val/var/for-generator def->use pairs. Fixture: TestScalaTrailing_Pekko_DefUse. |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/scala_pekko_substrate_parity_test.go`<br>`internal/substrate/substrate.go` | Scala constant sniffer (sniffScala, substrate/scala.go) recognises sys.env.getOrElse("VAR", "default") with ProvenanceEnvFallback; framework-agnostic, fires on any .scala file. Value-asserted by TestScalaPekko_EnvFallbackRecognition: on a pekko-http PekkoServer object, PORT captures the EXACT env-var PEKKO_HTTP_PORT, default 8558, provenance env_fallback. Mirrors the akka-http sibling cell (full). |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/scala/exception_flow.go`<br>`internal/extractors/scala/exception_flow_test.go` | throw new X -> THROWS; pattern-match catch { case e: X } + .recover/.recoverWith { case e: X } -> CATCHES; qualified normalized to bare; catch-all/NonFatal/re-throw dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises Source.fromFile/Files/os-lib read+write primitives; framework-agnostic. |
| HTTP effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises outbound HTTP primitives (sttp/akka-pekko/http4s/requests); framework-agnostic. |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/scala_pekko_substrate_parity_test.go`<br>`internal/substrate/substrate.go` | Scala constant sniffer (sniffScala, substrate/scala.go) resolves plain/braced/rebinding imports to their ImportSource (ProvenanceCrossFile); framework-agnostic, fires on any .scala file. Value-asserted by TestScalaPekko_ImportResolutionQuality: on a pekko-http directives file, Http resolves to org.apache.pekko.http.scaladsl, braced Directives to ...server.Directives, and the rebinding Route => PekkoRoute to ...server.Route. Partial: file-local, cross-file binding resolved by the links pass. Mirrors the akka-http sibling cell (partial). |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by the Scala extractor pipeline; framework-agnostic. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises this.<field>= mutation; framework-agnostic. |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | — | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags Scala functions with no effect properties; framework-agnostic (esp. apt for effectful/functional Scala idioms). |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability pass seeded from Scala entry points (entry_points_scala.go); framework-agnostic. |
| Request shape extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | Scala payload-shape sniffer (payload_shapes_scala.go, RegisterPayloadShapeSniffer('scala')) extracts request shapes from case-class bodies / req.as[T]; framework-agnostic. Fixture: TestScalaTrailing_Pekko_PayloadShape. |
| Request sink dataflow | ✅ `full` | — | 3991 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_scala.go`<br>`internal/substrate/dataflow_scala_test.go` | SCOPED request-input -> sink DATA_FLOWS_TO (#3628 area #22, epic #3872, audit #3887), added via #3991: Scala is now the 8th language with a connected source->sink dataflow pass (py/jsts/go/ruby/java/php + scala). dataflow_scala.go registers a sniffer on the "scala" slug, dispatched by .scala/.sc through LanguageForPath. Sources (aligned with taint_sites_scala.go): Play request.body/queryString/getQueryString("k"), Akka/Pekko entity(as[T]){dto=>} and parameter("q"){q=>}, http4s req.as[T]/req.params. Sinks: Slick q+= / .insertOrUpdate / .update / em.persist (db_write), Play Ok(...)/Akka-Pekko complete(...) (response), sttp basicRequest.post(...).body(...) (http_call). Intra-fn val/var assignment tracking, member-field lift (dto.email->email), bounded multi-hop (<=3) + cross-file boundaries continued by the links pass. Value-asserting tests connect the specific source field to the specific sink (both ends named), incl. negatives (logged-not-sunk, constant-fed sink, reassignment, embedded-expr). Full: both source and sink idioms resolve first-class for this framework. |
| Response shape extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | Scala payload-shape sniffer (payload_shapes_scala.go) extracts response shapes from Json.obj / case-class bodies; framework-agnostic. Fixture: TestScalaTrailing_Pekko_PayloadShape / Json.obj path. |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises parameterised-SQL/HTML-escape/Form-mapping sanitizers; framework-agnostic. |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go`<br>`internal/substrate/scala_pekko_substrate_parity_test.go` | Scala payload-shape sniffer (sniffPayloadShapesScala, payload_shapes_scala.go, RegisterPayloadShapeSniffer("scala")) feeds the payload_drift comparison (req/resp producer vs consumer shapes); framework-agnostic, fires on any .scala file. Value-asserted by TestScalaPekko_SchemaDriftDetection: on a pekko-http handler, the req.as[CreateUser] producer request shape has EXACT fields [age email name] with age Optional (drift-relevant nullability), and the Json.obj producer response shape has [id status]. Partial mirrors the akka-http sibling cell. |
| Taint sink detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises SQL-splice/command/path/XSS/ReDoS sinks; framework-agnostic. |
| Taint source detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go, RegisterTaintSniffer('scala')) recognises request/param/sys.env/decode sources; framework-agnostic, fires on any .scala file. Fixture: TestScalaTrailing_Pekko_Taint. |
| Template pattern catalog | 🟢 `partial` | `2026-06-04` | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/scala_pekko_substrate_parity_test.go`<br>`internal/substrate/template_pattern_scala.go` | Scala template-pattern sniffer (sniffTemplatePatternsScala, RegisterTemplatePatternSniffer("scala")) catalogues i18n (Messages/messagesApi/translate), log-format (logger.<level>/println literal), and SQL literal patterns; framework-agnostic, fires on any .scala file. Value-asserted by TestScalaPekko_TemplatePatternCatalog: on a pekko-http handler it captures i18n key user.created (tag Messages), log literal "user creation slow" (tag logger.warn), and the EXACT SQL SELECT literal (tag sql.literal). Mirrors the akka-http sibling cell (partial). |
| Vulnerability finding | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint flow (taint_flow.go over taint_sites_scala.go) reports source->sink findings; framework-agnostic. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.pekko-http ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
