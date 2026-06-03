<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.postgres` — PostgreSQL (schema)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/orm_queries.go` | — |
| Resource extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/database_index/language.yaml`<br>`internal/extractors/sql` | — |

## Related extraction records

This hub record tracks the technology at a high level. The deep, code-level
coverage for this technology lives in the per-language records below — each
one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.c-cpp.driver.libpqxx`](./lang.c-cpp.driver.libpqxx.md) | C/C++ | driver | 3 full, 3 missing, 5 n/a |
| [`lang.csharp.driver.npgsql`](./lang.csharp.driver.npgsql.md) | C# | driver | 4 missing, 7 n/a |
| [`lang.elixir.driver.postgrex`](./lang.elixir.driver.postgrex.md) | elixir | driver | 4 missing, 7 n/a |
| [`lang.go.orm.pgx`](./lang.go.orm.pgx.md) | go | orm | 1 full, 4 partial, 2 missing, 4 n/a |
| [`lang.jsts.driver.postgres`](./lang.jsts.driver.postgres.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.postgres`](./lang.php.driver.postgres.md) | php | driver | 1 partial, 4 missing, 6 n/a |
| [`lang.python.driver.postgres`](./lang.python.driver.postgres.md) | python | driver | 1 full, 1 partial, 3 missing, 6 n/a |
| [`lang.ruby.driver.postgres`](./lang.ruby.driver.postgres.md) | ruby | driver | 1 full, 3 missing, 7 n/a |
| [`lang.rust.driver.postgres`](./lang.rust.driver.postgres.md) | rust | driver | 4 missing, 7 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.postgres ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
