<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.orm.odb` — ODB

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: #pragma db object + table("…") → model with resolved table_name |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: #pragma db member → column with resolved column("…")/type("…")/id PK props |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: one_to_one/one_to_many/many_to_many in #pragma db member → relationship_kind + field |
| Foreign key extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: inverse(target) in #pragma db member → target_type on relationship |
| Lazy loading recognition | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: odb::lazy_ptr<T>/lazy_shared_ptr<T> → relationship with resolved target_type |
| Relationship extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: relationship kind + field from #pragma db member annotations |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: odb::query<T>/odb::result<T> → query with model_type |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | `2026-05-30` | — | — | ODB has no built-in migration system |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.orm.odb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
