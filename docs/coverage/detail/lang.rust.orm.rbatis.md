<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.orm.rbatis` — Rbatis

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/sqlx_rbatis.go`<br>`internal/custom/rust/sqlx_rbatis_test.go`<br>`internal/custom/rust/testdata/rbatis_models.rs` | Detects #[crud_table(table_name=...)] struct declarations as ORM models |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/sqlx_rbatis.go`<br>`internal/custom/rust/sqlx_rbatis_test.go` | Extracts table_name from #[crud_table(table_name=...)] attribute as schema table mapping |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | rbatis is a SQL/XML mapper; no relationship/association DSL |
| Foreign key extraction | — `not_applicable` | — | — | — | rbatis is a SQL/XML mapper; no relationship/association DSL |
| Lazy loading recognition | — `not_applicable` | — | — | — | rbatis is a SQL/XML mapper; no relationship/association DSL |
| Relationship extraction | — `not_applicable` | — | — | — | rbatis is a SQL/XML mapper; no relationship/association DSL |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/orm_props_test.go`<br>`internal/custom/rust/sqlx_rbatis.go` | Detects #[py_sql(...)], #[sql(...)], #[html_sql] macro annotations on async functions |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/sqlx_rbatis.go`<br>`internal/custom/rust/sqlx_rbatis_test.go` | Detects table_meta! and rbatis::table_sync! migration macros |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.orm.rbatis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
