<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.orm.anorm` — Anorm

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-06-12` | 4915 | `internal/custom/scala/anorm.go`<br>`internal/custom/scala/anorm_test.go` | #4915 (NEW extractor custom_scala_anorm — Anorm was the only mainstream Scala SQL library with no record). Anorm is the SQL data-access layer bundled with Play: SQL-as-string + RowParser-to-case-class, not a relationship-declaring ORM. anormCaseClassRe captures case class row models -> SCOPE.Schema(provenance INFERRED_FROM_ANORM_CASE_CLASS); anormMacroParserRe captures Macro.namedParser[T]/indexedParser[T] -> SCOPE.Schema row_parser:T naming the hydrated row type (TestAnormMacroRowParser). Partial: column-level field mapping (get[T]("col")/SqlParser.str) not parsed. |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | ✅ `full` | — | 4915 | `internal/custom/scala/anorm.go`<br>`internal/custom/scala/anorm_test.go` | #4915: anormSQLCallRe (SQL("…")/SQL("""…""")) and anormSQLInterpRe (interpolated SQL"…") capture every Anorm statement -> SCOPE.Operation/query, mining the leading SQL verb and primary table via the shared sqlVerb/firstSQLTable helpers (table_name=users/orders, sql_verb=select/insert in TestAnormSQLCallStatement/TestAnormSQLInterpolated). Row models come from Macro parser + case-class capture. Gate keys on the `anorm` token so doobie's lower-case sql"…" interpolator is not poached (TestAnormNoMatch). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | `internal/custom/scala/anorm.go` | Anorm is plain SQL + RowParser, not an ORM; associations are expressed via raw SQL JOINs with no declarative DSL to extract |
| Foreign key extraction | — `not_applicable` | — | — | `internal/custom/scala/anorm.go` | Anorm declares no foreign keys; FK constraints live in the externally managed database schema (Play Evolutions/Flyway) |
| Lazy loading recognition | — `not_applicable` | — | — | `internal/custom/scala/anorm.go` | Anorm has no lazy-loading; every query is an explicit SQL(...).as(parser) execution against an implicit Connection |
| Relationship extraction | — `not_applicable` | — | — | `internal/custom/scala/anorm.go` | No relationship declarations in Anorm; relationships are expressed via raw SQL with no extractable DSL |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | — | 4915 | `internal/custom/scala/anorm.go`<br>`internal/custom/scala/anorm_test.go` | #4915: each Anorm SQL statement is emitted as SCOPE.Operation/query stamped sql_verb + table_name (firstSQLTable/sqlVerb), attributing the statement to its target table and operation. Partial: multi-table JOIN attribution captures only the first table; bound-parameter (.on(...)) flow not yet modelled. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | `internal/custom/scala/anorm.go` | Anorm does not manage migrations; Play apps use Play Evolutions or Flyway alongside Anorm |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.orm.anorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
