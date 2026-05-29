<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.prisma` — Prisma

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/orms/prisma.yaml`<br>`internal/engine/rules/javascript_typescript/orms/prisma_client_js.yaml`<br>`internal/engine/rules/prisma/_manifest.yaml` | — |
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
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/prisma.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.prisma ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
