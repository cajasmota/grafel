<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.typeorm` — TypeORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/orms/typeorm.yaml` | — |
| Model lifecycle extraction | ✅ `full` | `2026-06-02` | 3628 | `internal/custom/javascript/lifecycle_traits_test.go`<br>`internal/custom/javascript/typeorm.go`<br>`internal/lifecycle/lifecycle.go`<br>`internal/lifecycle/lifecycle_test.go` | soft_delete + soft_delete_column (@DeleteDateColumn() -> its property name), timestamps (@CreateDateColumn() + @UpdateDateColumn() both present), audit_columns (@Column() createdBy/created_by/...). Honesty: a plain @Column() 'deleted' boolean with no @DeleteDateColumn is NOT soft_delete; @CreateDateColumn alone does not assert timestamps. Per-@Entity body isolation. |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/javascript/typeorm.go` | reTypeORMEntity+reTypeORMColumn+reTypeORMViewEntity extract entity/column/view decorators (#3183) |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_test.go`<br>`internal/custom/javascript/typeorm.go` | — |
| Foreign key extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/typeorm.go` | — |
| Lazy loading recognition | 🟢 `partial` | — | 3071 | `internal/custom/javascript/issue3071_lazy_loading_test.go`<br>`internal/custom/javascript/typeorm.go` | Detects @OneToMany/@ManyToOne/@OneToOne/@ManyToMany relation decorators carrying { lazy: true }; emits SCOPE.Pattern/lazy_relation with lazy_loading=true. Promise<T> return-type inference not yet implemented. |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/javascript/orm_relationship_edges_test.go`<br>`internal/custom/javascript/typeorm.go` | Model↔model GRAPH_RELATES edges with cardinality from @OneToMany/@ManyToOne/@OneToOne/@ManyToMany; target taken from the arrow-returned class (() => Order), hung off the owning @Entity node; cross-file targets honest-partial (target_entity prop only, no edge). Test: TestTypeORMGraphRelatesEdges/TestTypeORMUnresolvedTargetNoEdge. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/typeorm.go` | — |
| Migration schema ops | ✅ `full` | `2026-06-02` | — | `internal/custom/javascript/typeorm.go`<br>`internal/engine/migration_schema_ops.go`<br>`internal/engine/migration_schema_ops_test.go` | TypeORM queryRunner.createTable/addColumn migration ops converge via MODIFIES_TABLE (#3628). Asserted by TestTypeORMAddColumn. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: TypeORM dataSource/manager.transaction(...) and @Transaction() decorator stamp transactional=true + tx_source=typeorm_transaction on the enclosing fn. No transitive propagation. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.typeorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
