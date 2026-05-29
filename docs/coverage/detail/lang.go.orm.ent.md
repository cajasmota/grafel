<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.ent` — ent (Facebook)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |
| Schema extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |
| Lazy loading recognition | — `not_applicable` | — | — | — | ent loading is query-time via .With<Edge>() eager-load calls; there is no static eager/lazy declaration on the schema to extract. |
| Relationship extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.ent ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
