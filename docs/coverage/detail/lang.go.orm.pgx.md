<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.pgx` — pgx (PostgreSQL driver)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/sql_drivers.go`<br>`internal/custom/golang/sql_drivers_test.go` | structs carrying db:"col" field tags are treated as schemas (heuristic — db: tag does not prove the struct is a DB table); fixture-tested (TestSqlxModelsAndQueries / TestPgxModelsAndQueries) |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/sql_drivers.go`<br>`internal/custom/golang/sql_drivers_test.go` | columns from db: struct tags + CREATE TABLE SQL literals in double/backquoted strings; schema inferred from strings only, not from runtime DDL or migrate files (separate migration_parsing cell) |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/sql_drivers.go`<br>`internal/custom/golang/sql_drivers_test.go` | FOREIGN KEY clauses parsed from CREATE TABLE SQL string literals; only catches inline DDL, not FK declarations in external migration files or ALTER TABLE statements |
| Lazy loading recognition | — `not_applicable` | — | — | — | — |
| Relationship extraction | — `not_applicable` | — | — | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | — | `internal/custom/golang/sql_drivers.go`<br>`internal/custom/golang/sql_drivers_test.go`<br>`internal/extractors/cross/dbmap/extractor_test.go`<br>`internal/extractors/cross/dbmap/orms.go` | Exec/Query/QueryRow/Get/Select/NamedExec call sites captured; raw SQL literal table topology now resolved by dbmap.detectGoSQLDriver (import-gated on github.com/jackc/pgx) which parses FROM/INTO/UPDATE/JOIN and emits SCOPE.DataAccess + ACCESSES_TABLE edges with read/write verb (#3644). |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🟢 `partial` | `2026-06-03` | 4063 | `internal/extractors/golang/extractor.go`<br>`internal/txscope/txscope.go`<br>`internal/txscope/txscope_test.go` | #4063: the generic txscope.DetectGo stamper fires on pgx's database/sql-compatible surface — pool.BeginTx(ctx, pgx.TxOptions{}) stamps transactional=true (tx_source=go_sql_begin_tx) on the enclosing fn. Partial, not full: the pgx-idiomatic conn.Begin(ctx) (non-empty parens) does NOT fire (goBeginRE requires Begin()); no transitive propagation. Tested by TestDetectGo_PgxBun. |

## Related extraction records

This record provides code-level coverage for the
[`db.postgres`](./db.postgres.md) hub record (PostgreSQL (schema)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.pgx ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
