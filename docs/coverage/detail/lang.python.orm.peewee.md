<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.peewee` — Peewee

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/peewee.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3072 | `internal/custom/python/orm_schema.go` | Peewee field definitions (CharField, IntegerField etc.) are not extracted; only model class detection and query attribution are handled. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py`<br>`internal/custom/python/testdata/mongoengine_relationships.py`<br>`internal/custom/python/testdata/peewee_relationships.py`<br>`internal/custom/python/testdata/pony_relationships.py`<br>`internal/custom/python/testdata/tortoise_relationships.py` | — |
| Foreign key extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py`<br>`internal/custom/python/testdata/mongoengine_relationships.py`<br>`internal/custom/python/testdata/peewee_relationships.py`<br>`internal/custom/python/testdata/pony_relationships.py`<br>`internal/custom/python/testdata/tortoise_relationships.py` | — |
| Lazy loading recognition | 🔴 `missing` | `2026-05-29` | backfill:dictionary-completeness | — | Peewee does not support lazy loading; all queries are explicit. DeferredForeignKey is a different concept not tracked here. |
| Relationship extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py`<br>`internal/custom/python/testdata/mongoengine_relationships.py`<br>`internal/custom/python/testdata/peewee_relationships.py`<br>`internal/custom/python/testdata/pony_relationships.py`<br>`internal/custom/python/testdata/tortoise_relationships.py` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/peewee.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.peewee ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
