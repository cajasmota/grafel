<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.ent` — ent (Facebook)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/go/orms/ent.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-29` | 3255 | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | 3255 | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | 3255 | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | ent manages FKs implicitly via edges; owning edge.To captures relation and target model, but concrete FK column name is code-generated (not statically declared) |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | 3255 | `internal/custom/golang/ent.go` | ent eager-loads via .WithEdges() query-time chainers; no static eager/lazy declaration on schema fields; loading strategy is always query-time-determined |
| Relationship extraction | ✅ `full` | `2026-05-29` | 3255 | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-29` | 3255 | `internal/custom/golang/ent.go`<br>`internal/custom/golang/ent_test.go` | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.ent ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
