<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.mybatis` — MyBatis

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

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
| Foreign key extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3007) | `internal/custom/java/mybatis.go`<br>`internal/custom/java/orm_extractors_test.go` | Result maps + mapper->statement ownership; FK/join-result associations not modeled. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/java/mybatis.go`<br>`internal/custom/java/orm_extractors_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | — | — | No Java ORM migration extractor. Flyway/Liquibase migration parsing is tracked separately as its own category; not a responsibility of this ORM record. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.mybatis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
