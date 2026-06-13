<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.orm.allographer` — Allographer (Nim query/schema builder)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | Allographer is a query builder + schema builder, not an active-record ORM: there is no `ref object` model type to map to a table (unlike Norm). The schema builder IS the schema, so schema/table extraction is recorded under schema_extraction; there is no separate model entity to extract. |
| Model lifecycle extraction | — `not_applicable` | — | — | — | No model object layer — Allographer has no per-instance lifecycle (save/delete on a model). Persistence is via the rdb() query builder against tables, not model objects. |
| Schema extraction | ✅ `full` | `2026-06-12` | 4933 | `internal/custom/nim/allographer_orm.go`<br>`internal/custom/nim/extractors_test.go` | An Allographer `schema().create(table("name", [Column()...]))` declaration synthesises one SCOPE.Schema/table per `table("...")` block (table identity = the string literal) and one SCOPE.Schema/column per `Column().<method>("col")` builder call, carrying framework=allographer + provenance. column_type is the builder method name (string/integer/increments/foreign/…). Column-chain modifiers are stamped: `.unique()` -> unique=true, `.nullable()` -> nullable=true. Pre-filtered by nimAllographerHasSchema so arbitrary Nim is ignored. collectAllographerTables + collectAllographerColumns. Proven by TestNimAllographerORM_TablesColumnsFK + TestNimAllographerORM_NonSchemaNoop + TestNimAllographerORM_WrongLanguageNoop. alter()/drop() schema migrations are now modelled under the Migrations capability group (#5029). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Allographer has no declarative association DSL — relations are expressed only as `.foreign(...).reference(...).on(...)` foreign-key column chains in the schema builder (see foreign_key_extraction). |
| Foreign key extraction | ✅ `full` | — | 4933 | `internal/custom/nim/allographer_orm.go`<br>`internal/custom/nim/extractors_test.go` | A `Column().foreign("col").reference("refCol").on("refTable")` column chain yields a REFERENCES edge table->referenced-table (fk_field + to_table + references props, keyed by the `.on("...")` target) and stamps foreign_key=true / fk_target / fk_column on the column. nimAlloOnRe + nimAlloReferenceRe read the chain bounded to the owning Column() call. Proven by TestNimAllographerORM_TablesColumnsFK (posts.user_id -> users.id asserted). Honest remainder: cross-file FK targets carry the bare table name on the REFERENCES edge and resolve via the shared resolver. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Allographer loads related rows via explicit rdb() query-builder joins, not a lazy-loading proxy layer — no lazy-load annotation to recognise. |
| Relationship extraction | 🟢 `partial` | — | 4933 | `internal/custom/nim/allographer_orm.go` | Foreign-key relationships surface as REFERENCES edges (see foreign_key_extraction). Allographer has no separate declarative association DSL, so association_extraction/lazy_loading are not_applicable; bidirectional relationship modelling beyond the FK edge is follow-up #5029. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-13` | 5030 | `internal/custom/nim/allographer_query.go`<br>`internal/custom/nim/extractors_test.go` | The Allographer rdb() query builder (`rdb().table("t")...<op>()`) is attributed to its table by the query extractor (custom_nim_allographer_query, pre-filtered by nimAllographerHasQuery so schema-only files and arbitrary Nim are ignored). Each `rdb()` head bounds one query chain; the `.table("...")` anchor gives the table identity (the same key the schema-builder table uses, so query->table converges by name) and the terminal builder method classifies the op: `.get()/.first()/.find(/.pluck(/.count(/.max(/.min(/.avg(/.sum(` -> select; `.insert(/.insertId(/.insertID(` -> insert; `.update(` -> update; `.delete(` -> delete. One SCOPE.Schema/table per distinct table carries a QUERIES edge table->table (bare table name, resolved by the shared resolver) per distinct operation (operation + table props), reusing the SCOPE.Schema Kind + QUERIES edge the Norm/Debby query-attribution extractors use (no new kind). Proven by TestNimAllographerQuery_AttributesOps (users select+insert, posts update+delete asserted) + TestNimAllographerQuery_NonQueryNoop + TestNimAllographerQuery_WrongLanguageNoop. Honest remainder (follow-up #5116): join targets, raw SQL, and dynamic (non-literal) table names are not attributed. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-13` | 5111 | `internal/custom/nim/allographer_migrations.go`<br>`internal/custom/nim/extractors_test.go` | Allographer expresses schema EVOLUTION imperatively via `schema().alter(...)` and `schema().drop(...)`. The migrations extractor (custom_nim_allographer_migrations, pre-filtered by nimAllographerHasMigration so create-only schemas and arbitrary Nim are ignored) parses each op into a SCOPE.Evolution migration-op entity (framework=allographer, provenance=INFERRED_FROM_ALLOGRAPHER_MIGRATION) carrying the normalised op subtype + migration_op (raw builder method) + table (+ column when column-scoped). Recognised ops: `table("t").add(Column()...)` -> add_column; `.change(...)` -> alter_column; `.renameColumn(...)` -> rename_column; `.deleteColumn(...)` -> drop_column; `renameTable(old,new)` -> rename_table; `schema().drop("t")` and `schema().drop(table("t"))` -> drop_table. balancedParen bounds the alter() block; per-table op chains are anchored by table("name"). #5111: an FK chain added inside alter() (`.foreign("col").reference("refCol").on("refTable")`) yields a REFERENCES edge op-entity to the referenced table (fk_field/to_table/references rel props, same shape as the create-time path) plus foreign_key=true/fk_target/fk_column op props, resolved by the shared resolver; a `.dropForeign("col")` chain yields a drop_foreign op; add()/change() ops re-extract the new column TYPE re-declared in the `Column().<type>("col")` chain into new_column_type (the builder method name); and a const/let/var string-literal-bound table name in `schema().drop(IDENT)` / `table(IDENT)` is resolved to its literal (truly dynamic unbound identifiers are still skipped, no fabricated op). Proven by TestNimAllographerMigrations_AlterDropOps (all op kinds + both drop forms asserted) + TestNimAllographerMigrations_FKAndColumnTypeAndDynamicTable (alter-time FK REFERENCES edge + new_column_type + drop_foreign + const-bound dynamic-table resolution + unbound-identifier skip) + TestNimAllographerMigrations_NonMigrationNoop + TestNimAllographerMigrations_WrongLanguageNoop. |
| Migration schema ops | ✅ `full` | `2026-06-13` | 5029 | `internal/custom/nim/allographer_migrations.go`<br>`internal/engine/migration_schema_ops.go`<br>`internal/engine/migration_schema_ops_test.go` | Each Allographer SCOPE.Evolution migration-op entity is wired to the table it mutates by the shared engine migration-schema-ops pass (Pass, ApplyMigrationSchemaOps): evolutionOp's `allographer` case reads the normalised op subtype + table (+ column) and the pass emits a MODIFIES_TABLE edge op-entity -> a SYNTHESIZED_TABLE_CONVERGENCE SCOPE.Table node (same normTable key ACCESSES_TABLE uses), converging migration->table evolution with query->table access on one logical table. Proven by TestAllographerAlterDropMigration (add_column users.bio + drop_table posts MODIFIES_TABLE edges asserted). Reuses the existing MODIFIES_TABLE edge + SCOPE.Evolution kind (no new kind). |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🟢 `partial` | — | 5030 | `internal/custom/nim/allographer_query.go`<br>`internal/custom/nim/extractors_test.go` | Allographer transactions are run via the rdb() query-builder transaction API (`rdb().transaction(proc() = ...)`). The query extractor bounds each `rdb().transaction(...)` block with a balanced-paren scan (txnBlockEnd) and stamps transaction=true on the QUERIES edge of every rdb() query whose chain head falls inside that block, so the transaction boundary is recorded on the queries it encloses. Proven by TestNimAllographerQuery_AttributesOps (accounts.update + ledger.insert inside an rdb().transaction(...) asserted transaction=true). Partial (honest): the boundary is stamped on the enclosed queries rather than synthesised as a standalone SCOPE.Operation transaction entity — that, plus nested/named transactions, is follow-up #5116. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.orm.allographer ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
