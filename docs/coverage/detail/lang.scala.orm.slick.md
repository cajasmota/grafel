<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.slick` — Slick

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/slick.yaml` | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | slickTableClassRe extracts Table[T] class defs; slickColumnRe extracts column[T] defs; slickTableQueryRe extracts TableQuery[T] |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | Slick foreignKey() declarations extracted as relationship entities; no high-level ORM association DSL but FK constraints captured |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | slickForeignKeyRe captures foreignKey(name, col, targetTable)(_.col) declarations → SCOPE.Schema entities with pattern_type=foreign_key |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Slick uses explicit db.run() with explicit DBIOAction composition; no transparent lazy-loading proxy mechanism exists |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | FK declarations and TableQuery join patterns extracted; no higher-level hasMany/belongsTo DSL in Slick |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/slick.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/scala/orm_extractors.go` | slickMigrationRe captures schema.create / schema.createIfNotExists / DBIO.seq DDL patterns; full SQL migration file parsing not applicable (use Flyway) |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.slick ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
