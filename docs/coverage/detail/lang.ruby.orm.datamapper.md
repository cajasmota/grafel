<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.orm.datamapper` — DataMapper / Hanami Model (legacy)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/ruby/orms/datamapper_hanami_model_legacy.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | DataMapper associations (has n, belongs_to) are lazy by default. Part of #3282. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/ruby/orms/datamapper_hanami_model_legacy.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/ruby/activerecord.go` | DataMapper::Migration / migration(n, :name) blocks, create_table/add_column via dm-migrations. Part of #3282. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.orm.datamapper ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
