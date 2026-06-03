<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.framework.graphql-ruby` — graphql-ruby (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3621 | `internal/engine/http_endpoint_graphql_ruby.go`<br>`internal/engine/http_endpoint_graphql_ruby_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/httproutes/canonicalize.go` | synthesizeGraphQLRuby emits http:GRAPHQL:/graphql/<Query|Mutation|Subscription>/<field> per `field :name` on a root operation type class (QueryType/MutationType/SubscriptionType, subclasses of *BaseObject or GraphQL::Schema::Object) — EXACT canonical shape as gqlgen (Go) / Strawberry (Python) / HotChocolate (C#) / Apollo (JS) / Absinthe so client links + cross-repo linker join. Field name = the Ruby `field :name` snake_case symbol verbatim (graphql-ruby keeps snake_case on the wire). Value-asserting tests assert the EXACT endpoint ids for Query/users, Query/user, Mutation/create_user, Mutation/delete_user, Subscription/user_added. |
| Handler attribution | ✅ `full` | `2026-06-02` | 3621 | `internal/engine/http_endpoint_graphql_ruby.go`<br>`internal/engine/http_endpoint_graphql_ruby_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Each `field :name` is attributed to its same-name resolver method `def name` on the type class via source_handler=SCOPE.Operation:<field> plus a same-file handler_file hint; the resolver post-pass rebinds it to a HANDLES edge against the extracted Ruby method entity. Value-asserted in the tests. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3621 | `internal/engine/http_endpoint_graphql_ruby.go`<br>`internal/engine/http_endpoint_graphql_ruby_test.go`<br>`internal/engine/httproutes/canonicalize.go` | Operation endpoints synthesised from `field :name` declarations on the convention-named root type classes. Honest-partial: keys on the conventional QueryType/MutationType/SubscriptionType class names rather than the schema's query(...)/mutation(...) registration, and resolves the default same-name `def` resolver — does not yet follow `resolver: SomeResolver` / `method:` field overrides or dynamically generated fields. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

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
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | — | `internal/custom/ruby/rate_limit_endpoint.go`<br>`internal/custom/ruby/rate_limit_endpoint_test.go` | rack-attack 'Rack::Attack.throttle(name, limit: N, period: T)' detected and stamped as a SCOPE.Pattern/rate_limit marker (rate_limited/limit/period/rate_limit_name/rate_limit_source=rack-attack; literal period 1.minute->60 -> rate_limit '<N>/<secs>s'). Rack middleware applies to this Rack-based framework. PARTIAL: rack-attack throttles bind to a request discriminator (the block), not a named route, so the per-route binding is heuristic (rate_limit_scope=request); blocklist/safelist are not stamped as limits. Framework-native limiter idioms are future work. #4072 |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | 3804 | — | GraphQL object-type→type graph applies (this is a GraphQL server) but is not yet implemented for this framework/language; SDL servers are covered by internal/extractors/graphql/type_graph.go (#3805) and the TS/Python code-first set (TypeGraphQL/Nexus/Pothos/Strawberry/graphene) by the code-first type-graph extractors. This lane is the remaining backfill for other-language GraphQL frameworks. |

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
| DB effect | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | Ruby effect sniffer fires db_read (User.find_by/.where) + db_write (User.create!) on graphql-ruby resolver bodies, bound to the resolver def. Framework-agnostic per-language dispatch (#3948). |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3621 | — | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/ruby/config_consumer.go`<br>`internal/extractors/ruby/config_consumer_test.go` | ENV[...], ENV.fetch -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | 🔴 `missing` | — | 3621 | — | — |
| Dead code detection | 🔴 `missing` | — | 3621 | — | — |
| Def use chain extraction | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_ruby.go` | Per-LANGUAGE Ruby def-use sniffer fires on graphql-ruby resolver .rb bodies (def_use_pass.go dispatches by file language, framework-agnostic). Probed: record def->use chain inside a field resolver (#3948). |
| Env fallback recognition | 🔴 `missing` | — | 3621 | — | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/ruby/exception_flow.go`<br>`internal/extractors/ruby/exception_flow_test.go` | raise X / raise Mod::X -> THROWS; rescue A, B => e / method-level rescue / Rails rescue_from A, B, with: -> CATCHES; bare rescue catch-all + string raise + bare re-raise dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | fs_read/fs_write effect (File.read/.write) fires on graphql-ruby resolver file I/O. Per-language Ruby sniffer (#3948). |
| HTTP effect | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | http_out effect (Net::HTTP/HTTParty/Faraday) fires when a graphql-ruby resolver calls an HTTP client. Per-language Ruby sniffer (#3948). |
| Import resolution quality | 🔴 `missing` | — | 3621 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3621 | — | — |
| Mutation effect | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | mutation effect (@ivar assignment) fires inside graphql-ruby resolver methods. Per-language Ruby sniffer (#3948). |
| Pure function tagging | 🔴 `missing` | — | 3621 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3621 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3621 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3621 | — | — |
| Sanitizer recognition | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | Sanitizer recognition (AR placeholder where with bound value) fires on graphql-ruby resolvers. Per-language Ruby sniffer (#3948). |
| Schema drift detection | 🔴 `missing` | — | 3621 | — | — |
| Taint sink detection | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | Taint SINK (SQL interpolation in where) fires when a graphql-ruby resolver builds raw SQL from a field argument. Per-language Ruby sniffer (#3948). |
| Taint source detection | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | Taint SOURCE (JSON.parse of non-literal etc.) fires in graphql-ruby resolver bodies. Resolvers read keyword args not params, so params-style sources do not apply (#3948). |
| Template pattern catalog | 🔴 `missing` | — | 3621 | — | — |
| Vulnerability finding | 🟢 `partial` | `2026-06-02` | 3948 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | Vulnerability findings derive from the taint source->sink flow proven for graphql-ruby resolvers. Per-language Ruby sniffer (#3948). |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.framework.graphql-ruby ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
