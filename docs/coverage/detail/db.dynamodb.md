<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.dynamodb` — AWS DynamoDB

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/orm_queries_other.go` | — |
| Resource extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/iac_sns_edges.go` | — |

## Code-level coverage

This infra record tracks datastore-level extraction (resources, dependency
attribution). The deep, code-level coverage for this technology lives in the
per-language driver/ORM records below — each one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.csharp.driver.dynamodb`](./lang.csharp.driver.dynamodb.md) | C# | driver | 1 partial, 4 missing, 6 n/a |
| [`lang.elixir.driver.dynamodb`](./lang.elixir.driver.dynamodb.md) | elixir | driver | 4 missing, 7 n/a |
| [`lang.go.driver.dynamodb`](./lang.go.driver.dynamodb.md) | go | driver | 3 partial, 3 missing, 5 n/a |
| [`lang.java.orm.dynamodb`](./lang.java.orm.dynamodb.md) | java | orm | 2 full, 1 partial, 3 missing, 5 n/a |
| [`lang.jsts.driver.dynamodb`](./lang.jsts.driver.dynamodb.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.dynamodb`](./lang.php.driver.dynamodb.md) | php | driver | 1 full, 3 missing, 7 n/a |
| [`lang.python.driver.dynamodb`](./lang.python.driver.dynamodb.md) | python | driver | 1 full, 3 missing, 7 n/a |
| [`lang.ruby.driver.dynamodb`](./lang.ruby.driver.dynamodb.md) | ruby | driver | 1 full, 3 missing, 7 n/a |
| [`lang.rust.driver.dynamodb`](./lang.rust.driver.dynamodb.md) | rust | driver | 4 missing, 7 n/a |
| [`lang.scala.orm.scanamo`](./lang.scala.orm.scanamo.md) | scala | orm | 1 full, 2 partial, 3 missing, 5 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.dynamodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
