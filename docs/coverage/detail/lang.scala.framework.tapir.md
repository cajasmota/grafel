<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.tapir` — tapir (endpoint DSL)

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
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/tapir.go` | custom_scala_frameworks: tapir endpoint values synthesized into http_route entities backend-agnostically (akka/pekko/http4s/netty). method+path+DTO refs+handler in one entity. File-local. |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/tapir.go` | custom_scala_frameworks: tapir .serverLogic/.serverLogicSuccess/Pure(handler) bound to the endpoint, stamped as handler + handler_attribution prop. Value-asserting test asserts handler=handleGetUser/createUserHandler. File-local (handler def may live cross-file). |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/tapir.go` | custom_scala_frameworks: tapir endpoint-DSL. Each endpoint(.get/.post/.method(Method.X)).in("seg" / path[T]("name")).in(query[T]) chain parsed into one http_route with http_method + canonical http_path ({name} from path[T], query params separate). Value-asserting tests TestTapirEndpointRouteAndDTOs/PostRequestBodyDTO/MethodExplicitForm/NamedPathParam pin verb+path. File-local. |

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
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/tapir.go`<br>`internal/custom/scala/validation.go` | custom_scala_frameworks: tapir jsonBody[T]/plainBody/xmlBody under .in (request) / .out (response) / .errorOut (error) → dto_ref entities with role+dto+route; PLUS field-level case-class DTO modeling via extractScalaDTOFields (fields, Option nullability, circe/play-json/zio-json codec). Value-asserting tests assert response_dto=User, request_dto=CreateUserRequest, error_dto=ErrorInfo + dto_ref entities. File-local. |
| Request validation | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/tapir.go`<br>`internal/custom/scala/validation.go` | custom_scala_frameworks: field-level request validation via extractScalaValidation (refined types, cats Validated, accord, octopus) runs for tapir case-class DTOs. PARTIAL: tapir's own .validate()/Validator combinator chain not yet parsed; cross-file. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks (shared): test suites detected (ScalaTest/Specs2/MUnit/TestKit incl PekkoSpec/ScalaTestWithActorTestKit/ZIOSpec). PARTIAL: test->route linkage not resolved cross-file. |

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
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks (shared): log statement call sites detected (SLF4J/scala-logging/Play/Akka-Pekko/Cats-ZIO). HONEST PARTIAL: logger identity + message<->logger binding need cross-file dataflow. |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks (shared): reScalaMetricNamed captures literal metric name per call site (Kamon/Micrometer/Dropwizard). Runs for all Scala frameworks. metric_name in props. |
| Trace extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks (shared): reScalaTraceNamed captures literal span name per call site (Kamon/OTel/natchez). Runs for all Scala frameworks. span_name in props. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/tapir.go`<br>`internal/custom/scala/validation.go` | custom_scala_frameworks: tapir .in(jsonBody[T]) request body DTO captured as request_dto + dto_ref(role=request). Value-asserting test asserts request_dto=CreateUserRequest. File-local. |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/tapir.go`<br>`internal/custom/scala/validation.go` | custom_scala_frameworks: tapir .out(jsonBody[T]) / .errorOut(jsonBody[T]) captured as response_dto/error_dto + dto_ref(role=response/error). Value-asserting test asserts response_dto=User, error_dto=ErrorInfo. File-local. |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.tapir ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
