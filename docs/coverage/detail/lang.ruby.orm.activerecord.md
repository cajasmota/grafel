<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.orm.activerecord` — ActiveRecord

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/ruby/orms/activerecord.yaml` | — |
| Schema extraction | ✅ `full` | — | — | `internal/custom/ruby/activerecord.go`<br>`internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_deep_test.go` | db/schema.rb create_table parsed into table+typed columns (name/type/null/default/limit), t.references→FK column+key, t.timestamps; table linked to model by Rails inflection (users→User). Migrations also emit columns. Test: TestDeepSchema_ExactColumnsAndModelLink/IrregularModelLink assert exact columns+types+options+model link. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | — | — | `internal/custom/ruby/activerecord.go`<br>`internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_deep_test.go` | has_many/belongs_to/has_one/HABTM/has_many:through with options (:through/:source/:class_name/:foreign_key/polymorphic/:as); target-model inferred via inflection+class_name. Test: TestDeepAssoc_AllMacrosWithOptions asserts type+target+options per macro. |
| Foreign key extraction | ✅ `full` | — | — | `internal/custom/ruby/activerecord.go`<br>`internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_deep_test.go` | FK from belongs_to convention (x_id), explicit foreign_key:, add_foreign_key (with to_table:), t.references/t.belongs_to/add_reference (incl polymorphic x_id+x_type). target_model inferred. Tests: TestDeepAssoc_AllMacrosWithOptions, TestDeepMigration_CreateTableColumnsAndOps, TestDeepSchema_ExactColumnsAndModelLink. |
| Lazy loading recognition | ✅ `full` | — | — | `internal/custom/ruby/activerecord.go`<br>`internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_deep_test.go` | Declaration-level (parity with TypeORM bar): recognizes eager loading calls (includes/preload/eager_load) AND AR lazy-by-default associations, each emitted with loading_strategy. Test: TestDeepLazyLoading_EagerAndLazyBothRecognized. Note: query-site dataflow tracing of which records are actually eager-loaded is out of scope (static declaration-level). |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_graph_relates_test.go` | Model↔model GRAPH_RELATES edges with cardinality (one_to_many/many_to_one/one_to_one/many_to_many) emitted from associations, hung off the owner model node; ToID Class:<Target> honors class_name: + inflection; polymorphic stays honest-partial (no edge). Test: TestARGraphRelatesCardinality/TestARBelongsToManyToOne/TestARClassNameTargetRedirect/TestARPolymorphicNoEdge. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/orm_queries.go`<br>`internal/extractors/cross/dbmap/orms.go`<br>`internal/extractors/cross/dbmap/query_builders_test.go` | ActiveRecord Model.find/where attribution via orm_queries. #3628 area #3 ADDS association table-access: .joins(:assoc)/.includes/.preload/.eager_load pull the association table into a second ACCESSES_TABLE edge (symbol args only; string/var skipped). Proven by TestActiveRecordJoinsAssociation. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | — | 3639 | `internal/custom/ruby/activerecord.go`<br>`internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_deep_test.go`<br>`internal/engine/migration_sequence.go` | db/migrate/*.rb parsed into normalized SCOPE.Evolution ops (create_table/add_column/drop_column/alter_column/create_index/add_reference/add_foreign_key/drop_table), plus typed columns inside create_table blocks and FK entities. Test: TestDeepMigration_CreateTableColumnsAndOps asserts exact op subtypes+columns+FKs. #3639 additionally stamps sequence_number (the YYYYMMDDHHMMSS timestamp) + migration_name + migration_pattern=rails on each db/migrate entity via live Pass 8.9 (engine.ApplyMigrationSequence). |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/ruby/ruby.go`<br>`internal/extractors/ruby/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: ActiveRecord Model.transaction do / { } block stamps transactional=true + tx_source=rails_transaction on the enclosing Ruby method. No transitive propagation; attribute reads (obj.transaction) not matched. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.orm.activerecord ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
