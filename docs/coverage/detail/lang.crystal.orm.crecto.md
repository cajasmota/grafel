<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.orm.crecto` — Crecto (Crystal ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/crecto_orm_test.go` | Crecto (Crecto/crecto) is a Crystal ORM modelled on Elixir's Ecto. A persisted model is a class carrying a `schema "<table>" do … end` block — crectoClassRe finds the class and crectoSchemaRe gates on the schema block (a class with no schema block is skipped, honest), file-level pre-filtered by crectoHasModel on the Crecto::Schema marker. Each model emits one SCOPE.Schema/model + one SCOPE.Schema/table keyed by the `schema "<name>"` argument. Carries framework=crecto + provenance. Proven by TestCrystalCrectoORM_ModelTableColumns (schema "users") + TestCrystalCrectoORM_NonModelNoop (Config without schema block skipped) + TestCrystalCrectoORM_WrongLanguageNoop. |
| Model lifecycle extraction | — `not_applicable` | — | — | — | Crecto lifecycle hooks are deferred — only model/table/column/association extraction is implemented in this PR (#4936). No lifecycle hook recognition is claimed. |
| Schema extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/crecto_orm_test.go` | Each `field :<name>, <Type>` macro (crectoFieldRe) becomes a SCOPE.Schema/column carrying column_type + the owning model. Crecto injects an implicit `id` primary key by default; this is synthesised (column_type=PkeyValue, primary_key=true, provenance INFERRED_FROM_CRECTO_IMPLICIT_PK) unless the model declares an explicit `primary_key`/`set_primary_key` override (crectoPrimaryKeyOptRe). Proven by TestCrystalCrectoORM_ModelTableColumns (field :name/:email/:age with age Int32, synthesised id primary key). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/crecto_orm_test.go` | belongs_to/has_many/has_one macros (crectoAssocRe) each emit a SCOPE.Schema/association entity stamping assoc_kind, the owning model, and the resolved target (has_many singularised; explicit `, <Class>` target honoured by crectoAssocTarget). Proven by TestCrystalCrectoORM_ModelTableColumns (has_many :posts, Post; belongs_to :account, Account). |
| Foreign key extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/crecto_orm_test.go` | A `belongs_to :account, Account` association yields a REFERENCES edge model → target (fk_field + to_model props), the explicit `, <Class>` target winning over the CamelCased name. Cross-file target resolution is delegated to the shared resolver. Proven by TestCrystalCrectoORM_ModelTableColumns (REFERENCES User→Account, fk_field=account). |
| Lazy loading recognition | — `not_applicable` | — | — | — | Crecto associations are loaded via explicit Repo preloads, not a static lazy-loading proxy declaration — no lazy-load annotation to recognise. |
| Relationship extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/crecto_orm_test.go` | belongs_to/has_many/has_one association macros are extracted as association entities (assoc_kind + target) and belongs_to additionally yields a REFERENCES FK edge — see association_extraction/foreign_key_extraction. Proven by TestCrystalCrectoORM_ModelTableColumns. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Crecto routes reads through the Repo with the model as leading arg: Repo.<verb>(Model,...) naming an in-file model emits a QUERIES edge model->table with the canonical SQL op (collectCrectoRepoQueries). Instance-arg insert/update/delete pass a lowercase changeset -> honest skip. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Crecto migration create_table/drop_table/alter_table DSL emits a shared SCOPE.Evolution migration-op (collectCrystalMigrations). |
| Migration schema ops | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | create/drop/alter_table + raw schema-op SQL via .exec map to SCOPE.Evolution create_table/drop_table/alter_table. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/crecto_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | A <repo>.transaction do block emits a SCOPE.Pattern/transaction_boundary (collectCrystalTransactions). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.orm.crecto ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
