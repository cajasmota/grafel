<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.driver.redis` — redis-rb

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | — `not_applicable` | — | — | — | raw client driver; no ORM model/schema in user code |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw client driver; no association DSL |
| Foreign key extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |
| Relationship extraction | — `not_applicable` | — | — | — | raw client driver; no relationship DSL |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | 3643 | `internal/custom/ruby/redis.go` | Key/channel/stream topology extracted from redis-rb: concrete keys, prefix globs (user:*) from plus-concat and double-quoted interpolation heads (user:#{id}); single-quoted literals are not interpolated (treated as concrete keys). READS_FROM/WRITES_TO/PUBLISHES_TO/SUBSCRIBES_TO edges to SCOPE.Datastore keyspace targets. Mirrors the Python template (#3668). Value-asserting tests in internal/custom/ruby/redis_test.go (literal key, concat/interpolated prefix glob, single-quote no-interp, publish/subscribe channel, stream add, dynamic-key negative). |

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
(or use `go run ./tools/coverage update lang.ruby.driver.redis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
