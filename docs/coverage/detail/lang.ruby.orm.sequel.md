<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.orm.sequel` — Sequel

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/ruby/orms/sequel.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/routes.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | Sequel many_to_one/one_to_many/many_to_many associations are lazy by default. Part of #3282. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-02` | 3950 | `internal/engine/rules/ruby/orms/sequel.yaml`<br>`internal/substrate/effect_sinks_ruby.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/ruby/activerecord.go` | Sequel.migration, DB.create_table/alter_table, add_column inside Sequel migration blocks. Part of #3282. |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | 3950 | `internal/extractors/ruby/ruby.go`<br>`internal/txscope/txscope.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.orm.sequel ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
