<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.orm.linq-to-sql` — LINQ to SQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | [Table]/DataContext/DataConnection class patterns detected via regex; heuristic |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | [Column] attribute and Table<T> property patterns detected via regex; heuristic |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | [Association(ThisKey,OtherKey)] attribute detected via reAssocAttr; emits association_extraction entity with cardinality info |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | ThisKey/OtherKey extracted from [Association] and explicit [ForeignKey] attribute detected via reForeignKeyAttr |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | DataLoadOptions.LoadWith<T>() deferred load pattern detected via reLoadWith |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | [Association] attribute detected via regex; heuristic |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/csharp/orms/linqpad_linq_to_sql.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | micro-ORM/query-lib — no built-in migration system |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.orm.linq-to-sql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
