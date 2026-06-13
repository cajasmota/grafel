<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.orm.nanodbc` — nanodbc (ODBC)

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
| Query attribution | 🟢 `partial` | — | 5026 | `internal/custom/cpp/orm_sql_wrappers.go`<br>`internal/engine/rules/cpp/orms/nanodbc.yaml` | Regex (custom_cpp_nanodbc): nanodbc::execute/prepare(conn, "SQL"), nanodbc::statement(conn, "SQL"), conn.execute("SQL") → query with classified sql_verb + sql_text + sql_table (first FROM/INTO/UPDATE/JOIN/TABLE) + sql_tables (ALL referenced tables for multi-table JOINs/CTEs/subqueries, #5026). #5026 also resolves intra-file variable-built SQL: std::string sql="..."; / auto/const char* decls, sql+=/<< concatenation, and the nanodbc prepared-then-bound two-step (statement stmt(conn); stmt.prepare(sql)) — bare identifiers are resolved against a file-local var→SQL map gated by a SQL-verb looksLikeSQL guard so non-SQL strings (log messages, paths) aren't attributed. partial: cross-FILE dataflow (SQL built in another translation unit) remains a gap. Detection still via nanodbc.yaml. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | Thin SQL/ODBC wrapper, not an ORM/data-mapper — no association/relationship/FK/lazy-loading/migration layer. |
| Migration schema ops | 🔴 `missing` | — | 4978 | — | Detection-only today; no Go extractor emits this for the SQL/ODBC wrapper. Follow-up #4978. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 4978 | — | Detection-only today; no Go extractor emits this for the SQL/ODBC wrapper. Follow-up #4978. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.orm.nanodbc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
