<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.driver.cassandra` — gocql (Cassandra)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/gocql.go`<br>`internal/custom/golang/nosql_drivers_test.go`<br>`internal/engine/rules/go/orms/cassandra_go.yaml` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | NoSQL/graph driver: no ORM association metadata. |
| Foreign key extraction | — `not_applicable` | — | — | — | No foreign-key concept in this driver. |
| Lazy loading recognition | — `not_applicable` | — | — | — | No lazy/eager loading; queries are explicit. |
| Relationship extraction | — `not_applicable` | — | — | — | Cassandra is a wide-column store with no foreign keys or joins. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/gocql.go`<br>`internal/custom/golang/nosql_drivers_test.go`<br>`internal/engine/rules/go/orms/cassandra_go.yaml` | — |

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
(or use `go run ./tools/coverage update lang.go.driver.cassandra ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
