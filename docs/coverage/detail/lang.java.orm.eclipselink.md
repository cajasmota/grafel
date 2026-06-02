<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.eclipselink` — EclipseLink

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/java/eclipselink.go`<br>`internal/custom/java/orm_extractors_test.go` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/eclipselink.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | Captures @Table table_name + @Cache L2 marker + @Column(name/nullable/length) attribute depth; full DDL introspection not parsed. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/java/eclipselink.go`<br>`internal/custom/java/orm_extractors_test.go` | No EclipseLink-specific extractor. EclipseLink is a JPA provider, but its proprietary extensions (@Cache, @ReadTransformer, etc.) are not covered. Hibernate extractor handles standard JPA subset only. |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/eclipselink.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | @JoinColumn(name=) and @ForeignKey(name=) parsed; emits SCOPE.Component/foreign_key entities with column_name and constraint_name properties |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/eclipselink.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | FetchType.LAZY and FetchType.EAGER parsed; emits SCOPE.Component/fetch_config entities |
| Relationship extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/java/eclipselink.go`<br>`internal/custom/java/orm_extractors_test.go` | No EclipseLink-specific extractor. Proprietary EclipseLink relationship annotations not extracted. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/java/eclipselink.go`<br>`internal/custom/java/orm_extractors_test.go` | — |

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
(or use `go run ./tools/coverage update lang.java.orm.eclipselink ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
