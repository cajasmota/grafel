<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.orm.avram` — Avram (Lucky Crystal ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/avram_orm_test.go` | Avram (luckyframework/avram) is the ORM that ships with the Lucky web framework. A persisted model is a `class T < BaseModel` (avramModelRe), file-level pre-filtered by avramHasModel on the Avram::Model marker so an arbitrary BaseModel subclass never misfires. Each model emits one SCOPE.Schema/model + one SCOPE.Schema/table; the table identity is the explicit `table :<name> do` / `table "<name>" do` block argument (avramTableRe) when present, otherwise the model class name (anonymous `table do`). Carries framework=avram + provenance. Proven by TestCrystalAvramORM_ModelTableColumns (explicit table :accounts + anonymous table do → User) + TestCrystalAvramORM_NonModelNoop + TestCrystalAvramORM_WrongLanguageNoop. |
| Model lifecycle extraction | — `not_applicable` | — | — | — | Avram lifecycle hooks (SaveOperation callbacks) are deferred — only model/table/column/association extraction is implemented in this PR (#4936). No lifecycle hook recognition is claimed. |
| Schema extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/avram_orm_test.go` | Inside the `table do … end` block, each `column <name> : <Type>` (avramColumnRe) and each `primary_key <name> : <Type>` (avramPrimaryKeyRe, stamped primary_key=true) becomes a SCOPE.Schema/column carrying column_type (nilable `?` marker trimmed) + the owning model. The `timestamps` macro (avramTimestampsRe) synthesises created_at/updated_at Time columns stamped auto_timestamp=true. Proven by TestCrystalAvramORM_ModelTableColumns (primary_key id, email `String?` trimmed, timestamps columns). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/avram_orm_test.go` | belongs_to/has_many/has_one macros in Avram's typed form `belongs_to <name> : <Class>` (avramAssocRe) each emit a SCOPE.Schema/association entity stamping assoc_kind, the owning model, and the resolved target (has_many singularised; explicit `: <Class>` target honoured by avramAssocTarget). Proven by TestCrystalAvramORM_ModelTableColumns (has_many posts : Post, belongs_to account : Account). |
| Foreign key extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/avram_orm_test.go` | A `belongs_to account : Account` association yields a REFERENCES edge model → target (fk_field + to_model props), the typed `: <Class>` target winning over the CamelCased name. Cross-file target resolution is delegated to the shared resolver. Proven by TestCrystalAvramORM_ModelTableColumns (REFERENCES User→Account, fk_field=account). |
| Lazy loading recognition | — `not_applicable` | — | — | — | Avram associations are loaded via explicit query preloads, not a static lazy-loading proxy declaration — no lazy-load annotation to recognise. |
| Relationship extraction | ✅ `full` | `2026-06-14` | 4936 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/avram_orm_test.go` | belongs_to/has_many/has_one association macros are extracted as association entities (assoc_kind + target) and belongs_to additionally yields a REFERENCES FK edge — see association_extraction/foreign_key_extraction. Proven by TestCrystalAvramORM_ModelTableColumns. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Avram Query/SaveOperation DSL: a Model.<verb> call site naming an in-file model emits a QUERIES edge model->table with the canonical SQL op (collectCrystalModelQueries/crystalQueryRels). Mirrors Granite. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | Avram migration create_table/drop_table/alter_table :x DSL (collectCrystalMigrations) emits a shared SCOPE.Evolution migration-op. |
| Migration schema ops | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | create/drop/alter_table ops + raw schema-op SQL via .exec map to SCOPE.Evolution create_table/drop_table/alter_table (collectCrystalMigrations). |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-24` | 5366 | `internal/custom/crystal/avram_orm.go`<br>`internal/custom/crystal/orm_query_migration.go`<br>`internal/custom/crystal/orm_query_migration_test.go` | A <db>.transaction do block emits a SCOPE.Pattern/transaction_boundary (collectCrystalTransactions). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.orm.avram ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
