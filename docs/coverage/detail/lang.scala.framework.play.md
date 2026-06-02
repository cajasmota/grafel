<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.play` — Play Framework (Scala)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 37

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | — `not_applicable` | — | — | — | Play Framework is a server-side MVC framework. Frontend component model (React/Vue-style) does not exist in Play. Controllers and Twirl templates are server-rendered, not component trees. |
| Hook recognition | — `not_applicable` | — | — | — | Play Framework is a server-side MVC framework. React/lifecycle hooks do not exist. Application lifecycle hooks (ApplicationLifecycle) are covered by DI — not frontend hook model. |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Data loaders | — `not_applicable` | — | — | — | Play Framework is a server-side MVC framework. Data loaders (Next.js getServerSideProps/React Server Components) are frontend SSR patterns absent in Play. |

### Server

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Hydration boundaries | — `not_applicable` | — | — | — | Play Framework does not use hydration boundaries. Server-side rendering is via Twirl templates with no client hydration concept. |
| Server components | — `not_applicable` | — | — | — | Play Framework has no React Server Components or similar framework feature. All rendering is server-side via Twirl. |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/custom/scala/routing.go` | Play conf/routes file parsed by rePlayRoute regex (GET/POST/etc path controller.action); controller class patterns in play_framework.yaml. File-local. |
| Router pattern | 🟢 `partial` | `2026-05-30` | — | `internal/custom/scala/frameworks.go`<br>`internal/engine/rules/scala/frameworks/play_framework.yaml` | Play reverse routing (routes.reverse*, Routes.) detected by rePlayRouterPattern. play_framework.yaml contains route file_conventions. |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | — `not_applicable` | — | — | — | Play Framework is request-driven. There is no static site generation mode; output is dynamic per request. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: sealed trait → ADT, sealed abstract class, Scala 3 enum → SCOPE.Type/sealed_trait|enum. Captures Scala 2+3 ADT discriminant patterns. |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: trait → SCOPE.Interface/trait, abstract class → SCOPE.Interface/abstract_class. Scala traits are the primary interface mechanism. |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/scala/type_system.go` | custom_scala_type_system extractor: type Alias = T → SCOPE.Type/type_alias; opaque type (Scala 3) → SCOPE.Type/opaque_type. Scala type aliases are pervasive in functional libraries. |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | — `not_applicable` | — | — | — | Play Framework is a server-side MVC framework with no client-side state (useState/setState). State is managed via session cookies or database. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap Scala TESTS linkage: scalatest (AnyFunSuite/AnyFlatSpec/AnyWordSpec/AnyFunSpec), specs2, MUnit, ZIO Test leaf cases with subject-from-spec-name (UserServiceSpec->UserService) + body call resolution; Scala assertion/matcher stopwords (assert/assertResult/assertTrue/shouldBe/mustBe/must_==/specs2 matchers). Value-asserting tests in extractor_test.go assert specific test->target edges per framework. Closes #3457. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_scala.go` | Scala def-use sniffer (RegisterDefUseSniffer('scala')) is registered in def_use_scala.go; def_use_pass.go invokes it for all scala entities. File-local val/var/for-generator patterns. |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/scala.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by per-language extractors; Scala import edges are emitted by the Scala extractor pipeline. |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags functions with no effect properties; Scala is a functional language with many pure functions (cats-effect IO, ZIO effects, case class methods). Especially apt for cats-effect, http4s, zio-http. |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_scala.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_scala.go` | Scala payload shapes sniffer handles Play JSON reads (Json.reads[T], Json.format[T]) and case class request bodies. |
| Response shape extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_scala.go` | Scala payload shapes sniffer handles Play Ok(Json.toJson(...)), play.api.libs.json.Json.writes[T] patterns. |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_scala.go` | Payload drift pass compares inferred request/response shapes across commits. Scala payload shapes sniffer provides the shape data for Play. |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_scala.go` | Scala template-pattern sniffer recognises i18n (Messages/messagesApi), log-format (logger.info/warn/error), and SQL literal patterns in Scala source files. |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | — |

### Uncategorized

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: Play ActionBuilder/ActionFilter/ActionRefiner/ActionTransformer auth actions stamp action_kind + action_name + auth_method (jwt/bearer/basic from window). Value-asserting tests. File-local. |
| Middleware coverage | ✅ `full` | — | — | `internal/custom/scala/frameworks.go` | custom_scala_frameworks deep extractor: Play global filter chain via DefaultHttpFilters(...) and def filters=Seq(...) stamp ordered filter_chain; custom EssentialFilter/Filter defs stamped. Value-asserting tests. File-local. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.play ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
