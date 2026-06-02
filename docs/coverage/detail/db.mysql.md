<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.mysql` — MySQL / MariaDB (schema)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/orm_queries.go` | — |
| Resource extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/database_index/language.yaml`<br>`internal/extractors/sql` | — |

## Code-level coverage

This infra record tracks datastore-level extraction (resources, dependency
attribution). The deep, code-level coverage for this technology lives in the
per-language driver/ORM records below — each one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.c-cpp.driver.mysql-connector-cpp`](./lang.c-cpp.driver.mysql-connector-cpp.md) | C/C++ | driver | 3 full, 3 missing, 5 n/a |
| [`lang.csharp.driver.mysql`](./lang.csharp.driver.mysql.md) | C# | driver | 4 missing, 7 n/a |
| [`lang.elixir.driver.myxql`](./lang.elixir.driver.myxql.md) | elixir | driver | 4 missing, 7 n/a |
| [`lang.go.driver.mysql`](./lang.go.driver.mysql.md) | go | driver | 1 full, 1 partial, 3 missing, 6 n/a |
| [`lang.jsts.driver.mysql`](./lang.jsts.driver.mysql.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.mysql`](./lang.php.driver.mysql.md) | php | driver | 1 partial, 4 missing, 6 n/a |
| [`lang.python.driver.mysql`](./lang.python.driver.mysql.md) | python | driver | 1 full, 1 partial, 3 missing, 6 n/a |
| [`lang.ruby.driver.mysql`](./lang.ruby.driver.mysql.md) | ruby | driver | 4 missing, 7 n/a |
| [`lang.rust.driver.mysql`](./lang.rust.driver.mysql.md) | rust | driver | 4 missing, 7 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.mysql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
