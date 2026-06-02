<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.driver.elastic` — @elastic/elasticsearch

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | — `not_applicable` | — | 3069 | — | Raw driver binding executes SQL/query strings directly; no ORM model layer, no schema declarations, no associations, no FK definitions, no lazy-loading. N/A per issue #3069. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | 3069 | — | Raw driver binding executes SQL/query strings directly; no ORM model layer, no schema declarations, no associations, no FK definitions, no lazy-loading. N/A per issue #3069. |
| Foreign key extraction | — `not_applicable` | — | 3069 | — | Raw driver binding executes SQL/query strings directly; no ORM model layer, no schema declarations, no associations, no FK definitions, no lazy-loading. N/A per issue #3069. |
| Lazy loading recognition | — `not_applicable` | — | 3069 | — | Raw driver binding executes SQL/query strings directly; no ORM model layer, no schema declarations, no associations, no FK definitions, no lazy-loading. N/A per issue #3069. |
| Relationship extraction | — `not_applicable` | — | 3069 | — | Raw driver binding executes SQL/query strings directly; no ORM model layer, no schema declarations, no associations, no FK definitions, no lazy-loading. N/A per issue #3069. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts_drivers.go`<br>`internal/engine/orm_queries_jsts_drivers_test.go` | — |

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
(or use `go run ./tools/coverage update lang.jsts.driver.elastic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
