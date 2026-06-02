<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.orm.propel` — Propel

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/php/orms/propel.yaml` | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | Propel TableMap COL_* constants (schema); addRelation/addForeignKey (relations/FK); LAZY_LOAD constant; PropelMigration classes; XxxQuery::create() calls. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | Propel TableMap COL_* constants (schema); addRelation/addForeignKey (relations/FK); LAZY_LOAD constant; PropelMigration classes; XxxQuery::create() calls. |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | Propel TableMap COL_* constants (schema); addRelation/addForeignKey (relations/FK); LAZY_LOAD constant; PropelMigration classes; XxxQuery::create() calls. |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | Propel TableMap COL_* constants (schema); addRelation/addForeignKey (relations/FK); LAZY_LOAD constant; PropelMigration classes; XxxQuery::create() calls. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | Propel TableMap COL_* constants (schema); addRelation/addForeignKey (relations/FK); LAZY_LOAD constant; PropelMigration classes; XxxQuery::create() calls. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/php/orms/propel.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/php/orm_data.go` | Propel TableMap COL_* constants (schema); addRelation/addForeignKey (relations/FK); LAZY_LOAD constant; PropelMigration classes; XxxQuery::create() calls. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.orm.propel ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
