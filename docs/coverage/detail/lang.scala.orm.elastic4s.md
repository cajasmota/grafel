<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.elastic4s` — Elastic4s

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-06-03` | 3625 | `internal/custom/scala/orm_extractors.go`<br>`internal/custom/scala/orm_extractors_test.go` | Cite corrected (#3637): custom_scala_elastic4s (elastic4sExtractor) emits SCOPE.Schema document models from case class defs, HitReader[T]/HitWriter[T] mappings, and createIndex/indexInto index defs (value-asserting TestElastic4sHitReader / TestElastic4sIndexValues). Partial: field-level document mapping not parsed. |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
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
| Query attribution | 🟢 `partial` | `2026-06-03` | 3625 | `internal/custom/scala/orm_extractors.go`<br>`internal/custom/scala/orm_extractors_test.go` | Cite corrected (#3637): elastic4sSearchRe captures search("index") calls and emits SCOPE.Operation/query carrying the target index_name (value-asserting TestElastic4sSearch / TestElastic4sSearchAndHitValues). Partial: index-level attribution only — query-body / DSL clause structure not parsed. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Related extraction records

This record provides code-level coverage for the
[`db.elasticsearch`](./db.elasticsearch.md) hub record (Elasticsearch (indices)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.elastic4s ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
