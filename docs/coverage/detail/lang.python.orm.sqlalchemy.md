<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.sqlalchemy` — SQLAlchemy

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_field_edges.go`<br>`internal/engine/rules/python/orms/sqlalchemy.yaml` | — |
| Schema extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/sqlalchemy.go`<br>`internal/engine/orm_field_edges.go` | __tablename__, Mapped[] columns, relationship attributes, and ForeignKey targets are extracted as SCOPE.Schema entities; structured JSON Schema or OpenAPI emission not yet implemented |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/sqlalchemy.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/sqlalchemy.go` | — |
| Lazy loading recognition | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/sqlalchemy.go` | lazy= kwarg in relationship() calls is detected and recorded as lazy_strategy on the SCOPE.Schema entity; lazy_select_in, write_only, and dynamic_write_only strategies not yet distinguished |
| Relationship extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/sqlalchemy.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_python.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/alembic.yaml` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.sqlalchemy ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
