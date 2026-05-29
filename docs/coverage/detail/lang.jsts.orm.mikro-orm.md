<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.mikro-orm` — MikroORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/mikroorm.go`<br>`internal/engine/rules/javascript_typescript/orms/mikro_orm.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-29` | 3067 | `internal/custom/javascript/mikroorm.go`<br>`internal/custom/javascript/orm_build_3067_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | 3067 | `internal/custom/javascript/mikroorm.go`<br>`internal/custom/javascript/orm_build_3067_test.go` | — |
| Foreign key extraction | ⚠️ `partial` | — | 3067 | `internal/custom/javascript/mikroorm.go`<br>`internal/custom/javascript/orm_build_3067_test.go` | FK columns inferred from @ManyToOne decorator; no explicit FK column extraction (FK column not separately defined in MikroORM decorator-first pattern) |
| Lazy loading recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | ✅ `full` | `2026-05-29` | 3067 | `internal/custom/javascript/mikroorm.go`<br>`internal/custom/javascript/orm_build_3067_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts_drivers.go`<br>`internal/engine/orm_queries_jsts_drivers_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/mikroorm.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.mikro-orm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
