<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.orm.sqlitecpp` — SQLiteCpp

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🔴 `missing` | — | 4978 | — | Detection-only today; no Go extractor emits this for the SQL/ODBC wrapper. Follow-up #4978. |
| Model lifecycle extraction | 🔴 `missing` | — | 4978 | — | Detection-only today; no Go extractor emits this for the SQL/ODBC wrapper. Follow-up #4978. |
| Schema extraction | 🔴 `missing` | — | 4978 | — | Detection-only today; no Go extractor emits this for the SQL/ODBC wrapper. Follow-up #4978. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Thin SQL/ODBC wrapper, not an ORM/data-mapper — no association/relationship/FK/lazy-loading/migration layer. |
| Foreign key extraction | — `not_applicable` | — | — | — | Thin SQL/ODBC wrapper, not an ORM/data-mapper — no association/relationship/FK/lazy-loading/migration layer. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Thin SQL/ODBC wrapper, not an ORM/data-mapper — no association/relationship/FK/lazy-loading/migration layer. |
| Relationship extraction | — `not_applicable` | — | — | — | Thin SQL/ODBC wrapper, not an ORM/data-mapper — no association/relationship/FK/lazy-loading/migration layer. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | — | 4978 | `internal/engine/rules/cpp/orms/sqlitecpp.yaml` | SQLiteCpp DETECTED via internal/engine/rules/cpp/orms/sqlitecpp.yaml (find_package(SQLiteCpp), SQLite::Database/Statement/Transaction, db.exec markers). No Go extractor emits query_attribution / model / schema entities yet — detection-only. Follow-up #4978. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | Thin SQL/ODBC wrapper, not an ORM/data-mapper — no association/relationship/FK/lazy-loading/migration layer. |
| Migration schema ops | 🔴 `missing` | — | 4978 | — | Detection-only today; no Go extractor emits this for the SQL/ODBC wrapper. Follow-up #4978. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 4978 | — | Detection-only today; no Go extractor emits this for the SQL/ODBC wrapper. Follow-up #4978. |

## Related extraction records

This record provides code-level coverage for the
[`db.sqlite`](./db.sqlite.md) hub record (SQLite (schema)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.orm.sqlitecpp ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
