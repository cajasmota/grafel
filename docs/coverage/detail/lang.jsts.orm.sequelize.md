<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.sequelize` — Sequelize

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/orms/sequelize.yaml` | — |
| Model lifecycle extraction | ✅ `full` | `2026-06-02` | 3628 | `internal/custom/javascript/lifecycle_traits_test.go`<br>`internal/custom/javascript/sequelize.go`<br>`internal/lifecycle/lifecycle.go`<br>`internal/lifecycle/lifecycle_test.go` | soft_delete + soft_delete_column (paranoid:true -> deletedAt, or deletedAt:'col' override), timestamps (default-on for Sequelize models unless timestamps:false; paranoid forces them on), audit_columns (createdBy/created_by/... attribute keys). Honesty: no paranoid -> no soft_delete; timestamps:false omits timestamps. Reads attributes + options objects of define()/init(). |
| Schema extraction | ✅ `full` | `2026-05-29` | 3067 | `internal/custom/javascript/orm_build_3067_test.go`<br>`internal/custom/javascript/sequelize.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | 3067 | `internal/custom/javascript/orm_build_3067_test.go`<br>`internal/custom/javascript/sequelize.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-29` | 3067 | `internal/custom/javascript/orm_build_3067_test.go`<br>`internal/custom/javascript/sequelize.go` | — |
| Lazy loading recognition | 🟢 `partial` | — | 3071 | `internal/custom/javascript/issue3071_lazy_loading_test.go`<br>`internal/custom/javascript/sequelize.go` | Detects hasMany/belongsTo/hasOne/belongsToMany association calls with { lazy: true } in options; emits SCOPE.Pattern/lazy_association. Sequelize does not have a built-in lazy-loading mechanism; this detects explicit lazy: true flags in association definitions. |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/javascript/orm_relationship_edges_test.go`<br>`internal/custom/javascript/sequelize.go` | Model↔model GRAPH_RELATES edges with cardinality from User.hasMany/belongsTo/hasOne/belongsToMany(Target); Class:<src>→Class:<target>. Test: TestSequelizeGraphRelatesEdges. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/sequelize.go` | — |
| Migration schema ops | ✅ `full` | `2026-06-02` | — | `internal/custom/javascript/sequelize.go`<br>`internal/engine/migration_schema_ops.go`<br>`internal/engine/migration_schema_ops_test.go` | Sequelize queryInterface.createTable/addColumn migration ops converge via MODIFIES_TABLE (#3628). Same engine pass as knex/typeorm. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: sequelize.transaction(async t => ...) stamps transactional=true + tx_source=sequelize_transaction on the enclosing fn. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.sequelize ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
