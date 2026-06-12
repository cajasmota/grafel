<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.juniper` — juniper

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 4964 | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | 4964 | — | — |
| Endpoint response codes | 🔴 `missing` | — | 4964 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/juniper.go`<br>`internal/custom/rust/juniper_test.go` | #4964: internal/custom/rust/juniper.go synthesizes verb GRAPHQL endpoints from #[graphql_object]/#[graphql_subscription] impl blocks; RootNode::new/Schema::new root captured as SCOPE.Service (paren-balanced arg reader handles EmptyMutation::new()/EmptySubscription::new()). Mirrors the async-graphql extractor. Proven by TestJuniperResolverFields/TestJuniperSchemaRoot/TestJuniperSchemaRoot_EmptyConstructors. |
| Handler attribution | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/juniper.go`<br>`internal/custom/rust/juniper_test.go` | #4964: handler_name=<Root>.<field> attributed per resolver method (Query.user etc). Proven by TestJuniperResolverFields. |
| Route extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/juniper.go`<br>`internal/custom/rust/juniper_test.go` | #4964: each #[graphql_object] impl Query/Mutation + #[graphql_subscription] impl Subscription resolver method becomes a GRAPHQL endpoint at /graphql/<Root>/<field> — EXACT canonical shape async-graphql/gqlgen/Strawberry/Apollo/Absinthe emit so cross-repo client links join; operation kind derived from impl root. Proven by TestJuniperResolverFields. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | 4964 | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 4964 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/juniper.go`<br>`internal/custom/rust/juniper_test.go` | #4964: #[derive(GraphQLObject/GraphQLInputObject)] structs + #[derive(GraphQLEnum)] enums emitted as SCOPE.Schema DTOs with role (object/input/enum). Proven by TestJuniperDTOsAndEnum. |
| Request validation | 🔴 `missing` | — | 4964 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 4964 | — | — |
| Rate limit stamping | 🔴 `missing` | — | 4964 | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | 4964 | — | #4964 deferred: the juniper code-first object-type->type GRAPH_RELATES graph (mirroring graphql_codefirst_typegraph.go for async-graphql) is not yet built; DTOs are catalogued but field->type edges are not emitted. Follow-up filed. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/juniper.go`<br>`internal/custom/rust/juniper_test.go` | #4964: #[derive(GraphQLEnum)] GraphQL enums recovered as DTOs (role=enum). Proven by TestJuniperDTOsAndEnum. |
| Interface extraction | ✅ `full` | `2026-06-12` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level rust extractor (rust.go, unconditional per-language) emits trait_item -> SCOPE.Component subtype="trait" with methods/supertraits/generics + EXTENDS edges for every .rs file, juniper included. |
| Type alias extraction | ✅ `full` | `2026-06-12` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level rust extractor (rust.go) emits type_item -> SCOPE.Component subtype="type_alias" with aliased_type/generics for every .rs file, juniper included. |
| Type extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/juniper.go`<br>`internal/custom/rust/juniper_test.go` | #4964: juniper GraphQL DTO type names recovered from GraphQLObject/GraphQLInputObject derive macros. Proven by TestJuniperDTOsAndEnum. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 4964 | — | — |
| DI injection point | 🔴 `missing` | — | 4964 | — | — |
| DI scope resolution | 🔴 `missing` | — | 4964 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 4964 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 4964 | — | — |
| Metric extraction | 🔴 `missing` | — | 4964 | — | — |
| Trace extraction | 🔴 `missing` | — | 4964 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 4964 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 4964 | — | — |
| Config consumption | 🔴 `missing` | — | 4964 | — | — |
| Constant propagation | 🔴 `missing` | — | 4964 | — | — |
| Dead code detection | 🔴 `missing` | — | 4964 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 4964 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 4964 | — | — |
| Error flow | ✅ `full` | `2026-06-12` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Framework-agnostic rust error-flow pass (Err/bail!/ensure!/.ok_or -> THROWS, match/if let Err/.map_err -> CATCHES) fires on juniper resolver bodies like any .rs file. |
| Feature flag gating | 🔴 `missing` | — | 4964 | — | — |
| Fs effect | 🔴 `missing` | — | 4964 | — | — |
| HTTP effect | 🔴 `missing` | — | 4964 | — | — |
| Import resolution quality | 🔴 `missing` | — | 4964 | — | — |
| Module cycle detection | 🔴 `missing` | — | 4964 | — | — |
| Mutation effect | 🔴 `missing` | — | 4964 | — | — |
| Pure function tagging | 🔴 `missing` | — | 4964 | — | — |
| Reachability analysis | 🔴 `missing` | — | 4964 | — | — |
| Request shape extraction | 🔴 `missing` | — | 4964 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 4964 | — | — |
| Response shape extraction | 🔴 `missing` | — | 4964 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 4964 | — | — |
| Schema drift detection | 🔴 `missing` | — | 4964 | — | — |
| Taint sink detection | 🔴 `missing` | — | 4964 | — | — |
| Taint source detection | 🔴 `missing` | — | 4964 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 4964 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 4964 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.juniper ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
