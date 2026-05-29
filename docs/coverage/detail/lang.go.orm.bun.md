<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.bun` — Bun (uptrace)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/bun.go`<br>`internal/custom/golang/bun_test.go` | — |
| Schema extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/bun.go`<br>`internal/custom/golang/bun_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/bun.go`<br>`internal/custom/golang/bun_test.go` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/bun.go`<br>`internal/custom/golang/bun_test.go` | — |
| Lazy loading recognition | — `not_applicable` | — | — | — | bun relations are explicit and loaded at query time via .Relation("..."); there is no static eager/lazy declaration to extract. |
| Relationship extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/bun.go`<br>`internal/custom/golang/bun_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/bun.go`<br>`internal/custom/golang/bun_test.go` | Builder verbs (NewSelect/Insert/Update/Delete/Raw) detected; model binding resolves literal .Model(&T{})/(*T)(nil) forms but not bare-variable .Model(v) (needs data flow). |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/bun.go`<br>`internal/custom/golang/bun_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.bun ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
