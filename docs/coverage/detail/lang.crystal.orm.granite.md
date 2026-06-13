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
| Model lifecycle extraction | ✅ `full` | `2026-06-14` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Granite/ActiveRecord-style lifecycle callbacks — before_save/after_save/before_create/after_create/before_update/after_update/before_destroy/after_destroy/before_commit/after_commit/after_initialize/after_find (graniteCallbackRe) and the validate macro (graniteValidateRe) — each emit a SCOPE.Operation/function entity stamping framework=granite, callback_type, the owning model, and the target method symbol, mirroring the Rails ActiveRecord callback shape (rails.go). Proven by TestCrystalGraniteORM_Lifecycle (before_save :normalize_email, after_create :send_welcome, validate :email). |
| Schema extraction | ✅ `full` | `2026-06-14` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Each `column <name> : <Type>[, opts…]` macro (graniteColumnRe) becomes a SCOPE.Schema/column carrying column_type (nilable `?` marker trimmed) and the owning model, with primary_key=true stamped on the primary column. The column option tail is now read (#5032): default: <v> -> column_default, converter: <C> -> converter, unique: true -> unique=true. The `timestamps` macro (graniteTimestampsRe, #4935) additionally synthesises the conventional created_at/updated_at Time columns stamped auto_timestamp=true. Proven by TestCrystalGraniteORM_ModelTableColumns (id primary, body `String?` trimmed) + TestCrystalGraniteORM_Timestamps + TestCrystalGraniteORM_ColumnOptions (default/converter/unique). Standalone `index` macro declarations remain a minor honest follow-up. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-14` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | belongs_to/has_many/has_one macros (graniteAssocRe) each emit a SCOPE.Schema/association entity stamping assoc_kind, the owning model, and the resolved target model name (has_many singularised; explicit class_name:/typed `belongs_to x : T` override honoured). Association option tail now read (#5032): `has_many :users, through: :memberships` stamps through; `polymorphic: true` / `as: :owner` stamps polymorphic=true + poly_as; an explicit `foreign_key:` stamps foreign_key. Proven by TestCrystalGraniteORM_ModelTableColumns + TestCrystalGraniteORM_AssociationOptions (through, polymorphic/as, class_name/foreign_key override). |
| Foreign key extraction | ✅ `full` | `2026-06-14` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | A `belongs_to :user` association yields a REFERENCES edge model -> target (fk_field + to_model props). The explicit `foreign_key:` field override and `class_name:`/typed `belongs_to x : T` target override now win for an exact edge (#5032), with an optional `primary_key:` stamp and polymorphic=true on a polymorphic belongs_to. Cross-file target resolution is delegated to the shared resolver via the bare CamelCased/overridden target name. Proven by TestCrystalGraniteORM_ModelTableColumns (convention) + TestCrystalGraniteORM_AssociationOptions (Comment->Account, fk_field=author_id override). |
| Lazy loading recognition | — `not_applicable` | — | — | — | Granite associations are loaded via explicit accessor calls/queries, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | ✅ `full` | `2026-06-14` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | belongs_to/has_many/has_one association macros are extracted as association entities (assoc_kind + target) and belongs_to additionally yields a REFERENCES FK edge — see association_extraction/foreign_key_extraction. through/polymorphic relations and explicit foreign_key:/class_name: overrides are now modelled (#5032). |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-12` | 4935 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Granite's class-method query DSL at a call site referencing a known model — `Model.all/find/find_by/where/first/last/count/exists?` (select), `Model.create/import` (insert), `Model.save/update` (update), `Model.clear/delete` (delete) — emits a QUERIES edge model->table stamped operation+table+model (graniteQueryRe + graniteQueryOp). Only receivers naming a model declared in the file are attributed (honest, file-local), so `Unknown.find` is never falsely counted. Proven by TestCrystalGraniteORM_QueryAttribution (select/insert/delete on User, Unknown skipped). Mirrors the Nim/Norm query-attribution shape. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-14` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Granite migrations are parsed (#5032): a `<Model>.migrator.create`/`.drop` call (graniteMigratorRe), attributed only to a model declared in the file, and a raw CREATE/DROP/ALTER TABLE schema-op SQL string passed to `.exec(...)` (graniteSchemaSQLRe) each emit a shared SCOPE.Evolution migration-op entity (create_table/drop_table/alter_table) carrying framework=granite, migration_op, table, provenance — mirroring the Nim Allographer + JS knex migration shape so the engine migration-schema-ops pass converges op -> table. Proven by TestCrystalGraniteORM_Migrations + TestCrystalGraniteORM_MigrationNoMatchNoop. |
| Migration schema ops | ✅ `full` | `2026-06-14` | 5032 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Each Granite schema op (`<Model>.migrator.create`/`.drop` + raw CREATE/DROP/ALTER TABLE `.exec` SQL) is emitted as a normalised SCOPE.Evolution op subtype (create_table/drop_table/alter_table) stamping the target table, so the engine migration_schema_ops pass derives a MODIFIES_TABLE convergence edge unifying migration -> table evolution with query -> table access. Proven by TestCrystalGraniteORM_Migrations. |

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
