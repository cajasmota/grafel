<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.driver.mysql` — PDO MySQL / mysqli

Auto-generated. Back to [summary](../summary.md).

- **Language:** [PHP](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/driver_sql.go` | CREATE TABLE statements in PDO/mysqli execute calls scanned for table + column names (heuristic). |

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
| Query attribution | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/grafel/issues/3644) | — | Driver topology: PDO MySQL ($pdo->query/prepare/exec) and mysqli (OO $mysqli->query + procedural mysqli_query($conn,"...")) raw-SQL literals are table-parsed via scanPHPDrivers/emitSQLDatastoreTargets (phpSqlRe + mysql:/mysqli backend gate) into QUERIES->Class:<table> with orm=mysql. Interpolated/concatenated SQL is honest-skipped (no static literal table). #4271 |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Related extraction records

This record provides code-level coverage for the
[`db.mysql`](./db.mysql.md) hub record (MySQL / MariaDB (schema)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.driver.mysql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
