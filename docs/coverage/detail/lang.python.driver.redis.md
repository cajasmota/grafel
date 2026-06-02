<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.driver.redis` — redis-py

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
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
| Query attribution | ✅ `full` | `2026-06-02` | 3643 | `internal/custom/python/redis.go` | Key/channel/stream topology extracted: concrete keys, prefix globs (user:*) from concat and f-strings; READS_FROM/WRITES_TO/PUBLISHES_TO/SUBSCRIBES_TO edges to SCOPE.Datastore keyspace targets. Matches JS scanJSRedis shape (#3643). |

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
[`db.redis`](./db.redis.md) infra record (Redis (keys)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.driver.redis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
