<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.spring-data-cassandra` — Spring Data Cassandra

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |
| Schema extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_ecosystem.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Foreign key extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Lazy loading recognition | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Relationship extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | `2026-05-29` | — | — | ORM model-definition layer; database migration files are owned by Flyway/Liquibase, not the ORM itself. Same rationale as lang.java.orm.jooq and lang.java.orm.neo4j N/A. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.spring-data-cassandra ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
