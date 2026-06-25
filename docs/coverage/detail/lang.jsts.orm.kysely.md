<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.kysely` — Kysely (type-safe query builder)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | Kysely is a type-safe SQL query builder, not an ORM — its `Database` interface is a compile-time TypeScript type with no runtime model/entity layer to extract. The schema is defined imperatively in migrations, not as decorated model classes. |
| Model lifecycle extraction | — `not_applicable` | — | — | — | No model/entity layer; Kysely has no lifecycle hooks (no save/beforeCreate equivalents) — queries are explicit chains, not active-record instances. |
| Schema extraction | ✅ `full` | `2026-06-25` | 5599 | `internal/custom/javascript/kysely_migrations.go`<br>`internal/custom/javascript/kysely_migrations_test.go` | #5599 parses Kysely's imperative migration schema-builder DSL. db.schema.createTable('t')/.alterTable('t') yield a SCOPE.Schema/model table entity; .addColumn('c','type',...) yields a SCOPE.Component/column entity. The custom_js_kysely_migrations extractor gates on a `.schema.` builder chain plus a Kysely import / migrations-dir path so unrelated createTable/addColumn calls are not misread. Proven by TestKyselyMigrationSchemaExtraction / TestKyselyMigrationColumnExtraction. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Kysely is a SQL query builder with no ORM model layer; associations are expressed ad-hoc per query via .innerJoin()/.leftJoin(), not declared on a model — there is no static association to extract. |
| Foreign key extraction | ✅ `full` | `2026-06-25` | 5599 | `internal/custom/javascript/kysely_migrations.go`<br>`internal/custom/javascript/kysely_migrations_test.go` | #5599 resolves both Kysely FK spellings to a SCOPE.Component/foreign_key entity carrying local_column / ref_table / ref_column: the explicit .addForeignKeyConstraint('fk', ['localCol'], 'refTable', ['refCol']) form, and the column-level .references('refTable.refCol') inside an .addColumn callback (local column = nearest preceding .addColumn). Dynamic / non-literal lists are skipped (honest-partial). Proven by TestKyselyMigrationForeignKeyConstraint / TestKyselyMigrationColumnLevelForeignKey. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Kysely is a SQL query builder with no ORM model layer; there is no relation or lazy-loading concept to recognise — every join is explicit in the query chain. |
| Relationship extraction | ✅ `full` | `2026-06-25` | 5599 | `internal/custom/javascript/kysely_migrations.go`<br>`internal/custom/javascript/kysely_migrations_test.go` | #5599 derives a SCOPE.Pattern/relation (relation_kind=belongs_to) from each migration foreign-key constraint — the migration FK IS the table relationship. The relation carries local_column + ref_table, e.g. relation:owner_id->account. This is the only place Kysely declares a static relationship (joins are ad-hoc per query). Proven by TestKyselyMigrationRelationshipExtraction. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-24` | 5491 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effect_sinks_kysely_5491_test.go` | #5491 Kysely query-builder data-access effects: the chain ROOT method on a db/kysely/trx receiver determines read vs write — selectFrom("t") -> db_read; insertInto/updateTable/deleteFrom/replaceInto("t") -> db_write — terminating in .execute()/.executeTakeFirst()/.executeTakeFirstOrThrow()/.stream(). The string-literal table arg is captured in a model/table-bearing sink tag (kysely.read:user / kysely.write:post) attributed to the enclosing function, mirroring the #5490 Prisma model-bearing uplift. Raw sql`…`.execute(db) is classified by the leading SQL keyword (SELECT/WITH -> read; INSERT/UPDATE/DELETE/REPLACE -> write; undeterminable -> generic db_read, sink kysely.raw); (db|kysely|trx).executeQuery(...) -> generic db_read. The distinctive chain-root + db/kysely/trx receiver gate (trx = transaction-callback handle) stops an unrelated .execute() from being misread. effect_sinks_jsts.go. Proven by TestKyselyReadEffects_5491 / TestKyselyWriteEffects_5491 / TestKyselyReplaceInto_5491 / TestKyselyRawSQL_5491 / TestKyselyTrxAndKyselyReceiver_5491 / TestKyselyNonKyselyExecuteNotCredited_5491. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-06-25` | 5599 | `internal/custom/javascript/kysely.go`<br>`internal/custom/javascript/kysely_migrations_test.go` | #5599 recognises Kysely migration modules: the up()/down() named exports become SCOPE.Operation/migration entry-point entities (direction up|down, framework=kysely), and the per-op schema mutations become SCOPE.Evolution entities. The custom_js_kysely base extractor gates on a `.schema.` builder chain so non-migration code is ignored. Proven by TestKyselyMigrationOps. |
| Migration schema ops | ✅ `full` | `2026-06-25` | 5599 | `internal/custom/javascript/kysely.go`<br>`internal/engine/migration_schema_ops.go`<br>`internal/engine/migration_schema_ops_test.go` | #5599 emits SCOPE.Evolution migration ops (framework=kysely, table set) for db.schema.createTable/alterTable/dropTable; these converge to MODIFIES_TABLE via the #3628 engine pass (kysely added to evolutionOp alongside knex/typeorm/sequelize/objection/mikroorm) onto the shared SCOPE.Table node, so migrations and queries meet on one logical table. Proven by TestKyselyMigrationSchemaOps (engine) + TestKyselyMigrationOps (extractor). |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 5599 | — | db.transaction().execute(async (trx) => ...) interactive-transaction boundary stamping (transactional=true / tx_source) is not yet emitted; the trx handle IS receiver-gated for query/effect attribution (#5491) and the schema/migration layer landed in #5599 — only the transaction-boundary function stamp remains as a small follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.kysely ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
