<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.driver.elastic` — elasticsearch-ruby

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/routes.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw client driver; no association DSL |
| Foreign key extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw driver — no ORM relationship/lazy-load layer |
| Relationship extraction | — `not_applicable` | — | — | — | raw client driver; no relationship DSL |

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
[`db.elasticsearch`](./db.elasticsearch.md) infra record (Elasticsearch (indices)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.driver.elastic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
