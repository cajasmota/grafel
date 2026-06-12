<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.orm.sqlx` — sqlx (Rust)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/rust/orms/sqlx.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/sqlx_rbatis.go`<br>`internal/custom/rust/sqlx_rbatis_test.go`<br>`internal/custom/rust/testdata/sqlx_models.rs` | Detects #[derive(FromRow)] structs and Pool::connect() as schema-mapped models |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | sqlx is a compile-checked query layer; no relationship/association DSL |
| Foreign key extraction | — `not_applicable` | — | — | — | sqlx is a compile-checked query layer; no relationship/association DSL |
| Lazy loading recognition | — `not_applicable` | — | — | — | sqlx is a compile-checked query layer; no relationship/association DSL |
| Relationship extraction | — `not_applicable` | — | — | — | sqlx is a compile-checked query layer; no relationship/association DSL |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/orm_props_test.go`<br>`internal/custom/rust/sqlx_rbatis.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/sqlx_rbatis.go`<br>`internal/custom/rust/sqlx_rbatis_test.go` | sqlx::migrate! path + migration file refs detected; parsing the referenced migrations/*.sql DDL is out of scope (sqlx resolves them at compile time, not in the .rs source). |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-12` | 5021 | `internal/custom/rust/transactions.go`<br>`internal/custom/rust/transactions_test.go` | #5021: sqlx `pool.begin()` / `conn.begin()` plus `tx.commit()` / `tx.rollback()` emit one SCOPE.Pattern/transaction_boundary stamping transactional=true, framework=sqlx, transaction_api=sqlx_begin|sqlx_commit, the db_handle receiver, and the enclosing fn via `function`. No transitive propagation (a fn receiving a Transaction handle is not stamped). Proven by TestRustTx_SqlxBegin. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.orm.sqlx ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
