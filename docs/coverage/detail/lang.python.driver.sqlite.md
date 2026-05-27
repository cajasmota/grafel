<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.driver.sqlite` — sqlite3 (stdlib)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `migration_parsing` | — `not_applicable` | — | — | — | — |
| `model_extraction` | — `not_applicable` | — | — | — | — |
| `query_attribution` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/python/orms/sqlite_py.yaml`<br>`internal/extractors/python/raw_sql_db_calls.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.driver.sqlite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
