<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.orm.dapper` — Dapper

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | POCO classes with [Table] attribute detected via regex; heuristic |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | [Column] attribute annotation on POCO properties detected via regex; heuristic |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Dapper is a micro-ORM; relationships must be manually queried via SQL — no built-in association mechanism |
| Foreign key extraction | — `not_applicable` | — | — | — | Dapper is a micro-ORM; no FK attribute support — FK columns are raw SQL strings |
| Lazy loading recognition | — `not_applicable` | — | — | — | Dapper does not support lazy loading — all queries are explicit SQL |
| Relationship extraction | — `not_applicable` | — | — | — | Dapper does not model relationships; multi-table patterns are raw SQL joins |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | Dapper Query<T>/Execute/ExecuteScalar calls detected via regex; heuristic |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | micro-ORM/query-lib — no built-in migration system |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.orm.dapper ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
