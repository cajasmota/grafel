<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.gen` — gen (gentleman / GORM gen)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/gen.go` | — |
| Schema extraction | — `not_applicable` | — | — | — | field detail on wrapped GORM structs (gorm.go) |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | associations are GORM tags (gorm.go) |
| Foreign key extraction | — `not_applicable` | — | — | — | FKs are GORM tags (gorm.go) |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/golang/gen.go` | — |
| Relationship extraction | — `not_applicable` | — | — | — | relationships are wrapped GORM tags (gorm.go) |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/gen.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | gen generates query code, not migrations (gorm.go) |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.gen ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
