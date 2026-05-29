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
| Schema extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/extractors/python/django_relational.go` | field_type and all keyword arguments (max_length, null, blank, on_delete, related_name, etc.) are stamped on each SCOPE.Schema/field entity by stampDjangoFieldProperties(); structured JSON Schema or OpenAPI emission not yet implemented |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/django_relational.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/django_relational.go` | — |
| Lazy loading recognition | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/orm_queries_python.go` | select_related() and prefetch_related() detected as is_join=true by pythonIsJoinDjango(); full recognition of all lazy strategies not yet implemented |
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
