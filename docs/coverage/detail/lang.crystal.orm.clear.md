<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.orm.clear` — Clear (Crystal ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/clear_orm_test.go` | Clear (anykeyh/clear) is a Crystal ORM + query builder for PostgreSQL. A persisted model is a class that mixes in `Clear::Model` — clearClassRe finds the class and clearIncludeRe gates on the `include Clear::Model` marker within the class body (so a class that does not include it is skipped, honest), pre-filtered by clearHasModel. Each model emits one SCOPE.Schema/model + one SCOPE.Schema/table; the table identity is the explicit `self.table = "<name>"` assignment (clearTableRe) when present, otherwise the model class name. Carries framework=clear + provenance. Proven by TestCrystalClearORM_ModelTableColumns (self.table users/accounts) + TestCrystalClearORM_NonModelNoop (Config without include skipped) + TestCrystalClearORM_WrongLanguageNoop. |
| Model lifecycle extraction | — `not_applicable` | — | — | — | Clear lifecycle hooks are deferred — only model/table/column/association extraction is implemented in this PR (#4936). No lifecycle hook recognition is claimed. |
| Schema extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/clear_orm_test.go` | Each `column <name> : <Type>[, primary: true]` macro (clearColumnRe) becomes a SCOPE.Schema/column carrying column_type (nilable `?` marker trimmed) + the owning model, with primary_key=true stamped on the primary column. The `timestamps` macro (clearTimestampsRe) synthesises created_at/updated_at Time columns stamped auto_timestamp=true. Proven by TestCrystalClearORM_ModelTableColumns (id primary, email `String?` trimmed, timestamps columns). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/clear_orm_test.go` | belongs_to/has_many/has_one macros in Clear's typed form `belongs_to <name> : <Class>` (clearAssocRe) each emit a SCOPE.Schema/association entity stamping assoc_kind, the owning model, and the resolved target (has_many singularised; explicit `: <Class>` target honoured by clearAssocTarget). Proven by TestCrystalClearORM_ModelTableColumns (has_many posts : Post, belongs_to account : Account). |
| Foreign key extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/clear_orm_test.go` | A `belongs_to account : Account` association yields a REFERENCES edge model → target (fk_field + to_model props), the typed `: <Class>` target winning over the CamelCased name. Cross-file target resolution is delegated to the shared resolver. Proven by TestCrystalClearORM_ModelTableColumns (REFERENCES User→Account, fk_field=account). |
| Lazy loading recognition | — `not_applicable` | — | — | — | Clear associations are loaded via explicit query/accessor calls, not a static lazy-loading proxy declaration — no lazy-load annotation to recognise. |
| Relationship extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/clear_orm_test.go` | belongs_to/has_many/has_one association macros are extracted as association entities (assoc_kind + target) and belongs_to additionally yields a REFERENCES FK edge — see association_extraction/foreign_key_extraction. Proven by TestCrystalClearORM_ModelTableColumns. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Clear query-builder DSL: a Model.<verb> call site naming an in-file model emits a QUERIES edge model->table with the canonical SQL op (collectCrystalModelQueries/crystalQueryRels). |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Clear migration create_table/drop_table/alter_table DSL emits a shared SCOPE.Evolution migration-op (collectCrystalMigrations). |
| Migration schema ops | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | create/drop/alter_table + raw schema-op SQL via .exec map to SCOPE.Evolution create_table/drop_table/alter_table. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/clear_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | A Clear::SQL.transaction do block emits a SCOPE.Pattern/transaction_boundary (collectCrystalTransactions). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.orm.clear ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
