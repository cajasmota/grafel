<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.driver.postgres` — psycopg / asyncpg (PostgreSQL drivers)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3189 | `internal/custom/python/driver_schema.go` | Heuristic: parses CREATE TABLE DDL embedded in raw-driver cursor.execute(...) string literals into SCOPE.Schema table + column entities. Single-literal SQL only; string-concatenated DDL not stitched. |

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
| Query attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/rules/python/orms/postgresql_py.yaml`<br>`internal/extractors/cross/dbmap/orms.go`<br>`internal/extractors/python/raw_sql_db_calls.go` | Raw cursor.execute("…") SQL resolves table topology: dbmap.detectPyDBAPI/detectPsycopg2 (asyncpg/psycopg2 import-gated) parses FROM/INTO/UPDATE/JOIN and emits SCOPE.DataAccess + ACCESSES_TABLE edges with read/write verb (#3644). |

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
(or use `go run ./tools/coverage update lang.python.driver.postgres ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
