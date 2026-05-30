<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.orm.eloquent` — Eloquent (Laravel)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/php/orms/eloquent_laravel_orm.yaml` | — |
| Schema extraction | ✅ `full` | — | — | `internal/custom/php/orm_data.go` | Eloquent $fillable/$casts/Schema::create/Blueprint columns; belongsTo/belongsToMany FK relations; $with eager hints; migration DDL. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | — | — | `internal/custom/php/orm_data.go` | Eloquent $fillable/$casts/Schema::create/Blueprint columns; belongsTo/belongsToMany FK relations; $with eager hints; migration DDL. |
| Foreign key extraction | ✅ `full` | — | — | `internal/custom/php/orm_data.go` | Eloquent $fillable/$casts/Schema::create/Blueprint columns; belongsTo/belongsToMany FK relations; $with eager hints; migration DDL. |
| Lazy loading recognition | ✅ `full` | — | — | `internal/custom/php/orm_data.go` | Eloquent $fillable/$casts/Schema::create/Blueprint columns; belongsTo/belongsToMany FK relations; $with eager hints; migration DDL. |
| Relationship extraction | ✅ `full` | — | — | `internal/custom/php/orm_data.go` | Eloquent $fillable/$casts/Schema::create/Blueprint columns; belongsTo/belongsToMany FK relations; $with eager hints; migration DDL. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_other.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | — | — | `internal/custom/php/orm_data.go` | Eloquent $fillable/$casts/Schema::create/Blueprint columns; belongsTo/belongsToMany FK relations; $with eager hints; migration DDL. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.orm.eloquent ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
