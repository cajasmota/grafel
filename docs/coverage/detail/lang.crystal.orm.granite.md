<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.orm.granite` — Granite (Crystal ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-12` | 4905 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Granite is one of the most widely used Crystal ORMs (the default ORM for the Amber web framework). A persisted model is a `class T < Granite::Base` class. graniteModelRe recognises each such class (pre-filtered by graniteHasModel on the Granite::Base marker) and emits one SCOPE.Schema/model + one SCOPE.Schema/table per model; the table identity is the explicit `table <name>` macro argument (graniteTableRe) when present, otherwise the model class name. Carries framework=granite + provenance. Proven by TestCrystalGraniteORM_ModelTableColumns (explicit `table users`) + TestCrystalGraniteORM_ImplicitTableName + TestCrystalGraniteORM_NonModelNoop. |
| Model lifecycle extraction | 🔴 `missing` | — | 5032 | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-12` | 4935 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Each `column <name> : <Type>[, primary: true]` macro (graniteColumnRe) becomes a SCOPE.Schema/column carrying column_type (nilable `?` marker trimmed) and the owning model, with primary_key=true stamped on the primary column. The `timestamps` macro (graniteTimestampsRe, #4935) additionally synthesises the conventional created_at/updated_at Time columns stamped auto_timestamp=true. Proven by TestCrystalGraniteORM_ModelTableColumns (id primary, body `String?` trimmed) + TestCrystalGraniteORM_Timestamps. Partial (honest): column converters/defaults and index declarations are not yet read — follow-up #5032. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-12` | 4905 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | belongs_to/has_many/has_one macros (graniteAssocRe) each emit a SCOPE.Schema/association entity stamping assoc_kind, the owning model, and the CamelCased target model name. Proven by TestCrystalGraniteORM_ModelTableColumns (has_many :posts, belongs_to :user). Honest follow-up #5032: `has_many through:` and polymorphic associations are not yet resolved. |
| Foreign key extraction | 🟢 `partial` | `2026-06-12` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | A `belongs_to :user` association yields a REFERENCES edge model->User (fk_field + to_model props, target name CamelCased from the symbol). Proven by TestCrystalGraniteORM_ModelTableColumns. Partial (honest): the explicit `foreign_key:` option override and cross-file target resolution are deferred — the edge targets the bare CamelCased name for the shared resolver to bind. Follow-up #5032. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Granite associations are loaded via explicit accessor calls/queries, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | ✅ `full` | `2026-06-12` | 4905 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | belongs_to/has_many/has_one association macros are extracted as association entities (assoc_kind + target) and belongs_to additionally yields a REFERENCES FK edge — see association_extraction/foreign_key_extraction. Full follow-up modelling of through/polymorphic relations is #5032. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-12` | 4935 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Granite's class-method query DSL at a call site referencing a known model — `Model.all/find/find_by/where/first/last/count/exists?` (select), `Model.create/import` (insert), `Model.save/update` (update), `Model.clear/delete` (delete) — emits a QUERIES edge model->table stamped operation+table+model (graniteQueryRe + graniteQueryOp). Only receivers naming a model declared in the file are attributed (honest, file-local), so `Unknown.find` is never falsely counted. Proven by TestCrystalGraniteORM_QueryAttribution (select/insert/delete on User, Unknown skipped). Mirrors the Nim/Norm query-attribution shape. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | 5032 | — | — |
| Migration schema ops | 🔴 `missing` | — | 5032 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-12` | 4935 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | A Crystal-DB `<db>.transaction do … end` block (graniteTxRe) emits one SCOPE.Pattern/transaction_boundary entity stamping transactional=true, framework=granite, and the db_handle receiver, mirroring the Nim/Norm + Kotlin/Java @Transactional boundary shape. Proven by TestCrystalGraniteORM_Transaction. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.orm.granite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
