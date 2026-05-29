<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.ebean` — Ebean ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/java/ebean.go`<br>`internal/custom/java/orm_extractors_test.go` | No Ebean extractor; Ebean uses non-JPA @Entity from io.ebean package. |
| Schema extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/ebean.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | Captures @Table table_name + Model base-class marker + @Column(name/nullable/length) attribute depth; full DDL introspection not parsed. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/java/ebean.go`<br>`internal/custom/java/orm_extractors_test.go` | No Ebean extractor exists. Ebean has its own annotation style (@Entity from io.ebean). A dedicated extractor would be needed; tracked in issue #3001. |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/ebean.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | @JoinColumn(name=) and @ForeignKey(name=) parsed; emits SCOPE.Component/foreign_key entities |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3097) | `internal/custom/java/ebean.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/jpa_fk_lazy_test.go` | FetchType.LAZY and FetchType.EAGER parsed from association annotations; emits SCOPE.Component/fetch_config entities |
| Relationship extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/java/ebean.go`<br>`internal/custom/java/orm_extractors_test.go` | No Ebean extractor exists. Ebean has its own annotation style (@Entity from io.ebean). A dedicated extractor would be needed; tracked in issue #3001. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/java/ebean.go`<br>`internal/custom/java/orm_extractors_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | — | — | No Java ORM migration extractor. Flyway/Liquibase migration parsing is tracked separately as its own category; not a responsibility of this ORM record. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.ebean ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
