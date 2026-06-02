<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.ktorm` — Ktorm

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/ktorm.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | Ktorm supports lazy sequence loading; regex detects .references() FK bindings which imply eager joins |
| Relationship extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_query.go`<br>`internal/custom/kotlin/orm_query_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | Ktorm has no built-in migration layer; migrations are handled externally (Flyway, Liquibase, etc.) |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.orm.ktorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
