<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.orm.rom-rb` — ROM (Ruby Object Mapper)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/ruby/frameworks/rom_rb_ruby_object_mapper.yaml`<br>`internal/engine/rules/ruby/orms/rom_rb_as_orm.yaml` | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/routes.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | ROM-rb associations (has_many, belongs_to) are lazy by default. Part of #3282. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/ruby/orms/rom_rb_as_orm.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/ruby/activerecord.go` | ROM-rb uses Sequel.migration underneath; migration block detection covers this. Part of #3282. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.orm.rom-rb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
