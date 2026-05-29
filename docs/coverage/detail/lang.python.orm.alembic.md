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
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3192 | `internal/custom/python/alembic_schema.go`<br>`internal/custom/python/alembic_schema_test.go`<br>`internal/custom/python/testdata/alembic_schema.py` | alembic_schema.go scans Alembic migrations (gated on `from alembic import op`) for op.create_table / op.add_column / op.create_index and emits SCOPE.Schema table, column, and index entities. Heuristic regex over migration source (not a full Python parse), distinct from and non-overlapping with driver_schema.go (#3189); partial because dropped/altered columns, server_default/constraint metadata, and op.alter_column type changes are not modeled. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Migration tool only; no ORM model associations (#3184) |
| Foreign key extraction | — `not_applicable` | — | — | — | Migration-execution layer; FK already partial via sqlalchemy; no model associations (#3184) |
| Lazy loading recognition | — `not_applicable` | — | — | — | Migration tool — no lazy loading concept; alembic only executes schema migrations (#3184) |
| Relationship extraction | — `not_applicable` | — | — | — | Migration tool — no relationship model; no ORM entity classes or associations (#3184) |

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
