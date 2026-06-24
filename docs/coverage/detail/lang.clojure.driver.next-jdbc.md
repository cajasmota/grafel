<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.clojure.driver.next-jdbc` — next.jdbc

Auto-generated. Back to [summary](../summary.md).

- **Language:** [clojure](../by-language/clojure.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🔴 `missing` | — | 4910 | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 4910 | — | — |
| Schema extraction | 🔴 `missing` | — | 4910 | — | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🔴 `missing` | — | 4910 | — | — |
| Foreign key extraction | 🔴 `missing` | — | 4910 | — | — |
| Lazy loading recognition | 🔴 `missing` | — | 4910 | — | — |
| Relationship extraction | 🔴 `missing` | — | 4910 | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | — | 5362 | `internal/engine/orm_queries_clojure.go`<br>`internal/engine/orm_queries_clojure_test.go` | #5362: scanClojureJDBC (orm_queries_clojure.go) emits QUERIES edges (caller → Class:<Table>, operation, orm=next.jdbc) for two next.jdbc idioms: the raw-SQL vector form (jdbc/execute!|execute-one!|execute / sql/query ds ["SELECT … FROM users …" …]) — table+verb parsed from the SQL string via extractSQLTable/sqlOp — and the next.jdbc.sql friendly fns (sql/insert!|update!|delete!|query|find-by-keys|get-by-id ds :users …) where the table is the keyword arg after the datasource. Proven by TestClojure_NextJDBC_SQLString / _FriendlyFns. Partial: dynamic/interpolated SQL and non-literal table args are honest-skipped; transaction-function stamping still tracked under #4910. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | 4910 | — | — |
| Migration schema ops | 🔴 `missing` | — | 4910 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 4910 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.clojure.driver.next-jdbc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
