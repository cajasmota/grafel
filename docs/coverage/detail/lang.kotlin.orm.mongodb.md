<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.mongodb` — MongoDB (Kotlin driver)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_query.go`<br>`internal/custom/kotlin/orm_query_test.go` | @Document collection models extracted with concrete collection name (mongodb:@Document:<collection>); value-asserted in TestOrmQuery_MongoDocumentAndRepo |
| Schema extraction | — `not_applicable` | — | — | — | MongoDB is a schemaless document database. There is no DDL schema to extract — documents have arbitrary fields and there is no enforced schema at the database level. Spring Data MongoDB @Document annotations define class-level hints, not a database schema. N/A is honest. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw/NoSQL driver — no ORM relationship/association/FK/lazy-load layer |
| Foreign key extraction | — `not_applicable` | — | — | — | raw/NoSQL driver — no ORM relationship/association/FK/lazy-load layer |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw/NoSQL driver — no ORM relationship/association/FK/lazy-load layer |
| Relationship extraction | — `not_applicable` | — | — | — | raw/NoSQL driver — no ORM relationship/association/FK/lazy-load layer |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_query.go`<br>`internal/custom/kotlin/orm_query_test.go` | — |

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
(or use `go run ./tools/coverage update lang.kotlin.orm.mongodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
