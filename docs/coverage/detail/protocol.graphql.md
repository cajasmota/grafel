<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.graphql` — GraphQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | 🟢 `partial` | `2026-06-02` | — | `internal/engine/http_endpoint_graphql_client.go`<br>`internal/engine/http_endpoint_match.go` | — |
| Method attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/graphql_subscriptions.go` | — |
| Service extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/graphql_subscriptions.go`<br>`internal/engine/rules/graphql/frameworks/graphql_schema.yaml` | — |

## Related extraction records

This hub record tracks the technology at a high level. The deep, code-level
coverage for this technology lives in the per-language records below — each
one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.csharp.framework.hotchocolate`](./lang.csharp.framework.hotchocolate.md) | C# | framework | 4 full, 3 partial, 42 missing |
| [`lang.dart.framework.graphql-flutter`](./lang.dart.framework.graphql-flutter.md) | dart | framework | 1 full, 36 missing |
| [`lang.elixir.framework.absinthe`](./lang.elixir.framework.absinthe.md) | elixir | framework | 6 full, 31 partial, 12 missing |
| [`lang.go.framework.gqlgen`](./lang.go.framework.gqlgen.md) | go | framework | 4 full, 8 partial, 37 missing |
| [`lang.java.framework.spring-graphql`](./lang.java.framework.spring-graphql.md) | java | framework | 8 full, 5 partial, 42 missing |
| [`lang.jsts.framework.graphql-resolvers`](./lang.jsts.framework.graphql-resolvers.md) | JS/TS | framework | 10 full, 19 partial, 25 missing, 1 n/a |
| [`lang.jsts.framework.type-graphql`](./lang.jsts.framework.type-graphql.md) | JS/TS | framework | 4 full, 5 partial, 40 missing |
| [`lang.kotlin.framework.graphql-kotlin`](./lang.kotlin.framework.graphql-kotlin.md) | kotlin | framework | 4 full, 51 missing |
| [`msg.graphql-subscriptions`](./msg.graphql-subscriptions.md) | multi |  | 3 full |
| [`lang.php.framework.api-platform-graphql`](./lang.php.framework.api-platform-graphql.md) | php | framework | 7 full, 42 missing |
| [`lang.php.framework.graphql-php`](./lang.php.framework.graphql-php.md) | php | framework | 7 full, 42 missing |
| [`lang.python.framework.graphene`](./lang.python.framework.graphene.md) | python | framework | 15 full, 24 partial, 10 missing |
| [`lang.python.framework.strawberry-graphql`](./lang.python.framework.strawberry-graphql.md) | python | framework | 16 full, 23 partial, 10 missing |
| [`lang.ruby.framework.graphql-ruby`](./lang.ruby.framework.graphql-ruby.md) | ruby | framework | 3 full, 10 partial, 36 missing |
| [`lang.rust.framework.async-graphql`](./lang.rust.framework.async-graphql.md) | rust | framework | 11 full, 3 partial, 35 missing |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.graphql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
