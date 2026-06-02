<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.orm.cycleorm` — CycleORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | — | — | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | — | — | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/php/orm_data.go` | CycleORM #[Entity]/#[Column] attributes; HasMany/BelongsTo/ManyToMany relations; lazy Promise proxy; findByPK/findOne/select queries; MigrationInterface class and schema sync. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.orm.cycleorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
