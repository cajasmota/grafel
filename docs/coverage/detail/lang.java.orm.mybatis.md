<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.mybatis` — MyBatis

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3096) | `internal/custom/java/mybatis.go`<br>`internal/custom/java/orm_extractors_test.go`<br>`internal/engine/rules/java/orms/mybatis.yaml` | Annotation @Results + XML <resultMap> become result_map schema entities (SCOPE.Schema); column DDL not parsed; model class references extracted from resultMap type attribute only |
| Schema extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3007) | `internal/custom/java/mybatis.go`<br>`internal/custom/java/orm_extractors_test.go` | Annotation @Results + XML <resultMap> become result_map schema entities; column DDL not parsed. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3007) | `internal/custom/java/mybatis.go`<br>`internal/custom/java/orm_extractors_test.go` | result_map result_type captured; <association>/<collection> nested joins not yet parsed. |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/jpa_fk_lazy.go` | MyBatis mappers may co-exist with JPA annotations in hybrid projects; jpa_fk_lazy.go handles @JoinColumn/@FetchType patterns when present. Coverage is indirect (#3180). |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/jpa_fk_lazy.go` | MyBatis mappers may co-exist with JPA annotations in hybrid projects; jpa_fk_lazy.go handles @JoinColumn/@FetchType patterns when present. Coverage is indirect (#3180). |
| Relationship extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3007) | `internal/custom/java/mybatis.go`<br>`internal/custom/java/orm_extractors_test.go` | Result maps + mapper->statement ownership; FK/join-result associations not modeled. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/java/mybatis.go`<br>`internal/custom/java/orm_extractors_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | `2026-05-29` | — | — | MyBatis is an SQL mapper; database migration files are owned by Flyway/Liquibase, not MyBatis. Same rationale as T1 ORM sweep (#3180). |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.mybatis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
