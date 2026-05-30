<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.elastic4s` — Elastic4s

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/elastic4s.yaml` | — |
| Schema extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | elastic4sIndexRe captures createIndex/indexInto index defs; elastic4sHitReaderRe captures HitReader[T]/HitWriter[T] document type mappings; elastic4sCaseClassRe captures document case classes |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Elasticsearch is a distributed document search engine; relational associations are not_applicable — documents are denormalized, parent-child relationships via join fields only |
| Foreign key extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Elasticsearch has no foreign key concept; elastic4s provides no FK declarations |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | elastic4s uses explicit Future/IO-based query execution; no transparent lazy-loading mechanism |
| Relationship extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Elasticsearch NoSQL — no relational relationship declarations; document relationships via nested objects or parent-child join fields |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/elastic4s.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.elastic4s ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
