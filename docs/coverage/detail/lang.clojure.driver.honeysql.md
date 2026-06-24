<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.clojure.driver.honeysql` — HoneySQL

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
| Query attribution | 🟢 `partial` | — | 5362 | `internal/engine/orm_queries_clojure.go`<br>`internal/engine/orm_queries_clojure_test.go` | #5362: scanClojureHoneySQL (orm_queries_clojure.go) emits QUERIES edges (caller → Class:<Table>, operation, orm=honeysql) from the HoneySQL data DSL — the table is read from the :from clause ({:select [..] :from [:users] :where …} → find) and from the insert-into / update / delete-from DML clauses ({:insert-into :orders …} → create, {:update :users …} → update, {:delete-from :sessions …} → delete). Proven by TestClojure_HoneySQL. Partial: non-literal table keywords and JOINs are honest-skipped; full predicate/column attribution still under #4910. |

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
(or use `go run ./tools/coverage update lang.clojure.driver.honeysql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
