<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.jooq` — jOOQ

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

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
| Query attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/rules/java/orms/jooq.yaml`<br>`internal/extractors/cross/dbmap/orms.go`<br>`internal/extractors/cross/dbmap/query_builders.go`<br>`internal/extractors/cross/dbmap/query_builders_test.go` | #3628 area #3: jOOQ builder calls dsl.selectFrom(USERS)/insertInto/update/deleteFrom resolve the generated table constant (lower-cased) into SCOPE.DataAccess + ACCESSES_TABLE edges with the right op kind. Proven by TestJOOQSelectFrom/InsertInto/DeleteFrom. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | 3098 | `internal/custom/java/jooq.go` | jOOQ is a query DSL, not schema-migration tooling; migration_parsing is not applicable |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.jooq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
