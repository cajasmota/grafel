<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.quill` — Quill

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/quill.yaml` | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | quillQuerySchemaRe captures querySchema[T]("table") mappings; quillCaseClassRe captures entity case classes; compile-time macro expansion limits runtime introspection |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | quillQuoteQueryRe captures quote {} blocks containing query[T] references; JOIN associations expressed in quoted DSL blocks |
| Foreign key extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Quill has no FK declaration DSL; FK constraints live in the DB schema, not in Quill entity definitions |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Quill generates compile-time queries; all queries are explicit and eager — no transparent lazy-loading mechanism |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/scala/orm_extractors.go` | quote {} blocks with join / nested query patterns captured; no declarative hasMany/belongsTo DSL |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/quill.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Quill does not manage schema migrations; use Flyway/Liquibase/scala-migrations alongside Quill |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.quill ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
