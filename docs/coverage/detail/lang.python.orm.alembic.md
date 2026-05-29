<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.alembic` — Alembic (migration tool)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Foreign key extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | — `not_applicable` | — | — | — | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-29` | 3060 | `internal/engine/rules/python/orms/alembic.yaml`<br>`internal/enrichers/migration_sequence_enricher.go` | YAML rule detects alembic.ini and versions/*.py files; migration_sequence_enricher.go parses Alembic filename convention ([A-Za-z0-9]{12}_<name>.py). upgrade()/downgrade() operation content is not yet parsed (full would require a dedicated alembic migration extractor analogous to django_migration.go). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.alembic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
