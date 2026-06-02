<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.sqlalchemy` — SQLAlchemy

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_field_edges.go`<br>`internal/engine/rules/python/orms/sqlalchemy.yaml` | — |
| Model lifecycle extraction | ✅ `full` | `2026-06-02` | 3628 | `internal/custom/python/lifecycle_traits_test.go`<br>`internal/custom/python/sqlalchemy.go`<br>`internal/lifecycle/lifecycle.go`<br>`internal/lifecycle/lifecycle_test.go` | soft_delete + soft_delete_column (deleted_at Column/mapped_column, or a SoftDeleteMixin base -> deleted_at), timestamps (created_at + updated_at columns WITH a server_default/onupdate/default=func|datetime signal), audit_columns (created_by/updated_by). Honesty: a plain 'deleted' boolean Column is NOT soft_delete; a pair of plain DateTime columns without a timestamp-default signal does NOT assert timestamps. |
| Schema extraction | ✅ `full` | `2026-05-29` | 3060 | `internal/custom/python/sqlalchemy.go`<br>`internal/engine/orm_field_edges.go` | __tablename__, Mapped[] columns, relationship attributes, and ForeignKey targets are extracted as SCOPE.Schema entities; structured JSON Schema or OpenAPI emission not yet implemented |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/sqlalchemy.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/python/sqlalchemy.go` | — |
| Lazy loading recognition | ✅ `full` | `2026-05-29` | 3060 | `internal/custom/python/sqlalchemy.go` | lazy= kwarg in relationship() calls is detected and recorded as lazy_strategy on the SCOPE.Schema entity; lazy_select_in, write_only, and dynamic_write_only strategies not yet distinguished |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/python/sqlalchemy.go`<br>`internal/custom/python/sqlalchemy_graph_relates_test.go` | Model↔model GRAPH_RELATES edges with cardinality from relationship("Target"): default collection→one_to_many, uselist=False→one_to_one; Class:<parent>→Class:<target>. Test: TestSQLAlchemyGraphRelatesEdges/TestSQLAlchemyNoRelationshipNoEdge. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/orm_queries_python.go`<br>`internal/extractors/cross/dbmap/orms.go`<br>`internal/extractors/cross/dbmap/query_builders.go`<br>`internal/extractors/cross/dbmap/query_builders_test.go` | ORM session.query attribution via orm_queries_python. #3628 area #3 ADDS the SQLAlchemy Core builder surface: select(table_obj) and table_obj.insert()/update()/delete() resolve the Table('name',...) object to its literal → ACCESSES_TABLE edges. Proven by TestSQLAlchemyCore* in query_builders_test.go. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-29` | 3060 | `internal/engine/rules/python/orms/alembic.yaml` | — |
| Migration schema ops | ✅ `full` | `2026-06-02` | — | `internal/custom/python/alembic_schema.go`<br>`internal/engine/migration_schema_ops.go`<br>`internal/engine/migration_schema_ops_test.go` | Alembic op.create_table/add_column/create_index SCOPE.Schema entities converge onto a synthetic SCOPE.Table via MODIFIES_TABLE edges (engine pass, #3628). Asserted by TestAlembicCreateTableAndAddColumn. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/python/transaction_boundary.go`<br>`internal/extractors/python/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: SQLAlchemy session.begin()/begin_nested()/engine.begin() stamps transactional=true + tx_source=sqlalchemy_begin on the enclosing Python fn. No transitive propagation. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.sqlalchemy ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
