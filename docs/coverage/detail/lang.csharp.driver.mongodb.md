<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.driver.mongodb` — MongoDB.Driver (C#)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/driver_schema.go` | MongoDB.Bson [BsonCollection]/[BsonElement]/[BsonIgnore] attrs detected via reBsonCollection/reBsonElement/reBsonIgnore |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |
| Foreign key extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |
| Relationship extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

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
[`db.mongodb`](./db.mongodb.md) infra record (MongoDB (collections)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.driver.mongodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
