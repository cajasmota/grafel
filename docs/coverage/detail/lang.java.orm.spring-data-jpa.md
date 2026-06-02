<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.spring-data-jpa` — Spring Data JPA

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_field_edges.go`<br>`internal/engine/rules/java/orms/spring_data_jpa.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | @Column(name/nullable/length) depth parsed via spring_ecosystem.go FK helper; table_name and entity extraction remain via hibernate.go. Full DDL not parsed. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/hibernate.go` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | @JoinColumn(name=) and @ForeignKey(name=) parsed via ExtractJPAFKAndLazy from spring_ecosystem.go; emits SCOPE.Component/foreign_key entities |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | FetchType.LAZY and FetchType.EAGER parsed from association annotations; emits SCOPE.Component/fetch_config entities |
| Relationship extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/hibernate.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries.go` | — |

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
(or use `go run ./tools/coverage update lang.java.orm.spring-data-jpa ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
