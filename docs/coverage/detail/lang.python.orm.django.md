<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.django` — Django ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/orms/django_orm.yaml`<br>`internal/extractors/python/django_relational.go` | — |
| Schema extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/django_relational.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/django_relational.go` | — |
| Lazy loading recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/django_relational.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_python.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/extractors/python/django_migration.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.django ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
