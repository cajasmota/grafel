<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.spring-data` — Spring Data (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/spring_data_kotlin.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-04` | — | `internal/custom/java/hibernate.go`<br>`internal/custom/java/kotlin_port_test.go` | Parity-flip (epic #3872): the `ExtractHibernate` (internal/custom/java/hibernate.go:69) gate accepts ctx.Language=="kotlin" for the spring_data_jpa framework — Spring Data JPA entities use the same @Entity/@Table annotations as Hibernate, emitting SCOPE.Schema entities with the table_name property. Live-verified by TestKotlinHibernate_SchemaTableName_Parity3872, which drives the real dispatch with Framework="spring_data_jpa" on a Kotlin data class and asserts table_name=="orders". Mirrors java.orm.spring-data-jpa schema_extraction=partial; partial because Kotlin-specific JPA idioms are not all captured and spring_ecosystem.go remains Java-only. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/java/jpa_fk_lazy.go` | Recording win: same as orm.hibernate — hibernate.go hibAssociationRE matches @OneToMany/@ManyToOne on Kotlin Spring Data JPA entities. @JoinColumn / @ForeignKey handled by jpa_fk_lazy.go ExtractJPAFKAndLazy. |
| Foreign key extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; same jpa_fk_lazy.go covers Spring Data JPA entities with @JoinColumn/@ForeignKey |
| Lazy loading recognition | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; FetchType.LAZY/EAGER on Kotlin Spring Data JPA entities |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go` | java extractor language-gated to kotlin; @OneToMany/@ManyToOne on Kotlin Spring Data entities covered by hibernate.go hibAssociationRE |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_query.go`<br>`internal/custom/kotlin/orm_query_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: kotlinJPAMigrationExtractor covers Flyway/Liquibase migration declarations in Kotlin — same patterns apply to Spring Data JPA projects (both use Flyway/Liquibase for schema migration). SpringLiquibase bean detection is explicit. |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.orm.spring-data ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
