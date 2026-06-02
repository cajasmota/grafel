<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.driver.redis` — redis-rs

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | — `not_applicable` | — | — | — | raw client driver; no schema/model definitions in user code |

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
| Query attribution | ✅ `full` | `2026-06-02` | 3643 | `internal/custom/rust/redis.go` | Key/channel/stream topology extracted from redis-rs (typed Commands trait con.get/set/publish/xadd + low-level cmd("VERB").arg("key") builder): concrete literal keys/channels/streams; READS_FROM/WRITES_TO/PUBLISHES_TO/SUBSCRIBES_TO edges to SCOPE.Datastore keyspace targets. Rust has no in-literal interpolation, so format!()/bare-var keys stay dynamic (honest-partial, no fabricated key). Mirrors the Python template (#3668). Value-asserting tests in internal/custom/rust/redis_test.go (literal key, turbofish, publish channel, stream add, cmd-builder GET/SET/PUBLISH, dynamic-key negative). |

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
(or use `go run ./tools/coverage update lang.rust.driver.redis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
