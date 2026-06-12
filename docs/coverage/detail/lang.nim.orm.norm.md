<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.orm.norm` — Norm (Nim ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-12` | 4904 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/norm_orm.go` | Norm is the de-facto Nim ORM; a persisted model is a `T* = ref object of Model` declaration. nimNormModelRe recognises each such type and emits one SCOPE.Schema/model + one SCOPE.Schema/table (table identity = the model type name) per model, carrying framework=norm + provenance. Pre-filtered by nimNormHasModel so plain Nim objects are ignored. Proven by TestNimNormORM_ModelTableColumns + TestNimNormORM_NonModelNoop. |
| Model lifecycle extraction | 🔴 `missing` | — | 4932 | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-12` | 4904 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/norm_orm.go` | Each public object field of a model becomes a SCOPE.Schema/column carrying column_type (Option[T]/seq[T] generic wrappers unwrapped to the inner type) and the owning model name. Partial (honest): Norm column-type pragmas ({.unique.}, {.dbType.}, {.tableName.} table-name overrides) and index declarations are not yet read — the table name is taken from the Nim type name, not a pragma override. Pragma/table-name override extraction is follow-up #4932. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Norm models relations as plain typed object fields, not a declarative association DSL — there is no association macro to extract. |
| Foreign key extraction | 🟢 `partial` | `2026-06-12` | 4904 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/norm_orm.go` | A field typed as another model declared in the same file yields a REFERENCES edge model->referenced-model (fk_field + to_model props) and stamps foreign_key=true / column_type on the column; Option[Model]/seq[Model] wrappers are unwrapped first. Proven by TestNimNormORM_ModelTableColumns + TestNimNormORM_OptionWrappedFK. Partial (honest): cross-file FK targets emit a REFERENCES edge to the bare type name but are not resolved to the concrete entity here, and explicit {.fk: Other.} pragmas are not yet read. Follow-up #4932. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Norm loads related rows via explicit `db.select` calls, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | 🟢 `partial` | — | 4932 | `internal/custom/nim/norm_orm.go` | Field-typed-as-model relationships surface as REFERENCES edges (see foreign_key_extraction). Norm has no separate declarative association DSL (relationships are plain typed fields), so association_extraction/lazy_loading are not_applicable; full bidirectional relationship modelling is follow-up #4932. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | — | 4932 | — | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | 4932 | — | — |
| Migration schema ops | 🔴 `missing` | — | 4932 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 4932 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.orm.norm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
