<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.driver.dynamodb` — AWS SDK DynamoDB (Go)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/dynamodb.go`<br>`internal/custom/golang/nosql_drivers_test.go`<br>`internal/engine/rules/go/orms/dynamodb_go.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/dynamodb.go`<br>`internal/custom/golang/nosql_drivers_test.go`<br>`internal/engine/rules/go/orms/dynamodb_go.yaml` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | NoSQL/graph driver: no ORM association metadata. |
| Foreign key extraction | — `not_applicable` | — | — | — | No foreign-key concept in this driver. |
| Lazy loading recognition | — `not_applicable` | — | — | — | No lazy/eager loading; queries are explicit. |
| Relationship extraction | — `not_applicable` | — | — | — | DynamoDB has no foreign keys or joins; GSIs/LSIs are access-path indexes. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/dynamodb.go`<br>`internal/custom/golang/nosql_drivers_test.go`<br>`internal/engine/rules/go/orms/dynamodb_go.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.driver.dynamodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
