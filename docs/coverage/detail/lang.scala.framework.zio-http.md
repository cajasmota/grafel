<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.zio-http` — ZIO HTTP / ZIO

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 45

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/frameworks/zio.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/frameworks/zio.yaml` | — |
| Route extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go`<br>`internal/engine/rules/scala/frameworks/zio.yaml` | custom_scala_frameworks extractor: framework-specific route DSL patterns. File-local. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: framework-specific auth patterns (Akka-HTTP authenticateBasic/OAuth2, http4s AuthMiddleware, Scalatra ScentrySupport, Cask Authorization header, ZIO bearerAuth, Finatra @Authenticated, Lagom authenticated). File-local. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: case class with parameters detected as DTO (SCOPE.Type/dto). Scala uses case classes as request/response bodies idiomatically. |
| Request validation | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: framework-specific validation patterns (entity(as[T]), jsonOf[T], params(), decode[T], @NotEmpty, MessageSerializer). File-local. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-05-30` | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: framework-specific middleware (Akka-HTTP mapRequest/cors, http4s Middleware/Logger, Scalatra before/after, Cask Decorator, ZIO HttpMiddleware, Finatra SimpleFilter, Lagom CircuitBreaker). File-local. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: ScalaTest (AnyFlatSpec/WordSpec/FunSuite), Specs2 (Specification), MUnit (CatsEffectSuite), Akka TestKit, http4s test suite, ZIO Test, Finatra EmbeddedTwitterServer detection. File-local. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: sealed trait → ADT, sealed abstract class, Scala 3 enum → SCOPE.Type/sealed_trait|enum. Captures Scala 2+3 ADT discriminant patterns. |
| Interface extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: trait → SCOPE.Interface/trait, abstract class → SCOPE.Interface/abstract_class. Scala traits are the primary interface mechanism. |
| Type alias extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: type Alias = T → SCOPE.Type/type_alias; opaque type (Scala 3) → SCOPE.Type/opaque_type. Scala type aliases are pervasive in functional libraries. |
| Type extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: case class, class, object → SCOPE.Type. File-local; cross-file type hierarchies not resolved. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/di.go` | custom_scala_di extractor: ZLayer.make[Env]/ZLayer.succeed bindings extracted as SCOPE.DI/di_binding. ZIO ZLayer is the idiomatic DI mechanism for ZIO-HTTP apps. File-local. |
| DI injection point | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/di.go` | custom_scala_di extractor: .provide()/.provideLayer() call sites detected as ZLayer injection points. File-local. |
| DI scope resolution | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/di.go` | custom_scala_di extractor: typed val layer: ZLayer[R,E,A] declarations extracted as scoped bindings. ZLayer scoping (scoped/succeed/fromZIO) controls resource lifecycle. File-local. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | — `not_applicable` | — | — | — | Scala HTTP framework layer does not define transaction boundaries. Transactions are managed by the ORM/DB layer (Slick, Doobie, quill, ZIO-jdbc). These frameworks do not provide @Transactional annotations or equivalent transaction interceptors. The orm.* records cover transaction tracking for Scala persistence libraries. |
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
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: SLF4J LoggerFactory, Akka actor logging, Play Logger, Cats Effect/ZIO log, logger.info/warn/error call sites. File-local. |
| Metric extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: Micrometer/Kamon metric builders. File-local. |
| Trace extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/scala/frameworks.go` | custom_scala_frameworks extractor: OpenTelemetry/Kamon trace patterns. File-local. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_scala.go` | Scala def-use sniffer (RegisterDefUseSniffer('scala')) is registered in def_use_scala.go; def_use_pass.go invokes it for all scala entities. File-local val/var/for-generator patterns. |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by per-language extractors; Scala import edges are emitted by the Scala extractor pipeline. |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags functions with no effect properties; Scala is a functional language with many pure functions (cats-effect IO, ZIO effects, case class methods). Especially apt for cats-effect, http4s, zio-http. |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_scala.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_scala.go` | Scala template-pattern sniffer recognises i18n (Messages/messagesApi), log-format (logger.info/warn/error), and SQL literal patterns in Scala source files. |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.zio-http ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
