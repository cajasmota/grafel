<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.driver.sqlite` — sqlite3 (stdlib)

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
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3189 | `internal/custom/python/driver_schema.go` | Heuristic: parses CREATE TABLE DDL embedded in raw-driver cursor.execute(...) string literals into SCOPE.Schema table + column entities. Single-literal SQL only; executescript multi-statement DDL supported. |

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
| Query attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/rules/python/orms/sqlite_py.yaml`<br>`internal/extractors/cross/dbmap/orms.go`<br>`internal/extractors/python/raw_sql_db_calls.go` | Raw cursor.execute("…") SQL now resolves table topology: dbmap.detectPyDBAPI (import-gated on sqlite3/aiosqlite) parses FROM/INTO/UPDATE/JOIN and emits SCOPE.DataAccess + ACCESSES_TABLE edges with read/write verb. Value-asserting tests TestPyDBAPISqlite3SelectReadsUsers + negative TestPyDBAPIDynamicSQLNoFabricatedTable (#3644). |

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
[`db.sqlite`](./db.sqlite.md) infra record (SQLite (schema)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.driver.sqlite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
