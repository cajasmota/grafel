<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.django` — Django ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/orms/django_orm.yaml`<br>`internal/extractors/python/django_relational.go` | — |
| Model lifecycle extraction | ✅ `full` | `2026-06-02` | 3628 | `internal/custom/python/django.go`<br>`internal/custom/python/lifecycle_traits_test.go`<br>`internal/lifecycle/lifecycle.go`<br>`internal/lifecycle/lifecycle_test.go` | soft_delete + soft_delete_column (deleted_at DateTimeField -> deleted_at, is_deleted BooleanField -> is_deleted, or a django-safedelete base -> deleted_at), timestamps (auto_now_add + auto_now, or created_at + updated_at fields), audit_columns (created_by/updated_by FK). Honesty: a plain 'deleted' boolean with no deleted_at/is_deleted/safedelete is NOT soft_delete. Emits a SCOPE.Schema/model node per class X(models.Model). |
| Schema extraction | ✅ `full` | `2026-05-29` | 3060 | `internal/extractors/python/django_relational.go` | field_type and all keyword arguments (max_length, null, blank, on_delete, related_name, etc.) are stamped on each SCOPE.Schema/field entity by stampDjangoFieldProperties(); structured JSON Schema or OpenAPI emission not yet implemented |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/django_relational.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/django_relational.go` | — |
| Lazy loading recognition | ✅ `full` | `2026-05-29` | 3060 | `internal/engine/orm_queries_python.go` | select_related() and prefetch_related() detected as is_join=true by pythonIsJoinDjango(); full recognition of all lazy strategies not yet implemented |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/extractors/python/django_graph_relates_test.go`<br>`internal/extractors/python/django_relational.go` | Model↔model GRAPH_RELATES edges with cardinality alongside the field-level REFERENCES edge: ForeignKey→many_to_one, OneToOneField→one_to_one, ManyToManyField→many_to_many; hung off the owning model class node; scalar fields emit no edge. Test: TestDjangoGraphRelatesForeignKey/TestDjangoGraphRelatesScalarFieldNoEdge. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_python.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-02` | 3639 | `internal/engine/migration_sequence.go`<br>`internal/extractors/python/django_migration.go` | django_migration.go parses NNNN_name.py into op_count/operations/dependencies. #3639 additionally stamps sequence_number (the NNNN ordinal) + migration_name + migration_pattern=django on each migration entity via live Pass 8.9 (engine.ApplyMigrationSequence). |
| Migration schema ops | ✅ `full` | `2026-06-02` | — | `internal/engine/migration_schema_ops.go`<br>`internal/engine/migration_schema_ops_test.go`<br>`internal/extractors/python/django_migration.go` | Django CreateModel/AddField/RemoveField operations (from the Migration entity operations JSON) emit MODIFIES_TABLE edges keyed by model name (#3628). Asserted by TestDjangoCreateModelAndAddField. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/python/transaction_boundary.go`<br>`internal/extractors/python/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3628: Django @transaction.atomic decorator AND 'with transaction.atomic():' block stamp transactional=true + tx_source=django_atomic on the enclosing fn. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.django ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
