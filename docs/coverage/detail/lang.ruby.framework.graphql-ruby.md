<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.framework.graphql-ruby` — graphql-ruby (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 42

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3621 | `internal/engine/http_endpoint_graphql_ruby.go`<br>`internal/engine/http_endpoint_graphql_ruby_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/httproutes/canonicalize.go` | synthesizeGraphQLRuby emits http:GRAPHQL:/graphql/<Query|Mutation|Subscription>/<field> per `field :name` on a root operation type class (QueryType/MutationType/SubscriptionType, subclasses of *BaseObject or GraphQL::Schema::Object) — EXACT canonical shape as gqlgen (Go) / Strawberry (Python) / HotChocolate (C#) / Apollo (JS) / Absinthe so client links + cross-repo linker join. Field name = the Ruby `field :name` snake_case symbol verbatim (graphql-ruby keeps snake_case on the wire). Value-asserting tests assert the EXACT endpoint ids for Query/users, Query/user, Mutation/create_user, Mutation/delete_user, Subscription/user_added. |
| Handler attribution | ✅ `full` | `2026-06-02` | 3621 | `internal/engine/http_endpoint_graphql_ruby.go`<br>`internal/engine/http_endpoint_graphql_ruby_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Each `field :name` is attributed to its same-name resolver method `def name` on the type class via source_handler=SCOPE.Operation:<field> plus a same-file handler_file hint; the resolver post-pass rebinds it to a HANDLES edge against the extracted Ruby method entity. Value-asserted in the tests. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3621 | `internal/engine/http_endpoint_graphql_ruby.go`<br>`internal/engine/http_endpoint_graphql_ruby_test.go`<br>`internal/engine/httproutes/canonicalize.go` | Operation endpoints synthesised from `field :name` declarations on the convention-named root type classes. Honest-partial: keys on the conventional QueryType/MutationType/SubscriptionType class names rather than the schema's query(...)/mutation(...) registration, and resolves the default same-name `def` resolver — does not yet follow `resolver: SomeResolver` / `method:` field overrides or dynamically generated fields. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3621 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3621 | — | — |
| Request validation | 🔴 `missing` | — | 3621 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3621 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3621 | — | — |
| Interface extraction | 🔴 `missing` | — | 3621 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3621 | — | — |
| Type extraction | 🔴 `missing` | — | 3621 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3621 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3621 | — | — |
| Metric extraction | 🔴 `missing` | — | 3621 | — | — |
| Trace extraction | 🔴 `missing` | — | 3621 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3621 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3621 | — | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/ruby/config_consumer.go`<br>`internal/extractors/ruby/config_consumer_test.go` | ENV[...], ENV.fetch -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | 🔴 `missing` | — | 3621 | — | — |
| Dead code detection | 🔴 `missing` | — | 3621 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3621 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3621 | — | — |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | 3621 | — | — |
| HTTP effect | 🔴 `missing` | — | 3621 | — | — |
| Import resolution quality | 🔴 `missing` | — | 3621 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3621 | — | — |
| Mutation effect | 🔴 `missing` | — | 3621 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3621 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3621 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3621 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3621 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3621 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3621 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3621 | — | — |
| Taint source detection | 🔴 `missing` | — | 3621 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3621 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3621 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.framework.graphql-ruby ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
