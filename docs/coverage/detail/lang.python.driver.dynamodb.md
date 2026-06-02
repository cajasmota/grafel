<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.driver.dynamodb` — boto3 DynamoDB

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | — `not_applicable` | — | — | — | NoSQL driver — no relational schema model |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship model |
| Foreign key extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship model |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw driver — no ORM relationship model |
| Relationship extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship model |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_drivers_other.go`<br>`internal/engine/orm_queries_drivers_other_test.go` | Driver topology: dynamodb.Table('X') / TableName='X' captured via scanPythonDrivers/emitDynamoTargets; QUERIES edge to Class:<Table>; dynamic names honest-skipped. |

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
(or use `go run ./tools/coverage update lang.python.driver.dynamodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
