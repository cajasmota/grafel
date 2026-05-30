<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.spring-data` — Spring Data (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/spring_data_kotlin.yaml` | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/java/hibernate.go` | Recording win: hibernate.go accepts kotlin language with spring_data_jpa framework. Spring Data JPA entities use the same @Entity/@Table annotations as Hibernate — regex patterns match Kotlin data class declarations. spring_ecosystem.go is Java-only but the JPA schema layer is shared. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/java/hibernate.go`<br>`internal/custom/java/jpa_fk_lazy.go` | Recording win: same as orm.hibernate — hibernate.go hibAssociationRE matches @OneToMany/@ManyToOne on Kotlin Spring Data JPA entities. @JoinColumn / @ForeignKey handled by jpa_fk_lazy.go ExtractJPAFKAndLazy. |
| Foreign key extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; same jpa_fk_lazy.go covers Spring Data JPA entities with @JoinColumn/@ForeignKey |
| Lazy loading recognition | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; FetchType.LAZY/EAGER on Kotlin Spring Data JPA entities |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | 3274 | `internal/custom/java/hibernate.go`<br>`internal/custom/java/jpa_fk_lazy.go`<br>`internal/custom/java/kotlin_port_test.go` | java extractor language-gated to kotlin; @OneToMany/@ManyToOne on Kotlin Spring Data entities covered by hibernate.go hibAssociationRE |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_query.go`<br>`internal/custom/kotlin/orm_query_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/kotlin/jpa_compose_ext.go` | New extractor: kotlinJPAMigrationExtractor covers Flyway/Liquibase migration declarations in Kotlin — same patterns apply to Spring Data JPA projects (both use Flyway/Liquibase for schema migration). SpringLiquibase bean detection is explicit. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.orm.spring-data ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
