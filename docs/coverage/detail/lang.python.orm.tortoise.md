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
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3072 | `internal/custom/python/orm_schema.go` | Tortoise ORM field definitions (fields.CharField etc.) are not parsed by any Go extractor; only model class detection (model_extraction) is handled via YAML rules. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/tortoise_relationships.py` | — |
| Foreign key extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/tortoise_relationships.py` | — |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/tortoise_relationships.py` | Tortoise ORM does not have a lazy-loading concept comparable to SQLAlchemy; prefetch_related is async-explicit and tracked under query_attribution only. |
| Relationship extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/tortoise_relationships.py` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/orm_queries_python.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.tortoise ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
