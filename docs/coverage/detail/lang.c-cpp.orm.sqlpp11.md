<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.orm.sqlpp11` — sqlpp11

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: struct T : sqlpp::table<T,...> → model with class_name |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: column structs (trailing _) inside table body → Table.col with parent_table + col_struct; SQLPP_ALIAS_PROVIDER → alias |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | `2026-05-30` | — | — | sqlpp11 is a type-safe SQL DSL; no ORM-level association layer |
| Foreign key extraction | — `not_applicable` | `2026-05-30` | — | — | sqlpp11 has no ORM FK layer; FK constraints are in DB schema |
| Lazy loading recognition | — `not_applicable` | `2026-05-30` | — | — | sqlpp11 is a type-safe SQL DSL; no lazy-loading concept |
| Relationship extraction | — `not_applicable` | `2026-05-30` | — | — | sqlpp11 has no ORM relationship layer |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/orm.go` | Regex: db(select/insert_into/update/remove_from) → query with classified sql_verb |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | `2026-05-30` | — | — | sqlpp11 has no built-in migration system |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.orm.sqlpp11 ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
