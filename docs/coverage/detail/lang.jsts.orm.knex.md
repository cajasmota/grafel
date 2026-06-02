<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.knex` — Knex (query builder)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | Knex is a SQL query builder, not an ORM — it has no model/entity layer to extract. Persistent model_extraction belongs to Objection.js, which layers Active-Record models on top of Knex (see lang.jsts.orm.objection). |
| Schema extraction | ✅ `full` | `2026-05-30` | 3187 | `internal/custom/javascript/knex_migrations.go`<br>`internal/custom/javascript/knex_migrations_test.go` | Knex migration schema-builder DSL: knex.schema.createTable() emits SCOPE.Schema/model table entities and t.string()/t.integer()/... column builders emit SCOPE.Component/column entities. Proven by TestKnexMigrationSchemaExtraction / TestKnexMigrationColumnExtraction. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-30` | 3187 | `internal/custom/javascript/knex_migrations.go`<br>`internal/custom/javascript/knex_migrations_test.go` | Each migration foreign key (.references().inTable() / .foreign().references().inTable()) yields a SCOPE.Pattern/relation association edge between the local column's table and the referenced table. Proven by TestKnexMigrationAssociationExtraction. |
| Foreign key extraction | ✅ `full` | `2026-05-30` | 3187 | `internal/custom/javascript/knex_migrations.go`<br>`internal/custom/javascript/knex_migrations_test.go` | Resolves both .references('id').inTable('users') and explicit .foreign('col').references().inTable() chains, plus the qualified single-arg .references('table.column') spelling, into SCOPE.Component/foreign_key entities with ref_table/ref_column/local_column. Proven by TestKnexMigrationForeignKeyInline / TestKnexMigrationForeignKeyExplicit / TestKnexMigrationQualifiedReference. |
| Lazy loading recognition | — `not_applicable` | — | 3071 | — | Knex is a SQL query builder with no ORM model layer; there is no relation or lazy-loading concept to extract. Lazy loading is not applicable. |
| Relationship extraction | ✅ `full` | `2026-05-30` | 3187 | `internal/custom/javascript/knex_migrations.go`<br>`internal/custom/javascript/knex_migrations_test.go` | Migration foreign keys are the only place Knex declares table relationships; each FK emits a SCOPE.Pattern/relation entity (relation_kind=belongs_to) capturing local column → referenced table. Proven by TestKnexMigrationRelationshipExtraction. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/orm_queries_jsts_drivers.go`<br>`internal/engine/orm_queries_jsts_drivers_test.go`<br>`internal/extractors/cross/dbmap/orms.go`<br>`internal/extractors/cross/dbmap/query_builders.go`<br>`internal/extractors/cross/dbmap/query_builders_test.go` | Driver query attribution via orm_queries_jsts_drivers. #3628 area #3 ADDS table-level access: knex('t')/.from('t')/.into('t') resolve the string-literal table → ACCESSES_TABLE (op from .insert/.update/.del/.truncate); dynamic knex(tableVar) skipped. Proven by TestKnex* in query_builders_test.go. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/knex.go` | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: knex.transaction(...) stamps transactional=true + tx_source=knex_transaction on the enclosing fn. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.knex ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
