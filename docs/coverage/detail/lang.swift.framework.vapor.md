<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.swift.framework.vapor` — Vapor

Auto-generated. Back to [summary](../summary.md).

- **Language:** [swift](../by-language/swift.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor.go` | vapor.go emits SCOPE.Operation entities with http_method and route_path properties from Vapor route registrations; the cross-repo http_pass.go can match these endpoints for cross-link synthesis; proven by TestVaporRoute. |
| Handler attribution | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/swift/vapor.go` | vapor.go emits RouteCollection controller entities (SCOPE.Component/controller) and links routes to file context; full handler-to-function attribution requires resolving trailing closures back to named handler functions which is not yet implemented. |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor.go` | vapor.go custom extractor handles app.get/post/put/delete/patch/options route registrations, RouteCollection conformances, and .grouped prefix declarations; emits SCOPE.Operation/endpoint with http_method and route_path properties; proven by TestVaporRoute and TestVaporRouteCollection. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor_extended.go` | vapor_extended.go extracts BearerAuthenticator/BasicAuthenticator/JWTAuthenticator protocol conformances, req.auth.require/get call sites, and .grouped(AuthMiddleware) protected route groups; proven by TestVaporExtendedAuth. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor_extended.go`<br>`internal/substrate/payload_shapes_t3.go` | vapor_extended.go extracts Validatable-conforming types and req.content.decode(T.self) DTO usage; payload_shapes_t3.go extracts Codable/Content-conforming struct fields; proven by TestVaporExtendedValidation. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor_extended.go` | vapor_extended.go extracts Vapor Validatable protocol conformances and validations.add() rule call sites; proven by TestVaporExtendedValidation showing both Validatable struct extraction and validations.add pattern detection. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor.go`<br>`internal/custom/swift/vapor_extended.go` | vapor.go extracts Middleware protocol conformances (struct/class conforming to Middleware); vapor_extended.go extracts .grouped(XMiddleware, YMiddleware) middleware chains from route group definitions; proven by TestVaporRouteCollection and TestVaporExtendedMiddleware. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor.go`<br>`internal/extractors/swift/swift.go` | swift.go (tree-sitter extractor) handles enum declarations via class_declaration node with enum keyword, emitting SCOPE.Component/subtype=enum; proven by existing test corpus. |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/swift/swift.go` | swift.go (tree-sitter extractor) handles protocol_declaration nodes, emitting SCOPE.Component/subtype=protocol; Swift protocols are the idiomatic interface/trait construct; proven by existing test in swift_test.go. |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor_extended.go` | vapor_extended.go (custom extractor) handles Swift typealias declarations via regex, emitting SCOPE.Component/subtype=typealias with alias_target property; proven by TestVaporExtendedTypeAlias covering public/internal/bare typealias forms. |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/swift/swift.go` | swift.go (tree-sitter extractor) handles class_declaration nodes for class and struct subtypes, emitting SCOPE.Component/subtype=class|struct; Fluent @Model classes additionally handled by vapor.go custom extractor; proven by existing test corpus. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor_extended.go`<br>`internal/substrate/entry_points_swift.go` | vapor_extended.go extracts XCTestCase subclasses, test function declarations, and Application.testable() bootstrap; entry_points_swift.go marks test functions as EntryKindTestEntry for reachability seeding; XCTAssert call sites are also captured; proven by TestVaporExtendedTesting. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor_extended.go`<br>`internal/substrate/template_pattern_swift.go` | vapor_extended.go extracts req.logger.info/error/debug, app.logger, and OSLog calls; template_pattern_swift.go extracts print/NSLog/os.Logger format strings as TemplateKindLog; proven by TestVaporExtendedObservabilityLog. |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/swift/vapor_extended.go` | vapor_extended.go extracts swift-metrics Counter(label:), Gauge(label:), Timer(label:), and Histogram(label:) metric declarations; these are the canonical swift-metrics / Prometheus client patterns used in Vapor apps; proven by TestVaporExtendedObservabilityMetric. |
| Trace extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/swift/vapor_extended.go` | vapor_extended.go extracts InstrumentationSystem.tracer, Tracer.withSpan, and span.setAttribute/addEvent/end calls from swift-distributed-tracing API; partial because span propagation context and baggage extraction require more complete tracer integration. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-30` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | Confidence overlay is language-agnostic infrastructure applied at graph-query time; all Swift entities receive confidence scores from the same overlay mechanism as all other languages. |
| Constant propagation | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/swift.go` | constant_propagation.go is language-agnostic; swift.go (substrate) provides Swift-specific literal let bindings (let X = literal), static let namespace bindings, and ProcessInfo.processInfo.environment env-fallback patterns; partial because complex Swift computed properties are not statically resolved. |
| DB effect | 🟢 `partial` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| Dead code detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_swift.go` | reachability.go BFS from entry-points to detect unreachable entities; entry_points_swift.go (new) provides Swift-specific sniffers for @main, static func main, Vapor lifecycle hooks (configure/boot), XCTest methods, and public/open exported functions; partial because comprehensive dead-code detection requires full Swift module resolution. |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_swift.go` | def_use_pass.go runs on all indexed languages including Swift; def_use_swift.go provides Swift-specific let/var/identifier sniffers; partial because comprehensive Swift pattern coverage requires broader test corpus. |
| Env fallback recognition | ✅ `full` | `2026-05-30` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/swift.go` | swift.go (substrate) explicitly recognises ProcessInfo.processInfo.environment[KEY] ?? default as ProvenanceEnvFallback with confidence 0.85; constant_propagation.go promotes these bindings into the graph; this is the canonical Swift env-fallback idiom. |
| Fs effect | 🟢 `partial` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| HTTP effect | 🟢 `partial` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| Import resolution quality | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/swift.go` | constant_propagation.go resolves cross-file bindings including import statements; swift.go (substrate) sniffs Swift import declarations (import Module, import struct Foundation.Date) and maps them to local names; partial because Swift's module-system scoping requires Package.swift integration for full cross-module resolution. |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | module_cycle_pass.go is language-agnostic and runs on Swift IMPORTS edges; partial because Swift module-level granularity is coarser than package-level and cross-module cycles require framework-specific configuration. |
| Mutation effect | 🟢 `partial` | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | pure_function_pass.go is language-agnostic and tags functions with no observed effect-sinks as pure; Swift effect_sinks_swift.go feeds the effect graph; partial because Swift async/await patterns can obscure purity. |
| Reachability analysis | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_swift.go` | reachability.go BFS seeds from entry-points and walks CALLS edges; entry_points_swift.go provides @main, Vapor boot/configure lifecycle hooks, XCTest entry points, and public/open exported functions; partial because full Swift module-boundary traversal requires package manifest integration. |
| Request shape extraction | ✅ `full` | `2026-05-30` | — | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_t3.go` | payload_shapes_t3.go provides full Swift/Vapor sniffer: req.content.decode(T.self) + Codable/Content-conforming struct field extraction; payload_drift.go cross-references producer/consumer shapes. |
| Response shape extraction | ✅ `full` | `2026-05-30` | — | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_t3.go` | payload_shapes_t3.go sniffer captures struct-literal return shapes (return T(field:v)) from Encodable/Content-conforming types; payload_drift.go cross-references producer/consumer shapes. |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-30` | — | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_t3.go` | payload_drift.go consumes the Swift payload shape records emitted by payload_shapes_t3.go and cross-references producer request fields against consumer field sets; schema drift findings are emitted as SchemaDrift entities. |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_swift.go` | template_pattern_pass.go is language-agnostic; template_pattern_swift.go provides Swift-specific i18n (NSLocalizedString/Text), log-format (print/NSLog/os.Logger), and SQL literal sniffers; partial because Vapor Leaf template renders require full-corpus coverage. |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.swift.framework.vapor ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
