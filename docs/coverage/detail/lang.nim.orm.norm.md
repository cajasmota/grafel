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
| Schema extraction | ✅ `full` | `2026-06-12` | 4932 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/norm_orm.go` | Each public object field of a model becomes a SCOPE.Schema/column carrying column_type (Option[T]/seq[T] generic wrappers unwrapped to the inner type) and the owning model name. #4932 deepened: a model-header `{.tableName: "x".}` / `{.dbName: "x".}` pragma keys the table entity by the override name and stamps table_name on the model (table identity is no longer forced to the Nim type name); field-level pragmas are read — `{.unique.}` -> unique=true, `{.dbType: "TEXT".}` -> db_type=TEXT on the column. Proven by TestNimNormORM_Deepen_4932 (tableName override + unique + dbType asserted) + TestNimNormORM_ModelTableColumns. Honest remainder: index pragmas beyond unique are not modelled (follow-up #4932). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Norm models relations as plain typed object fields, not a declarative association DSL — there is no association macro to extract. |
| Foreign key extraction | 🟢 `partial` | `2026-06-12` | 4904 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/norm_orm.go` | A field typed as another model declared in the same file yields a REFERENCES edge model->referenced-model (fk_field + to_model props) and stamps foreign_key=true / column_type on the column; Option[Model]/seq[Model] wrappers are unwrapped first. #4932 deepened: an explicit `{.fk: Other.}` pragma on a scalar-typed field (e.g. `authorId* {.fk: User.}: int64`) now yields a REFERENCES edge (fk_pragma=true) + a foreign_key=true/fk_target column even though the field type is not itself a Model. Proven by TestNimNormORM_ModelTableColumns + TestNimNormORM_OptionWrappedFK + TestNimNormORM_Deepen_4932. Partial (honest): cross-file FK targets emit a REFERENCES edge to the bare type name but are not resolved to the concrete entity here. Cross-file resolution remains follow-up #4932. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Norm loads related rows via explicit `db.select` calls, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | 🟢 `partial` | — | 4932 | `internal/custom/nim/norm_orm.go` | Field-typed-as-model relationships surface as REFERENCES edges (see foreign_key_extraction). Norm has no separate declarative association DSL (relationships are plain typed fields), so association_extraction/lazy_loading are not_applicable; full bidirectional relationship modelling is follow-up #4932. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-12` | 4932 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/norm_orm.go` | A `db.select/insert/update/delete(Model, ...)` call site whose first argument names a recognised model TYPE emits a QUERIES edge from the model entity to its table (operation/table/model props), one edge per distinct operation. collectNormQueries + nimNormQueryRe. Proven by TestNimNormORM_Deepen_4932 (Post -> insert+update, User -> select to table users). Partial (honest): only the model-typed first-argument form (`db.select(User, ...)`) is attributed; a query through a variable handle (`db.select(user, ...)`) and rawSelect/sql(...) string queries are not resolved file-locally. Variable-handle + raw-SQL attribution is follow-up #4932. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | 4932 | — | — |
| Migration schema ops | 🔴 `missing` | — | 4932 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🟢 `partial` | `2026-06-12` | 4932 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/norm_orm.go` | A Norm `db.transaction:` / `<conn>.transaction:` block header emits a SCOPE.Pattern/transaction_boundary entity (transactional=true, framework=norm, db_handle, provenance INFERRED_FROM_NORM_TRANSACTION), mirroring the Kotlin/Java @Transactional boundary shape. collectNormTransactions + nimNormTxRe. Proven by TestNimNormORM_Deepen_4932. Partial (honest): the boundary is recorded at the block header but the enclosing proc is not resolved/stamped and writes inside the block are not flagged on it (no cross-line proc binding). Enclosing-proc stamping is follow-up #4932. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.orm.norm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
