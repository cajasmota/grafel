<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.gorm` — GORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_field_edges.go`<br>`internal/engine/rules/go/frameworks/gorm.yaml`<br>`internal/engine/rules/go/orms/gorm.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |
| Lazy loading recognition | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |
| Relationship extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/engine/orm_queries.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.gorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
