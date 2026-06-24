<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.clojure.driver.clojure-java-jdbc` — clojure.java.jdbc (legacy)

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
| Query attribution | 🟢 `partial` | — | 5362 | `internal/engine/orm_queries_clojure.go`<br>`internal/engine/orm_queries_clojure_test.go` | #5362: scanClojureJDBC (orm_queries_clojure.go) also covers the legacy clojure.java.jdbc surface — it gates on the clojure.java.jdbc import and the shared (jdbc/execute! …) / (jdbc/query …) markers and emits QUERIES edges (caller → Class:<Table>, operation) from the SQL-string vector form via extractSQLTable/sqlOp. Proven by TestClojure_NextJDBC_SQLString (same code path). Partial: dynamic SQL is honest-skipped; the legacy (jdbc/insert! db :table {…}) keyword-table sugar is tracked under #4910. |

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
(or use `go run ./tools/coverage update lang.clojure.driver.clojure-java-jdbc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
