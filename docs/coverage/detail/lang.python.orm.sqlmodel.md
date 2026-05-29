<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.sqlmodel` — SQLModel

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/orms/sqlmodel.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/sqlalchemy.go` | SQLModel table=True class detection added to python_sqlalchemy extractor (issue #2990). Only classes with both SQLModel base and table=True kwarg are emitted; schema-only classes are excluded. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | 3056 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/sqlalchemy.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-29` | 3056 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/sqlalchemy.go` | — |
| Lazy loading recognition | ✅ `full` | `2026-05-29` | 3056 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/sqlalchemy.go` | SQLModel delegates to SQLAlchemy relationship() for lazy loading, but the sqlalchemy extractor's lazy_strategy detection (issue #2986) applies only when the SQLAlchemy extractor fires on a SQLModel file. SQLModel-specific lazy loading is not yet explicitly tracked. |
| Relationship extraction | ✅ `full` | `2026-05-29` | 3056 | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/sqlalchemy.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_python.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-29` | 3056 | `internal/engine/rules/python/orms/alembic.yaml` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.sqlmodel ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
