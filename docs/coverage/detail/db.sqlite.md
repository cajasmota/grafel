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
| Resource extraction | 🟢 `partial` | `2026-06-11` | 4295 | `internal/extractors/sql/sql.go`<br>`internal/extractors/sql/sql_4295_test.go` | Offline DDL extraction (#4295): CREATE TABLE → SCOPE.Datastore table + SCOPE.Schema column members (CONTAINS) carrying col_type, nullable, is_primary_key, is_unique, default flags. FK edges (inline REFERENCES, table-level FOREIGN KEY, ALTER TABLE ADD CONSTRAINT) → column→table REFERENCES; query-only .sql mints no table (negative test). Partial pending LIVE information_schema introspection (follow-up to #4295) and migration-DSL forms (raw .sql + ALTER covered). |

## Related extraction records

This hub record tracks the technology at a high level. The deep, code-level
coverage for this technology lives in the per-language records below — each
one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.c-cpp.orm.sqlite-direct-c-api`](./lang.c-cpp.orm.sqlite-direct-c-api.md) | C/C++ | orm | 1 partial, 5 missing, 5 n/a |
| [`lang.c-cpp.orm.sqlitecpp`](./lang.c-cpp.orm.sqlitecpp.md) | C/C++ | orm | 1 partial, 5 missing, 5 n/a |
| [`lang.csharp.driver.sqlite`](./lang.csharp.driver.sqlite.md) | C# | driver | 4 missing, 7 n/a |
| [`lang.elixir.orm.ecto-sqlite3`](./lang.elixir.orm.ecto-sqlite3.md) | elixir | orm | 6 full, 1 partial, 3 missing, 1 n/a |
| [`lang.go.driver.sqlite`](./lang.go.driver.sqlite.md) | go | driver | 1 full, 3 partial, 3 missing, 4 n/a |
| [`lang.jsts.driver.sqlite`](./lang.jsts.driver.sqlite.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.sqlite`](./lang.php.driver.sqlite.md) | php | driver | 1 full, 1 partial, 3 missing, 6 n/a |
| [`lang.python.driver.sqlite`](./lang.python.driver.sqlite.md) | python | driver | 1 full, 1 partial, 3 missing, 6 n/a |
| [`lang.ruby.driver.sqlite`](./lang.ruby.driver.sqlite.md) | ruby | driver | 1 full, 3 missing, 7 n/a |
| [`lang.rust.driver.sqlite`](./lang.rust.driver.sqlite.md) | rust | driver | 1 full, 3 missing, 7 n/a |
| [`lang.rust.orm.rusqlite`](./lang.rust.orm.rusqlite.md) | rust | orm | 1 full, 3 missing, 7 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.sqlite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
