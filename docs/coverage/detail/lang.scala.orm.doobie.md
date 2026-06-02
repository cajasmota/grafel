<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.doobie` — Doobie

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/doobie.yaml` | — |
| Schema extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | doobieQueryRe captures .query[T] row type mappings; doobieCaseClassRe captures case class row models; doobieSQLRe captures sql"..." fragments |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Doobie is a functional JDBC wrapper, not an ORM; associations are expressed via raw SQL JOINs with no declarative DSL to extract |
| Foreign key extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Doobie does not declare foreign keys; FK constraints live in the database schema managed externally (Flyway/Liquibase) |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Doobie has no lazy-loading; all queries are explicit ConnectionIO values composed via for-comprehensions |
| Relationship extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | No relationship declarations in doobie; relationships expressed via raw SQL with no extractable DSL |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/doobie.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Doobie does not manage migrations; use Flyway or Liquibase alongside doobie |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.doobie ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
