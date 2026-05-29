<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.jooq` — jOOQ

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/java/orms/jooq.yaml` | — |
| Schema extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/jooq.go` | jOOQ schema is expressed via generated Table/Record classes from DDL, not annotations. Cannot be extracted via annotation scanning. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/jooq.go` | jOOQ is code-generation first; relationships are expressed via generated FKs in schema classes, not annotations. Static type-safe DSL extraction requires a different paradigm; tracked in issue #3001. |
| Foreign key extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/jooq.go` | jOOQ FK extraction requires parsing generated schema classes or DDL, not annotation scanning. Not currently implemented; tracked in issue #3001. |
| Lazy loading recognition | — `not_applicable` | — | 3098 | `internal/custom/java/jooq.go` | jOOQ is a query DSL with no lazy-loading concept; lazy_loading_recognition is not applicable |
| Relationship extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/jooq.go` | jOOQ relationships are in generated code; no extractor for generated jOOQ schema classes. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/java/orms/jooq.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | 3098 | `internal/custom/java/jooq.go` | jOOQ is a query DSL, not schema-migration tooling; migration_parsing is not applicable |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.jooq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
