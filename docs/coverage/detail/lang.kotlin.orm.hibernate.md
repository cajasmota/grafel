<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.hibernate` — Hibernate (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/hibernate_kotlin.yaml` | — |
| Schema extraction | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/hibernate.go` | Recording win: hibernate.go ExtractHibernate() accepts ctx.Language=="kotlin" — @Entity/@Table on Kotlin data classes matched identically to Java. Partial because Kotlin-specific JPA idioms (constructor-param columns, data class shorthand) may not all be captured. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/java/jpa_fk_lazy.go` | Recording win: hibernate.go hibAssociationRE matches @OneToMany/@ManyToOne/@OneToOne/@ManyToMany before Kotlin 'var/val' properties. jpa_fk_lazy.go ExtractJPAFKAndLazy also runs on Kotlin (language gate: java or kotlin). Partial because collection types differ from Java generics. |
| Foreign key extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; @JoinColumn/@ForeignKey on Kotlin entities proven by TestKotlinHibernate_Issue3274 |
| Lazy loading recognition | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; FetchType.LAZY/EAGER on Kotlin val/var properties proven by TestKotlinHibernate_Issue3274 |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go` | java extractor language-gated to kotlin; @OneToMany/@ManyToOne/@ManyToMany on Kotlin proved by TestKotlinHibernate_Issue3274 |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/hibernate_kotlin.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: kotlinJPAMigrationExtractor emits migration entities from Flyway versioned/repeatable class declarations (V2__...), BaseJavaMigration extends, Liquibase @ChangeSet annotations, flyway.migrate() calls, and config bean detection. Partial because SQL migration files (V1__init.sql) are handled by internal/extractors/sql/sql.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.orm.hibernate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
