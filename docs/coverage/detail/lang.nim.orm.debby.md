<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.orm.debby` — Debby (Nim ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-12` | 5028 | `internal/custom/nim/debby_orm.go`<br>`internal/custom/nim/extractors_test.go` | Debby maps a PLAIN Nim `object` type (NOT `ref object of Model` like Norm) to a table whose name is the type name. An object type is treated as a persisted model only when the file imports Debby AND the type is named by a Debby db op (`db.createTable/dropTable/insert/get/update/delete/query(Type, ...)`) — the registration/usage is the signal, so arbitrary Nim records are not misfired. nimDebbyObjectRe + collectDebbyObjects + collectDebbyOps emit one SCOPE.Schema/model + one SCOPE.Schema/table (table identity = type name) per registered model, framework=debby + provenance. Pre-filtered by nimDebbyHasDebby. Proven by TestNimDebbyORM_ModelTableColumns + TestNimDebbyORM_UnregisteredObjectNoop + TestNimDebbyORM_NoDebbyNoop + TestNimDebbyORM_WrongLanguageNoop. |
| Model lifecycle extraction | 🔴 `missing` | — | 5031 | — | — |
| Schema extraction | ✅ `full` | `2026-06-12` | 5028 | `internal/custom/nim/debby_orm.go`<br>`internal/custom/nim/extractors_test.go` | Each public object field of a registered Debby model becomes a SCOPE.Schema/column carrying column_type (Option[T]/seq[T] wrappers unwrapped to the inner type, reusing normaliseNimFieldType) and the owning model name. collectDebbyFields. Proven by TestNimDebbyORM_ModelTableColumns (id/name/email/title/author/authorId columns asserted). Honest remainder: Debby column index pragmas beyond {.fk.} are not modelled (follow-up #5031). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Debby models relations as plain typed object fields, not a declarative association DSL — there is no association macro to extract. |
| Foreign key extraction | 🟢 `partial` | `2026-06-12` | 5028 | `internal/custom/nim/debby_orm.go`<br>`internal/custom/nim/extractors_test.go` | A field typed as another registered Debby model yields a REFERENCES edge model->referenced-model (fk_field + to_model props) and stamps foreign_key=true / fk_target on the column; an explicit `{.fk: Other.}` pragma on a scalar field yields a REFERENCES edge (fk_pragma=true) + foreign_key column even when the field type is not itself a model. nimDebbyFkPragmaRe. Proven by TestNimDebbyORM_ModelTableColumns (author typed FK + authorId pragma FK asserted). Partial (honest): cross-file FK targets emit a REFERENCES edge to the bare type name but are not resolved to the concrete entity here — cross-file resolution is follow-up #5031. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Debby loads related rows via explicit `db.get`/`db.query` calls, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | 🟢 `partial` | — | 5028 | `internal/custom/nim/debby_orm.go` | Field-typed-as-model + {.fk.} relationships surface as REFERENCES edges (see foreign_key_extraction). Debby has no separate declarative association DSL, so association_extraction/lazy_loading are not_applicable; full bidirectional relationship modelling is follow-up #5031. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | — | 5028 | `internal/custom/nim/debby_orm.go`<br>`internal/custom/nim/extractors_test.go` | A `db.insert/get/update/delete/query(Model, ...)` call site whose first argument names a recognised model TYPE emits a QUERIES edge from the model entity to its table (operation/table/model props), one edge per distinct operation; createTable/dropTable are treated as schema registration, not queries. collectDebbyOps. Proven by TestNimDebbyORM_ModelTableColumns (Post insert + User get asserted). Partial (honest): only the model-typed first-argument form is attributed; a query through a lowercase instance handle (`db.insert(post)`) and raw `db.query(sql(...))` string queries are not resolved file-locally — variable-handle + raw-SQL attribution is follow-up #5031. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-24` | 5367 | `internal/custom/nim/debby_migrations.go`<br>`internal/custom/nim/extractors_test.go` | Debby creates/drops tables imperatively against a db handle taking the model TYPE (db.createTable(User)/db.dropTable(Post)); custom_nim_debby_migrations parses both verbs (and the receiver-less form) into normalised create_table/drop_table ops. A lowercase instance argument is skipped (Debby schema ops take the type), keeping attribution honest. |
| Migration schema ops | ✅ `full` | `2026-06-24` | 5367 | `internal/custom/nim/debby_migrations.go`<br>`internal/custom/nim/extractors_test.go`<br>`internal/engine/migration_schema_ops.go`<br>`internal/engine/migration_schema_ops_test.go` | Each Debby migration op is emitted as a shared SCOPE.Evolution entity (framework=debby, migration_op, table=model name, provenance INFERRED_FROM_DEBBY_MIGRATION) with the normalised op subtype — the same Kind the Nim Norm/Allographer and JS knex/typeorm migration extractors use. The engine migration-schema-ops pass (case "debby") derives the MODIFIES_TABLE op->table convergence edge; table identity = the model type name (matching debby_orm.go QUERIES->table). |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-24` | 5367 | `internal/custom/nim/debby_migrations.go`<br>`internal/custom/nim/extractors_test.go` | A Debby db.withTransaction: block header emits a SCOPE.Pattern/transaction_boundary entity (transactional=true, framework=debby, db_handle, provenance INFERRED_FROM_DEBBY_TRANSACTION), mirroring the Norm db.transaction: boundary and the Kotlin/Java @Transactional shape. The boundary is stamped with the set of in-block write ops (insert/update/delete) so it records WHAT it wraps. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.orm.debby ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
