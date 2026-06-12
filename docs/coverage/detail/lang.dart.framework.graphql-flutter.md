<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.dart.framework.graphql-flutter` — graphql_flutter (GraphQL client)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [dart](../by-language/dart.md)
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
| Enum extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: the base Dart extractor (types.go, dartEnums) now emits a SCOPE.Enum value-set node per `enum` declaration via extractor.EnumEntity(kind_hint=dart_enum) — plain enums and Dart-2.17 enhanced enums (constants-before-`;` kept as members, single-literal ctor args lifted to values). Value-asserted: TestDartTypes_PlainEnum (Color -> red,green,blue), TestDartTypes_EnhancedEnum (Planet -> mercury,earth, post-`;` fields/methods dropped). PARTIAL: regex over canonical declarations; computed/multi-arg ctor values are not resolved. |
| Interface extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: Dart-3 class modifiers (types.go, dartModifiedClasses) — `sealed`/`base`/`interface`/`final`/`mixin class` — are now extracted as SCOPE.Component(class) carrying Properties{class_modifier, dart_sealed, dart_interface}; the base classRE only matched plain/`abstract` classes so these (incl. `interface class`, Dart's nominal-interface form) were invisible. Value-asserted: TestDartTypes_SealedClass (sealed Shape -> dart_sealed=true; interface Drawable -> dart_interface=true). PARTIAL: subtype permits-clause / exhaustiveness graph wiring not modelled; Dart has no standalone `interface` keyword (interfaces are implicit via `implements`). |
| Type alias extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: `typedef` (types.go, dartTypedefs) now emits SCOPE.Schema(subtype=type_alias) with type_body — both the modern `typedef Name = <type>;` (Dart 2.13+) and the legacy function-type `typedef Ret Name(params);` spellings, parity with the python/rust/go type_alias shape. Value-asserted: TestDartTypes_TypedefAlias (JsonMap -> Map<String, dynamic>), TestDartTypes_TypedefFunc (Comparator). PARTIAL: regex over canonical one-line forms. |

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
| HTTP effect | ✅ `full` | `2026-06-03` | 4036 | `internal/engine/http_endpoint_dart_client.go`<br>`internal/engine/http_endpoint_dart_graphql_client.go`<br>`internal/engine/http_endpoint_dart_graphql_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | graphql_flutter / graphql gql(r'''...''') documents -> one http_endpoint_call (consumer) per (operation, root field) keyed to the server endpoint shape http:GRAPHQL:/graphql/<RootType>/<field> + FETCHES from the enclosing func (top-level const resolved via reference site). Root type Query/Mutation/Subscription + root fields parsed from the inline gql document (shared gqlRootFieldsFromDoc with the JS/TS pass). Identical entity shape to the GraphQL server producer so the cross-repo linker pairs the Flutter screen with the backend resolver on reindex. Value-asserted: gql(r'''query GetUser { user { id name } }''') -> http:GRAPHQL:/graphql/Query/user; inline mutation createUser; multi-field query; subscription messageAdded; negatives (REST dio.get, plain string) emit nothing. |
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

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.dart.framework.graphql-flutter ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
