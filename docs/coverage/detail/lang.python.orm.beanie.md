<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.beanie` — Beanie (async MongoDB ODM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-06-03` | 3072 | `internal/custom/python/orm_schema.go`<br>`internal/custom/python/orm_schema_test.go` | Cite corrected (#3637): python_beanie_schema (BeanieSchemaExtractor) emits a SCOPE.Schema document-model entity per Beanie Document subclass plus annotated field entities (value-asserting TestBeanieSchema_DocumentEmitted / TestBeanieSchema_FieldsEmitted). Partial: Pydantic-typed field depth limited. |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3072 | `internal/custom/python/orm_schema.go` | Beanie Document field definitions (via Pydantic annotations) are not extracted at the ORM level; the schema_detector.go detects Pydantic usage generally but does not emit per-field ORM schema entities. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | — |
| Foreign key extraction | — `not_applicable` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | — |
| Lazy loading recognition | 🟢 `partial` | `2026-05-29` | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | Beanie (Motor-based async ODM) does not have a lazy-loading mechanism; all queries are explicit async calls. |
| Relationship extraction | 🟢 `partial` | — | 3070 | `internal/custom/python/orm_relationships.go`<br>`internal/custom/python/testdata/beanie_relationships.py` | — |

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

## Related extraction records

This record provides code-level coverage for the
[`db.mongodb`](./db.mongodb.md) hub record (MongoDB (collections)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.beanie ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
