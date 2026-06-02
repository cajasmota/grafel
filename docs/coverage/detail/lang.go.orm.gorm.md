<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.gorm` — GORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

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
| Lazy loading recognition | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/gorm.go` | GORM has no static eager/lazy declaration on models; loading is query-time via .Preload(name), .Joins(assoc), .Association(name) chain calls — detected as chainer ops but cannot statically infer which associations are always preloaded vs lazy |
| Relationship extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/engine/orm_queries.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/gorm.go`<br>`internal/custom/golang/gorm_test.go` | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/golang/extractor.go`<br>`internal/extractors/golang/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: stamps transactional=true + tx_source/tx_isolation on Go fn entities that open db.Begin()/BeginTx/GORM Transaction. No transitive propagation. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.gorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
