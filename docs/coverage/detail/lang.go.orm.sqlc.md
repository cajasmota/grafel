<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.orm.sqlc` — sqlc (codegen)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/sqlc.go` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/golang/sqlc.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | no association metadata in sqlc-generated code |
| Foreign key extraction | — `not_applicable` | — | — | — | FKs in source SQL, not generated Go |
| Lazy loading recognition | — `not_applicable` | — | — | — | no lazy/eager loading; explicit SQL |
| Relationship extraction | — `not_applicable` | — | — | — | sqlc generates plain structs; relationships in SQL JOINs |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/sqlc.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/sqlc.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.sqlc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
