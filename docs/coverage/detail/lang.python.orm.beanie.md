<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.beanie` — Beanie (async MongoDB ODM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/beanie.yaml` | — |
| Schema extraction | ❌ `missing` | `2026-05-29` | backfill:dictionary-completeness | — | Beanie Document field definitions (via Pydantic annotations) are not extracted at the ORM level; the schema_detector.go detects Pydantic usage generally but does not emit per-field ORM schema entities. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ⚠️ `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | — |
| Foreign key extraction | — `not_applicable` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | — |
| Lazy loading recognition | ⚠️ `partial` | `2026-05-29` | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | Beanie (Motor-based async ODM) does not have a lazy-loading mechanism; all queries are explicit async calls. |
| Relationship extraction | ⚠️ `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/beanie.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.beanie ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
