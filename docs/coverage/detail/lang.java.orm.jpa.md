<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.jpa` — JPA / Jakarta Persistence API

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/java/orms/jpa_jakarta_persistence_api.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | @Table table_name + @Column(name/nullable/length) attribute depth parsed via hibernate.go shared helper; full DDL introspection not parsed. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/hibernate.go` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | @JoinColumn(name=) and @ForeignKey(name=) parsed via hibernate.go shared helper; emits SCOPE.Component/foreign_key entities |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/jpa_fk_lazy.go` | FetchType.LAZY and FetchType.EAGER parsed; emits SCOPE.Component/fetch_config entities |
| Relationship extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/hibernate.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3096) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_other.go`<br>`internal/engine/orm_queries_test.go` | Engine pass scanJavaORM handles EntityManager.find/persist/remove/merge and Spring Data repository call patterns (findByX, save, etc.); emits QUERIES edges with orm=jpa or orm=spring_data; inline JPQL string content and @NamedQuery body text not parsed |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | `2026-05-29` | — | — | ORM model-definition layer; database migration files are owned by Flyway/Liquibase, not the ORM itself. Same rationale as lang.java.orm.jooq and lang.java.orm.neo4j N/A. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.jpa ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
