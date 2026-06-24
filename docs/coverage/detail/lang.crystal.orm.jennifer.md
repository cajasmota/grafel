<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.orm.jennifer` — Jennifer (Crystal ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/jennifer_orm_test.go` | Jennifer (imdrasil/jennifer.cr) is a Crystal ORM + query DSL. A persisted model is a `class T < Jennifer::Model::Base` (jenniferModelRe, pre-filtered by jenniferHasModel on the Jennifer::Model::Base marker). Each such class emits one SCOPE.Schema/model + one SCOPE.Schema/table; the table identity is the explicit `table_name "<name>"` macro (jenniferTableNameRe) when present, otherwise the model class name. Carries framework=jennifer + provenance. Proven by TestCrystalJenniferORM_ModelTableColumns (table_name users/accounts) + TestCrystalJenniferORM_NonModelNoop + TestCrystalJenniferORM_WrongLanguageNoop. |
| Model lifecycle extraction | — `not_applicable` | — | — | — | Jennifer lifecycle callbacks are deferred — only model/table/column/association extraction is implemented in this PR (#4936). No lifecycle callback recognition is claimed. |
| Schema extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/jennifer_orm_test.go` | Each `<name>: <Type>` entry inside the `mapping(…)` macro (jenniferMappingRe + jenniferMappingFieldRe) becomes a SCOPE.Schema/column carrying column_type (nilable `?` marker trimmed) + the owning model. A `Primary32`/`Primary64` type alias or an explicit `primary: true` option marks the column primary_key=true (jenniferFieldIsPrimary), and Primary32/Primary64 are normalised to Int32/Int64 (jenniferNormaliseType). The `with_timestamps` macro (jenniferWithTimestampsRe) synthesises the conventional created_at/updated_at Time columns stamped auto_timestamp=true. Proven by TestCrystalJenniferORM_ModelTableColumns (id Primary32→Int32 primary, email `String?` trimmed, with_timestamps columns). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/jennifer_orm_test.go` | belongs_to/has_many/has_one/has_and_belongs_to_many macros (jenniferAssocRe) each emit a SCOPE.Schema/association entity stamping assoc_kind, the owning model, and the resolved target model name (has_many/habtm singularised; explicit `, <Class>` target argument honoured by jenniferAssocTarget). Proven by TestCrystalJenniferORM_ModelTableColumns (has_many :posts, Post → target Post; belongs_to :account, Account). |
| Foreign key extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/jennifer_orm_test.go` | A `belongs_to :account, Account` association yields a REFERENCES edge model → target (fk_field + to_model props), the explicit `, <Class>` target winning over the CamelCased name. Cross-file target resolution is delegated to the shared resolver via the bare target name. Proven by TestCrystalJenniferORM_ModelTableColumns (REFERENCES User→Account, fk_field=account). |
| Lazy loading recognition | — `not_applicable` | — | — | — | Jennifer associations are loaded via explicit query/accessor calls, not a static lazy-loading proxy declaration — no lazy-load annotation to recognise. |
| Relationship extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/jennifer_orm_test.go` | belongs_to/has_many/has_one/has_and_belongs_to_many association macros are extracted as association entities (assoc_kind + target) and belongs_to additionally yields a REFERENCES FK edge — see association_extraction/foreign_key_extraction. Proven by TestCrystalJenniferORM_ModelTableColumns. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Jennifer Model query DSL: a Model.<verb> call site naming an in-file model emits a QUERIES edge model->table with the canonical SQL op (collectCrystalModelQueries/crystalQueryRels). |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Jennifer migration create_table/drop_table/alter_table DSL emits a shared SCOPE.Evolution migration-op (collectCrystalMigrations). |
| Migration schema ops | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | create/drop/alter_table + raw schema-op SQL via .exec map to SCOPE.Evolution create_table/drop_table/alter_table. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/jennifer_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | A <adapter>.transaction do block emits a SCOPE.Pattern/transaction_boundary (collectCrystalTransactions). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.orm.jennifer ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
