<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.scanamo` — Scanamo (DynamoDB)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/scanamo.yaml` | — |
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
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/scala/orms/scanamo.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.scanamo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
