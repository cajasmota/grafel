<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.orm.redbeanphp` — RedBeanPHP

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/php/orms/redbeanphp.yaml` | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | RedBeanPHP R::dispense/'table' implicit schema; R::related/associate relations. Zero-config ORM — no explicit FK/lazy/migration concepts. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | RedBeanPHP R::dispense/'table' implicit schema; R::related/associate relations. Zero-config ORM — no explicit FK/lazy/migration concepts. |
| Foreign key extraction | — `not_applicable` | — | — | — | RedBeanPHP zero-config ORM: foreign keys are auto-managed server-side via convention (ownXList/sharedXList), not extractable from PHP source. |
| Lazy loading recognition | — `not_applicable` | — | — | — | RedBeanPHP loads eagerly by default; no lazy-loading configuration surface in PHP code. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | RedBeanPHP R::dispense/'table' implicit schema; R::related/associate relations. Zero-config ORM — no explicit FK/lazy/migration concepts. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/php/orms/redbeanphp.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | RedBeanPHP is zero-config — schema is created/altered automatically at runtime; no migration files to parse. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.orm.redbeanphp ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
