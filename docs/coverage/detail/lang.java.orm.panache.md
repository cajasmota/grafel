<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.panache` — Quarkus Panache (SQL + Reactive + MongoDB)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/java/panache.go`<br>`internal/extractors/java/panache_test.go` | Panache entity records synthesized from PanacheEntity/PanacheEntityBase subclasses; field schema parsed via Hibernate extractor (hibernate.go) for JPA annotations; panache.go does not independently extract column-level schema |
| Schema extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/hibernate.go`<br>`internal/extractors/java/panache.go`<br>`internal/extractors/java/panache_test.go` | Panache entities use standard JPA @Entity/@Table/@Column annotations parsed by the Hibernate extractor; panache.go synthesizes entity records but does not independently parse field schemas |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/hibernate.go`<br>`internal/extractors/java/panache.go`<br>`internal/extractors/java/panache_test.go` | Panache entity classes use JPA @OneToMany/@ManyToOne/@OneToOne/@ManyToMany annotations parsed by the Hibernate extractor; panache.go synthesizes operation entities for CRUD but does not itself parse association annotations |
| Foreign key extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/hibernate.go`<br>`internal/extractors/java/panache.go`<br>`internal/extractors/java/panache_test.go` | Relationships inferred via JPA annotation parsing in hibernate.go; panache.go synthesizes repository binding edges but no direct FK/join column resolution |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3096) | `internal/extractors/java/panache.go`<br>`internal/extractors/java/panache_test.go` | Synthesizes full static-method and DSL-builder operation entities (findById, count, list, page, stream, etc.) and named query entities; does not parse inline JPQL/HQL string content |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.panache ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
