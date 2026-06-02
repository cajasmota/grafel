<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.vertx` — Vert.x

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_deprecation.go`<br>`internal/engine/http_endpoint_reactive_posture_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3858: deprecated/deprecation_source + path-derived api_version on vertx endpoints via the cross-language deprecation pass. @Deprecated (javaDeprecationVerdict), a // DEPRECATED banner comment at the route registration (genericCommentDeprecationVerdict), and a Sunset/Deprecation response header all credit deprecated=true; api_version is path-derived (/v\d/.. or /api/v\d/..). Value-asserted TestDeprecation_Vertx_CommentDeprecated and TestDeprecation_WebFlux_DeprecatedAnnotation (api_version=1) on representative reactive routes. |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_reactive_posture.go`<br>`internal/engine/http_endpoint_reactive_posture_test.go`<br>`internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3858: response_codes/success_code/response_codes_source on Vert.x Web endpoints via vertxResponseCodes (reactiveResponseCodes -> java branch of applyEndpointResponseCodes). Signal: rc.response().setStatusCode(NNN). Value-asserted TestResponseCodes_Vertx_SetStatusCodeCreated/_SetStatusCodeNotFound; negative _NoStatusNotStamped (no setStatusCode -> absent, honest-partial; dynamic setStatusCode(var) skipped). |
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/java/frameworks/vert_x.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/java/frameworks/vert_x.yaml` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3086) | `internal/engine/http_endpoint_synthesis.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3862) | `internal/custom/java/framework_auth.go`<br>`internal/custom/java/framework_auth_test.go`<br>`internal/custom/java/vertx_routes.go` | #3862: framework_auth.go stamps the flat auth contract on Vert.x route entities. JWTAuthHandler/OAuth2AuthHandler/BasicAuthHandler mounted on the router → auth_required=true + auth_mechanism=jwt/oauth2/basic (medium confidence, file-level subtree attribution); @RolesAllowed → auth_roles. Value-asserting tests: JWTAuthHandler on a route → auth_required=true auth_mechanism=jwt; BasicAuthHandler + @RolesAllowed({ADMIN}) → auth_mechanism=basic auth_roles=ADMIN; router with no auth handler → auth_required absent. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/vertx_routes.go` | — |
| Request validation | ✅ `full` | `2026-06-01` | — | `internal/custom/java/vertx_routes.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/vertx_routes.go` | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-02` | — | `internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_java_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | Java JUnit (4/5) deep test->SUT linkage via the shared cross/testmap extractor (#3855), same path that credits Kotlin JVM (#3437). detectJUnit fires on @Test/@ParameterizedTest/@RepeatedTest in *Test.java/*Tests.java/*IT.java (org.junit/junit.jupiter import hints); resolver emits high-confidence TESTS edges for direct SUT calls (new UserService(); userService.create()), medium for class-name subject (UserServiceTest->UserService) when the body has no prod call, and suppresses MockMvc/REST-assured/WebTestClient/AssertJ/Hamcrest/Mockito test-harness noise. Value-asserted in frameworks_java_test.go (TestJUnit_DirectCall_HighConfidence/_MethodCallOnInjectedSUT/_ClassNameSubject/_ParameterizedTest/_MockMvc_NoHTTPClientNoise/_RestAssured_NoDSLNoise). Scope: unit-level test->SUT; framework-handler attribution from HTTP integration tests (MockMvc/REST-assured -> controller endpoint) is out of scope. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |
| Type alias extraction | — `not_applicable` | — | — | — | Java has no type alias syntax |
| Type extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in DI container. Applications use external DI (Guice, CDI, Spring) or manual wiring. |
| DI injection point | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in DI container. Applications use external DI (Guice, CDI, Spring) or manual wiring. |
| DI scope resolution | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in DI container. Applications use external DI (Guice, CDI, Spring) or manual wiring. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in transaction management. Applications use manual async callbacks or Vert.x SQL client directly. |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in transaction management. Applications use manual async callbacks or Vert.x SQL client directly. |
| Transaction rollback rules | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in transaction management. Applications use manual async callbacks or Vert.x SQL client directly. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in AOP support. Applications use external AOP (AspectJ, Spring AOP) if needed. |
| Aspect extraction | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in AOP support. Applications use external AOP (AspectJ, Spring AOP) if needed. |
| Pointcut resolution | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/3086) | — | Vert.x has no built-in AOP support. Applications use external AOP (AspectJ, Spring AOP) if needed. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |
| Metric extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |
| Trace extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/observability.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3093) | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Framework-blind substrate: constant_propagation, effect_propagation, and taint_flow passes emit per-binding/per-finding Confidence values on Java entities via java.go sniffers. EntityRecord.Confidence not yet stamped by the Java extractor directly; MCP min_confidence filtering applies. Partial pending a dedicated confidence-scoring pass writing top-level EntityRecord.Confidence. |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/java/config_consumer.go`<br>`internal/extractors/java/config_consumer_test.go` | @Value, @ConfigurationProperties, env.getProperty, @ConfigProperty -> config:<key> (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.vertx ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
