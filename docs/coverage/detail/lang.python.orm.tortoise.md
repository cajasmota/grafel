<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.tortoise` — Tortoise ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/orms/tortoise_orm.yaml` | — |
| Schema extraction | ❌ `missing` | `2026-05-29` | backfill:dictionary-completeness | — | Tortoise ORM field definitions (fields.CharField etc.) are not parsed by any Go extractor; only model class detection (model_extraction) is handled via YAML rules. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Foreign key extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | ❌ `missing` | `2026-05-29` | backfill:dictionary-completeness | — | Tortoise ORM does not have a lazy-loading concept comparable to SQLAlchemy; prefetch_related is async-explicit and tracked under query_attribution only. |
| Relationship extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/orm_queries_python.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.tortoise ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
