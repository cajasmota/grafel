<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.alembic` — Alembic (migration tool)

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
| Query attribution | — `not_applicable` | — | — | — | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/alembic.yaml` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.alembic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
