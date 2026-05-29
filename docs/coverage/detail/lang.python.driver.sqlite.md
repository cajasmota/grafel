<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.driver.sqlite` — sqlite3 (stdlib)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Foreign key extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/sqlite_py.yaml`<br>`internal/extractors/python/raw_sql_db_calls.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.driver.sqlite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
