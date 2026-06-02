<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.alembic` — Alembic (migration tool)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

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
| Migration parsing | 🟢 `partial` | `2026-06-02` | 3639 | `internal/custom/python/alembic_schema.go`<br>`internal/engine/migration_sequence.go`<br>`internal/engine/rules/python/orms/alembic.yaml`<br>`internal/enrichers/migration_sequence_enricher.go` | Previously FALSE-full (#3630): the cited migration_sequence_enricher.go was orphaned (imported by zero production code) so no sequence/ordering metadata was ever emitted. #3639 wires it as live Pass 8.9 (engine.ApplyMigrationSequence, cmd/archigraph/index.go): each Alembic versions/*.py entity is stamped sequence_number/migration_name/migration_pattern from the filename, and the pass reads the file body (enrichers.ParseAlembicRevisions) to emit a PRECEDES edge along the down_revision to revision DAG, making the migration chain traversable. Separately alembic_schema.go parses upgrade() op content. Partial: op.alter_column type changes, dropped columns, and server_default/constraint metadata are still not modeled. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.alembic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
