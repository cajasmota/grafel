<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.hibernate` — Hibernate ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_field_edges.go`<br>`internal/engine/rules/java/orms/hibernate_core.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/hibernate.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | @Table table_name + @Column(name/nullable/length) attribute depth parsed; full DDL index/sequence introspection not parsed. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/hibernate.go` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/hibernate.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | @JoinColumn(name=) and @ForeignKey(name=) parsed; emits SCOPE.Component/foreign_key entities with column_name and constraint_name properties |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/hibernate.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | FetchType.LAZY and FetchType.EAGER parsed from association annotations; emits SCOPE.Component/fetch_config entities with fetch_type property |
| Relationship extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/hibernate.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.hibernate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
