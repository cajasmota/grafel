<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.slick` — Slick

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/slick.yaml` | — |
| Schema extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | slickTableClassRe extracts Table[T] class defs; slickColumnRe extracts column[T] defs; slickTableQueryRe extracts TableQuery[T] |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | Slick foreignKey() captured as relationship entity with local_column+target_table; no high-level hasMany/belongsTo DSL exists in Slick, so association resolution stays partial |
| Foreign key extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | slickForeignKeyRe captures foreignKey(name, col, targetTable)(_.col) declarations → SCOPE.Schema entities with pattern_type=foreign_key |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Slick uses explicit db.run() with explicit DBIOAction composition; no transparent lazy-loading proxy mechanism exists |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | FK declarations + TableQuery join sites extracted with local/target columns; resolving the joined Table[T] across files (cross-file association graph) is out of scope — honest partial |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/slick.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | slickMigrationRe captures schema.create / schema.createIfNotExists / DBIO.seq DDL patterns; full SQL migration file parsing not applicable (use Flyway) |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.slick ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
