<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.orm.mongoid` — Mongoid

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/ruby/orms/mongoid.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/routes.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | Mongoid has_many, belongs_to, has_one, has_and_belongs_to_many, embeds_many, embeds_one, embedded_in. Part of #3282. |
| Foreign key extraction | — `not_applicable` | — | — | — | Mongoid uses document references, not relational foreign keys |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | Mongoid associations are lazy by default; includes/eager_load markers detected. Part of #3282. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | Mongoid has_many, belongs_to, embeds_many, embeds_one relationship macros. Part of #3282. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Datastore

This driver/ORM record provides code-level coverage for the
[`db.mongodb`](./db.mongodb.md) infra record (MongoDB (collections)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.orm.mongoid ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
