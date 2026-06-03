<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.scanamo` — Scanamo (DynamoDB)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-06-03` | 3625 | `internal/custom/scala/orm_extractors.go`<br>`internal/custom/scala/orm_extractors_test.go` | Cite corrected (#3637): custom_scala_scanamo (scanamoExtractor) emits SCOPE.Schema for Table[T]("name") DynamoDB table defs, item case classes, and DynamoFormat[T] derivations (value-asserting TestScanamoTableDef / TestScanamoTableValues). Partial: field-level item schema not parsed. |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | ✅ `full` | — | — | `internal/custom/scala/orm_extractors.go` | scanamoTableRe captures Table[T]("name") DynamoDB table defs; scanamoDynamoFormatRe captures DynamoFormat[T] implicit derivations; scanamoCaseClassRe captures item case classes |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | DynamoDB is a NoSQL key-value store; relational associations/joins are not_applicable — item relationships modeled via single-table design or denormalization |
| Foreign key extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | DynamoDB has no foreign key concept; Scanamo provides no FK declarations |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | Scanamo uses explicit IO-based queries (cats-effect / ZIO); no ORM-style lazy-loading proxies |
| Relationship extraction | — `not_applicable` | — | — | `internal/custom/scala/orm_extractors.go` | DynamoDB NoSQL — no relational relationship declarations in Scanamo; item access patterns are modeled via GSI/LSI index design, not FK relationships |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | Overstated cite corrected (#3637): scanamo.yaml is detection-only (empty source_patterns/relationship_rules; dead custom_extractors Python block) and custom_scala_scanamo emits only table-def / item-model SCOPE.Schema entities — NO query/scan call-site SCOPE.Operation entity and no QUERIES edge. No native query-topology extractor exists; honest status is missing. |

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
[`db.dynamodb`](./db.dynamodb.md) hub record (AWS DynamoDB),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.scanamo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
