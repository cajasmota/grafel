<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.gql-client` — gql (GraphQL client)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Context extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Data fetching | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Prop extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| State management | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
| Config consumption | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ✅ `full` | `2026-06-11` | 3608 | `internal/engine/http_endpoint_python_graphql_client.go`<br>`internal/engine/http_endpoint_python_graphql_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Python gql package gql(...) documents -> one http_endpoint_call (consumer) per (operation, root field) keyed to the server endpoint shape http:GRAPHQL:/graphql/<RootType>/<field> + FETCHES from the enclosing func (top-level const resolved via the reference site). Root type Query/Mutation/Subscription + root fields parsed from the inline gql document via the shared gqlRootFieldsFromDoc parser (same as the JS/TS + Dart + Swift passes). Identical entity shape to the GraphQL server producers (Strawberry/Graphene/Ariadne/gqlgen/graphql-ruby/HotChocolate) so the Name-based cross-repo linker pairs the Python consumer with the backend resolver on reindex. Value-asserted: gql query GetUser{user} -> http:GRAPHQL:/graphql/Query/user; inline mutation createUser; multi-field query stats+alerts; subscription messageAdded; anonymous shorthand {users{id}} -> Query/users; negative (plain requests.get, no gql) emits nothing. Deferred: cross-file imported gql consts referenced by name only; dynamically-composed query strings. |
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
(or use `go run ./tools/coverage update lang.python.framework.gql-client ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
