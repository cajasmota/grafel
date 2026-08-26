<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.hibernate` — Hibernate (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/hibernate_kotlin.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-04` | — | `internal/custom/java/hibernate.go`<br>`internal/custom/java/kotlin_port_test.go` | Parity-flip (epic #3872): the `ExtractHibernate` (internal/custom/java/hibernate.go:69) gate accepts ctx.Language=="kotlin" for the hibernate framework — @Entity/@Table on Kotlin data classes emit SCOPE.Schema entities with the table_name property identically to Java. Live-verified by TestKotlinHibernate_SchemaTableName_Parity3872, which drives the real dispatch on a Kotlin data class and asserts table_name=="orders". Mirrors java.orm.hibernate schema_extraction=partial; partial because Kotlin-specific JPA idioms (constructor-param columns, data class shorthand) are not all captured. |

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
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.orm.hibernate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
