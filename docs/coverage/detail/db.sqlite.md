<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.sqlite` — SQLite (schema)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/orm_queries.go` | — |
| Resource extraction | 🟢 `partial` | `2026-05-28` | — | `internal/extractors/sql` | — |

## Code-level coverage

This infra record tracks datastore-level extraction (resources, dependency
attribution). The deep, code-level coverage for this technology lives in the
per-language driver/ORM records below — each one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.csharp.driver.sqlite`](./lang.csharp.driver.sqlite.md) | C# | driver | 4 missing, 7 n/a |
| [`lang.elixir.orm.ecto-sqlite3`](./lang.elixir.orm.ecto-sqlite3.md) | elixir | orm | 6 full, 1 partial, 3 missing, 1 n/a |
| [`lang.go.driver.sqlite`](./lang.go.driver.sqlite.md) | go | driver | 1 full, 3 partial, 3 missing, 4 n/a |
| [`lang.jsts.driver.sqlite`](./lang.jsts.driver.sqlite.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.sqlite`](./lang.php.driver.sqlite.md) | php | driver | 1 partial, 4 missing, 6 n/a |
| [`lang.python.driver.sqlite`](./lang.python.driver.sqlite.md) | python | driver | 1 full, 1 partial, 3 missing, 6 n/a |
| [`lang.ruby.driver.sqlite`](./lang.ruby.driver.sqlite.md) | ruby | driver | 4 missing, 7 n/a |
| [`lang.rust.driver.sqlite`](./lang.rust.driver.sqlite.md) | rust | driver | 4 missing, 7 n/a |
| [`lang.rust.orm.rusqlite`](./lang.rust.orm.rusqlite.md) | rust | orm | 1 full, 3 missing, 7 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.sqlite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
