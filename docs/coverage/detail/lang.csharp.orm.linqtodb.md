<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.orm.linqtodb` — LinqToDB

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | [Table]/DataContext/DataConnection class patterns detected via regex; heuristic |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | [Column] attribute and Table<T> property patterns detected via regex; heuristic |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | [Association(ThisKey,OtherKey)] attribute detected via reAssocAttr; emits association_extraction entity |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | ThisKey/OtherKey extracted from [Association] + explicit [ForeignKey] attr via reForeignKeyAttr |
| Lazy loading recognition | — `not_applicable` | — | — | — | LinqToDB does not support lazy loading — all queries are explicit LINQ expressions |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | [Association] attribute detected via regex; heuristic |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | — | — | `internal/custom/csharp/dapper_models.go` | DataConnection subclass + LINQ query patterns detected; issue #3263 |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | micro-ORM/query-lib — no built-in migration system |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.orm.linqtodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
