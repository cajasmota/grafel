<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.driver.npgsql` — Npgsql (PostgreSQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | — `not_applicable` | — | — | — | Raw ADO.NET/graph driver — no attribute-based schema mapping; models are plain SQL or driver-specific graph queries |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |
| Foreign key extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |
| Relationship extraction | — `not_applicable` | — | — | — | raw driver — no ORM relationship/association/FK/lazy-load layer |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3644) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

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
[`db.postgres`](./db.postgres.md) infra record (PostgreSQL (schema)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.driver.npgsql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
