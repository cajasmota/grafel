<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.driver.mongodb` — mongo-go-driver

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/mongo_driver.go`<br>`internal/custom/golang/mongo_redis_test.go` | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/mongo_driver.go`<br>`internal/custom/golang/mongo_redis_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | — |
| Foreign key extraction | — `not_applicable` | — | — | — | — |
| Lazy loading recognition | — `not_applicable` | — | — | — | — |
| Relationship extraction | — `not_applicable` | — | — | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/mongo_driver.go`<br>`internal/custom/golang/mongo_redis_test.go`<br>`internal/engine/rules/go/orms/mongo_driver.yaml` | — |

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
(or use `go run ./tools/coverage update lang.go.driver.mongodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
