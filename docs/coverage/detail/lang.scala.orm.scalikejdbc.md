<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.scalikejdbc` — ScalikeJDBC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/scalikejdbc.yaml` | — |
| Schema extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | scalikejdbcSyntaxSupportRe captures SQLSyntaxSupport[T] companion objects; scalikejdbcCaseClassRe captures row case classes |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | scalikejdbcHasManyRe captures hasMany/hasManyThrough/hasOne/belongsTo relationship declarations → SCOPE.Schema entities |
| Foreign key extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | ScalikeJDBC has no FK declaration DSL; FKs live in the DB schema and are not declared in ScalikeJDBC model code |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | ScalikeJDBC uses explicit DB session blocks; no transparent lazy-loading proxies — queries are always explicit |
| Relationship extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | hasMany/hasManyThrough/hasOne/belongsTo DSL extracted; DB session block patterns captured for join-query sites |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/scalikejdbc.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | scalikejdbcDBMigrationRe captures DB autoCommit/localTx blocks which can contain DDL; scalikejdbc-play-support includes DB evolution integration |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.scalikejdbc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
