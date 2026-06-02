<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.hibernate` — Hibernate ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_field_edges.go`<br>`internal/engine/rules/java/orms/hibernate_core.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | @Table table_name + @Column(name/nullable/length) attribute depth parsed; full DDL index/sequence introspection not parsed. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/hibernate.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Re-wired live via custom_java_patterns dispatch (#3586): @OneToMany/@ManyToOne/@OneToOne/@ManyToMany associations emit DEPENDS_ON edges through RunCustomExtractors; value-asserting smoke test TestJavaPatternsJpaEntityLive asserts the Order->LineItem @OneToMany DEPENDS_ON edge emits live |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | @JoinColumn(name=) and @ForeignKey(name=) parsed; emits SCOPE.Component/foreign_key entities with column_name and constraint_name properties |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | FetchType.LAZY and FetchType.EAGER parsed from association annotations; emits SCOPE.Component/fetch_config entities with fetch_type property |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/hibernate.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Re-wired live via custom_java_patterns dispatch (#3586): JPA association annotations emit directed DEPENDS_ON relationship edges through RunCustomExtractors; value-asserting smoke test TestJavaPatternsJpaEntityLive asserts the Order->LineItem DEPENDS_ON edge emits live |

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
(or use `go run ./tools/coverage update lang.java.orm.hibernate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
