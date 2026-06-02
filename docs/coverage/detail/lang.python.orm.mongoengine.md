<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.mongoengine` — MongoEngine

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/python/orms/mongoengine.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3072 | `internal/custom/python/orm_schema.go` | MongoEngine Document field definitions (StringField, IntField etc.) are not extracted at the ORM schema level; the MongoDB custom extractor handles aggregations/change streams but not model field schema. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/mongoengine_relationships.py` | — |
| Foreign key extraction | — `not_applicable` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/mongoengine_relationships.py` | — |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/mongoengine_relationships.py` | MongoEngine uses ReferenceField with lazy loading but no extractor tracks lazy_loading strategy properties. |
| Relationship extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/mongoengine_relationships.py` | — |

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
(or use `go run ./tools/coverage update lang.python.orm.mongoengine ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
