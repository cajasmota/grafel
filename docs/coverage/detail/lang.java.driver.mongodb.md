<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.driver.mongodb` — MongoDB Java Driver

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | NoSQL driver — no relational schema/ORM model in user code |
| Schema extraction | — `not_applicable` | — | — | — | NoSQL driver — no relational schema/ORM model in user code |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |
| Foreign key extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |
| Relationship extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_drivers_other.go`<br>`internal/engine/orm_queries_drivers_other_test.go` | Driver topology: database.getCollection('x') literal captured via scanJavaDrivers (javaGetCollectionRe); QUERIES edge to Class:<Collection>; dynamic names honest-skipped. |

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
(or use `go run ./tools/coverage update lang.java.driver.mongodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
