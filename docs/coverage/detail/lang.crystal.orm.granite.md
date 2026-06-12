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
| Model lifecycle extraction | 🔴 `missing` | — | 4935 | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-12` | 4935 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | Each `column <name> : <Type>[, primary: true]` macro (graniteColumnRe) becomes a SCOPE.Schema/column carrying column_type (nilable `?` marker trimmed) and the owning model, with primary_key=true stamped on the primary column. Proven by TestCrystalGraniteORM_ModelTableColumns (id primary, body `String?` trimmed). Partial (honest): column converters/defaults, the `timestamps` helper, and index declarations are not yet read — follow-up #4935. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-12` | 4905 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | belongs_to/has_many/has_one macros (graniteAssocRe) each emit a SCOPE.Schema/association entity stamping assoc_kind, the owning model, and the CamelCased target model name. Proven by TestCrystalGraniteORM_ModelTableColumns (has_many :posts, belongs_to :user). Honest follow-up #4935: `has_many through:` and polymorphic associations are not yet resolved. |
| Foreign key extraction | 🟢 `partial` | `2026-06-12` | 4935 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | A `belongs_to :user` association yields a REFERENCES edge model->User (fk_field + to_model props, target name CamelCased from the symbol). Proven by TestCrystalGraniteORM_ModelTableColumns. Partial (honest): the explicit `foreign_key:` option override and cross-file target resolution are deferred — the edge targets the bare CamelCased name for the shared resolver to bind. Follow-up #4935. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Granite associations are loaded via explicit accessor calls/queries, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | ✅ `full` | `2026-06-12` | 4905 | `internal/custom/crystal/granite_orm.go`<br>`internal/custom/crystal/granite_orm_test.go` | belongs_to/has_many/has_one association macros are extracted as association entities (assoc_kind + target) and belongs_to additionally yields a REFERENCES FK edge — see association_extraction/foreign_key_extraction. Full follow-up modelling of through/polymorphic relations is #4935. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | — | 4935 | — | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | 4935 | — | — |
| Migration schema ops | 🔴 `missing` | — | 4935 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 4935 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.orm.granite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
