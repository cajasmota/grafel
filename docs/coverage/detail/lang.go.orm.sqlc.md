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
| Model extraction | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/go/orms/sqlc.yaml` | — |
| Schema extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Foreign key extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/go/orms/sqlc.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/go/orms/sqlc.yaml` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.orm.sqlc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
