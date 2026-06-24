<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.orm.ormin` — ormin (Nim compile-time ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-06-12` | 5028 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/ormin_orm.go` | ormin is a COMPILE-TIME ORM: the schema is NOT Nim object types — a Nim model module binds an external SQL DSL file via `importModel(DbBackend.<be>, "model")`. The Nim-side binding is recorded as a SCOPE.Schema/model_import entity (framework=ormin) stamping backend + model_file (nimOrminImportRe). The model->table mapping itself is delivered by schema_extraction from the SQL DSL file. Proven by TestNimOrminORM_ImportModel. Partial (honest): there is no Nim `ref object` model layer to map (the SQL DSL IS the model), so model_import is the binding signal, not a per-type model entity — model entities are not_applicable on the Nim side. |
| Model lifecycle extraction | — `not_applicable` | — | — | — | ormin generates typed query bindings at compile time; there is no per-instance active-record lifecycle (save/delete on a model object) to extract. |
| Schema extraction | ✅ `full` | `2026-06-12` | 5028 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/ormin_orm.go` | The ormin model DSL is a `*.sql` file of `create table T(...)` DDL parsed at compile time. This extractor parses that DDL into one SCOPE.Schema/table per `create table T(` (orminCreateTableRe + paren-matched body) and one SCOPE.Schema/column per column definition (top-level-comma split, table-level constraint keywords filtered), framework=ormin, column_type = the SQL type, with primary_key=true / not_null=true stamped when present. Proven by TestNimOrminORM_SQLSchema (User/Post tables, name string not_null, id primary_key asserted) + TestNimOrminORM_NonSQLNoop. Honest remainder: the ormin query DSL (`query: select ... from ...`) attribution is follow-up #5031. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | ormin has no declarative association DSL — relations are expressed only as inline `references Other(col)` foreign-key column constraints in the SQL DSL (see foreign_key_extraction). |
| Foreign key extraction | 🟢 `partial` | — | 5028 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/ormin_orm.go` | An inline column-level `<col> ... references Other(col)` foreign key in the SQL DSL yields a REFERENCES edge table->referenced-table (fk_field + to_table + references props) and stamps foreign_key=true / fk_target / fk_column on the column. orminRefRe. Proven by TestNimOrminORM_SQLSchema (Post.author -> User.id asserted). Partial (honest): table-level `foreign key (...) references ...(...)` constraint syntax and composite keys are follow-up #5031; cross-file targets resolve via the shared resolver. |
| Lazy loading recognition | — `not_applicable` | — | — | — | ormin loads related rows via explicit generated query bindings, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | 🟢 `partial` | — | 5028 | `internal/custom/nim/ormin_orm.go` | Foreign-key relationships surface as REFERENCES edges (see foreign_key_extraction). ormin has no separate declarative association DSL, so association_extraction/lazy_loading are not_applicable; bidirectional relationship modelling beyond the FK edge is follow-up #5031. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-24` | 5367 | `internal/custom/nim/extractors_test.go`<br>`internal/custom/nim/ormin_orm.go` | ormin's compile-time query: DSL (SQL-shaped: select … from T / insert into T / update T / delete from T) is scanned by extractNimQueries: each query: block body yields QUERIES edges (operation+table) on a SCOPE.Schema/query entity, with the verb keyword mapped to the operation. Honest partial: nested sub-selects and dynamic table identifiers are not attributed (no fabricated query); the model->table mapping itself lives in the SQL-DSL create-table parse. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | 5031 | — | — |
| Migration schema ops | 🔴 `missing` | — | 5031 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 5031 | — | ormin transaction blocks are not yet stamped — transaction-boundary extraction is follow-up #5031. This record covers model->table/column mapping only. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.orm.ormin ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
