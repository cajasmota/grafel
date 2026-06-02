<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.mongodb` — MongoDB (collections)

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
| [`lang.c-cpp.driver.mongocxx`](./lang.c-cpp.driver.mongocxx.md) | C/C++ | driver | 2 full, 1 partial, 3 missing, 5 n/a |
| [`lang.csharp.driver.mongodb`](./lang.csharp.driver.mongodb.md) | C# | driver | 1 partial, 4 missing, 6 n/a |
| [`lang.elixir.driver.mongodb`](./lang.elixir.driver.mongodb.md) | elixir | driver | 4 missing, 7 n/a |
| [`lang.go.driver.mongodb`](./lang.go.driver.mongodb.md) | go | driver | 3 partial, 3 missing, 5 n/a |
| [`lang.java.driver.mongodb`](./lang.java.driver.mongodb.md) | java | driver | 1 full, 3 missing, 7 n/a |
| [`lang.java.orm.spring-data-mongo`](./lang.java.orm.spring-data-mongo.md) | java | orm | 6 missing, 5 n/a |
| [`lang.jsts.driver.mongodb`](./lang.jsts.driver.mongodb.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.jsts.orm.mongoose`](./lang.jsts.orm.mongoose.md) | JS/TS | orm | 3 full, 2 partial, 3 missing, 3 n/a |
| [`lang.kotlin.orm.mongodb`](./lang.kotlin.orm.mongodb.md) | kotlin | orm | 2 full, 3 missing, 6 n/a |
| [`lang.php.driver.mongodb`](./lang.php.driver.mongodb.md) | php | driver | 1 full, 3 missing, 7 n/a |
| [`lang.python.driver.mongodb`](./lang.python.driver.mongodb.md) | python | driver | 1 full, 3 missing, 7 n/a |
| [`lang.python.orm.beanie`](./lang.python.orm.beanie.md) | python | orm | 5 partial, 4 missing, 2 n/a |
| [`lang.python.orm.mongoengine`](./lang.python.orm.mongoengine.md) | python | orm | 5 partial, 4 missing, 2 n/a |
| [`lang.ruby.driver.mongodb`](./lang.ruby.driver.mongodb.md) | ruby | driver | 1 full, 3 missing, 7 n/a |
| [`lang.ruby.orm.mongoid`](./lang.ruby.orm.mongoid.md) | ruby | orm | 5 partial, 4 missing, 2 n/a |
| [`lang.rust.driver.mongodb`](./lang.rust.driver.mongodb.md) | rust | driver | 1 full, 3 missing, 7 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.mongodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
