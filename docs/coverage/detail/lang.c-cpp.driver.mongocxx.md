<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.driver.mongocxx` — mongocxx

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/driver_schema.go` | Regex: db["coll"]/db.collection("coll") → SCOPE.Schema with collection_name |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/driver_schema.go` | Collection access recorded as schema proxy; document store has no static field-level schema to extract |

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
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/driver_schema.go` | Regex: collection CRUD/aggregate ops (insert_one/find/update_one/delete_many/aggregate/...) → query with mongo_verb |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | raw driver — no built-in migration system |
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
(or use `go run ./tools/coverage update lang.c-cpp.driver.mongocxx ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
