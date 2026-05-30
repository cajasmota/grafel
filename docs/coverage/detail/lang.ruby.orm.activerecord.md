<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.orm.activerecord` — ActiveRecord

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

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
| Relationship extraction | ✅ `full` | — | — | `internal/custom/ruby/activerecord.go`<br>`internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_deep_test.go` | Each association emits a SCOPE.Pattern/association entity carrying association_type, target_model, owner_model and option props (through/source/class_name/foreign_key/polymorphic/as). Test: TestDeepAssoc_AllMacrosWithOptions. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | — | — | `internal/custom/ruby/activerecord.go`<br>`internal/custom/ruby/activerecord_deep.go`<br>`internal/custom/ruby/activerecord_deep_test.go` | db/migrate/*.rb parsed into normalized SCOPE.Evolution ops (create_table/add_column/drop_column/alter_column/create_index/add_reference/add_foreign_key/drop_table), plus typed columns inside create_table blocks and FK entities. Test: TestDeepMigration_CreateTableColumnsAndOps asserts exact op subtypes+columns+FKs. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.orm.activerecord ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
