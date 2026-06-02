<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.exposed` — Exposed (JetBrains)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/exposed.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |
| Lazy loading recognition | — `not_applicable` | — | — | — | Exposed is a query DSL without lazy-loading; all reads are explicit eager queries |
| Relationship extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/exposed.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.orm.exposed ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
