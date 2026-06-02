<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.sea-query` — SeaQuery

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-31` | 3558 | `internal/custom/rust/sea_query.go`<br>`internal/custom/rust/sea_query_test.go` | #[derive(Iden)] enum declarations emitted as SCOPE.Component(orm_model) recording the Iden enum as the logical table identity. Partial: the physical table name from an #[iden="..."] / impl Iden rename is not resolved (enum name used as table identity). |
| Schema extraction | 🟢 `partial` | `2026-05-31` | 3558 | `internal/custom/rust/sea_query.go`<br>`internal/custom/rust/sea_query_test.go` | .columns([Table::Col,...]) and .column(Table::Col) projections emitted as SCOPE.Component(schema_column) with the literal table+column Iden names. Partial: only columns referenced as Iden paths in the statement window are captured; dynamically-built column lists are not resolved. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Foreign key extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-31` | 3558 | `internal/custom/rust/sea_query.go`<br>`internal/custom/rust/sea_query_test.go` | Query::select()/insert()/update()/delete() statements emitted as SCOPE.Pattern(query) carrying statement_kind + the literal target table Iden (from(T)/into_table(T)/table(T)/from_table(T)). Partial: the captured table is the in-code Iden enum identifier, not the resolved physical SQL table string; statements whose table is built dynamically or in a helper are not attributed. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.sea-query ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
