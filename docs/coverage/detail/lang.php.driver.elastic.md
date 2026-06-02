<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.driver.elastic` — elasticsearch-php

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | — `not_applicable` | — | — | — | Schema-less or server-side schema only; raw PHP driver has no PHP-level schema model to extract. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Foreign key extraction | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Relationship extraction | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_drivers_other.go`<br>`internal/engine/orm_queries_drivers_other_test.go` | Driver topology: ['index'=>'x'] literal captured via scanPHPDrivers/emitElasticTargets; QUERIES edge to Class:<Index>; dynamic names honest-skipped. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Datastore

This driver/ORM record provides code-level coverage for the
[`db.elasticsearch`](./db.elasticsearch.md) infra record (Elasticsearch (indices)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.driver.elastic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
