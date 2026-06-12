<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.swift.framework.alamofire` — Alamofire (HTTP client)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [swift](../by-language/swift.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 37

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Navigation extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Screen detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| State management | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | — | 4913 | `internal/extractors/swift/types.go`<br>`internal/extractors/swift/types_test.go` | #4913: the base Swift tree-sitter extractor (types.go, buildEnumValueSet) now emits a SCOPE.Enum value-set per `enum` declaration via extractor.EnumEntity(kind_hint=swift_enum) IN ADDITION to the nominal SCOPE.Component(enum) — one member per `case` identifier (comma-grouped `case a, b` -> two members), with `case x = <literal>` raw values (int/string/bool) lifted to member values. Value-asserted: TestSwiftTypes_PlainEnumValueSet (Direction -> north,south,east,west + Component survives), TestSwiftTypes_RawValueEnum (HTTPStatus -> ok=200,notFound=404), TestSwiftTypes_StringRawValueEnum (quote-stripped). PARTIAL: associated-value case payload types and computed raw values are not modelled. |
| Interface extraction | 🔴 `missing` | — | 4913 | — | #4913: Swift has no `interface` keyword — the nearest construct is `protocol`, which IS extracted as SCOPE.Component(subtype=protocol) by the base walk (swift.go protocol_declaration), but it is not emitted as a type-system interface-alias node, so this dictionary cell stays missing. See lang.swift.base core_extraction for the protocol Component coverage. |
| Type alias extraction | 🟢 `partial` | — | 4913 | `internal/extractors/swift/types.go`<br>`internal/extractors/swift/types_test.go` | #4913: `typealias Name = <type>` (types.go, buildTypeAlias) now emits SCOPE.Schema(subtype=type_alias) with type_body, parity with the python/rust/go/dart type_alias shape, via tree-sitter typealias_declaration so the full RHS (function types `(Int) -> Void`, generics) is captured — superseding the loose vapor-only reSwiftTypealias->Component v1. Value-asserted: TestSwiftTypes_TypeAlias (UserID -> Int, Handler -> (Int) -> Void). PARTIAL: generic where-clause and protocol-composition aliases are stored as raw type_body text without further decomposition. |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ✅ `full` | — | — | `internal/engine/http_endpoint_swift_client.go`<br>`internal/engine/http_endpoint_swift_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Alamofire AF.request("...", method: .verb) / session.request(...) outbound calls -> one http_endpoint_call (consumer) per call site + FETCHES from the enclosing func; host stripped, \(expr) -> {param}; verb from method: .verb (default GET). Same entity shape as the backend producer so the cross-repo linker pairs the iOS screen with the backend route on reindex. Value-asserted in http_endpoint_swift_client_test.go (AF.request("/auth/login", method: .post) -> POST /auth/login). |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.swift.framework.alamofire ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
