<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.typeorm` — TypeORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/orms/typeorm.yaml` | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/javascript/typeorm.go` | reTypeORMEntity+reTypeORMColumn+reTypeORMViewEntity extract entity/column/view decorators (#3183) |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_test.go`<br>`internal/custom/javascript/typeorm.go` | — |
| Foreign key extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/typeorm.go` | — |
| Lazy loading recognition | 🟢 `partial` | — | 3071 | `internal/custom/javascript/issue3071_lazy_loading_test.go`<br>`internal/custom/javascript/typeorm.go` | Detects @OneToMany/@ManyToOne/@OneToOne/@ManyToMany relation decorators carrying { lazy: true }; emits SCOPE.Pattern/lazy_relation with lazy_loading=true. Promise<T> return-type inference not yet implemented. |
| Relationship extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/javascript/typeorm.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/typeorm.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.typeorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
