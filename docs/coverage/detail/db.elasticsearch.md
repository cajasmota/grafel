<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.elasticsearch` — Elasticsearch (indices)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/orm_queries_other.go` | — |
| Resource extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/orm_queries_other.go` | — |

## Code-level coverage

This infra record tracks datastore-level extraction (resources, dependency
attribution). The deep, code-level coverage for this technology lives in the
per-language driver/ORM records below — each one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.csharp.driver.elastic`](./lang.csharp.driver.elastic.md) | C# | driver | 1 partial, 4 missing, 6 n/a |
| [`lang.elixir.driver.elastic`](./lang.elixir.driver.elastic.md) | elixir | driver | 4 missing, 7 n/a |
| [`lang.go.driver.elastic`](./lang.go.driver.elastic.md) | go | driver | 4 partial, 3 missing, 4 n/a |
| [`lang.java.orm.spring-data-elastic`](./lang.java.orm.spring-data-elastic.md) | java | orm | 6 missing, 5 n/a |
| [`lang.jsts.driver.elastic`](./lang.jsts.driver.elastic.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.elastic`](./lang.php.driver.elastic.md) | php | driver | 1 full, 3 missing, 7 n/a |
| [`lang.python.driver.elastic`](./lang.python.driver.elastic.md) | python | driver | 1 full, 3 missing, 7 n/a |
| [`lang.ruby.driver.elastic`](./lang.ruby.driver.elastic.md) | ruby | driver | 1 partial, 4 missing, 6 n/a |
| [`lang.rust.driver.elastic`](./lang.rust.driver.elastic.md) | rust | driver | 4 missing, 7 n/a |
| [`lang.scala.orm.elastic4s`](./lang.scala.orm.elastic4s.md) | scala | orm | 1 full, 2 partial, 3 missing, 5 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.elasticsearch ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
