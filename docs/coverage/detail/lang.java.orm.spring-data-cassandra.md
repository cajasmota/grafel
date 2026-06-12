<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.spring-data-cassandra` — Spring Data Cassandra

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-11` | [link](https://github.com/cajasmota/archigraph/issues/4283) | `internal/extractors/java/nosql_model.go`<br>`internal/extractors/java/nosql_model_test.go` | Native Go pass (#4283): a class-level @Table("users") (Cassandra; disambiguated from JPA @Entity+@Table) emits a SCOPE.Schema model entity (Subtype "schema") named after the class, table name on the `table` property and `store=cassandra`. Field membership reuses the base extractor's SCOPE.Schema/field children via CONTAINS (BuildSchemaFieldStructuralRef); @Column("x") override names + @PrimaryKey recorded in model Properties (field.<n>.column / field.<n>.id). Honest limit: dynamic/non-literal table name -> model emitted without a `table` property. |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_ecosystem.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Foreign key extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Lazy loading recognition | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Relationship extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-05` | [link](https://github.com/cajasmota/archigraph/issues/4271) | `internal/engine/orm_queries_drivers_other.go`<br>`internal/engine/orm_queries_drivers_other_test.go` | Native Go query-topology pass (scanJavaSpringDataCassandra, #4271): @Query("SELECT/INSERT/UPDATE/DELETE ... FROM t") on a CassandraRepository method emits a QUERIES edge method -> Class:<table> via the shared CQL extractor (extractSQLTable/sqlOp), and @Table("t")/@Table(value="t") entity emits class -> Class:<table>. The native DataStax cqlSession.execute("CQL") form is covered separately by scanJavaDrivers/emitCQLTargets. Gated on org.springframework.data.cassandra / CassandraRepository / CassandraTemplate. Honest limit: dynamic/runtime-built CQL (no string literal) -> no edge (extractSQLTable returns empty). |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | `2026-05-29` | — | — | ORM model-definition layer; database migration files are owned by Flyway/Liquibase, not the ORM itself. Same rationale as lang.java.orm.jooq and lang.java.orm.neo4j N/A. |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Related extraction records

This record provides code-level coverage for the
[`db.cassandra`](./db.cassandra.md) hub record (Apache Cassandra (schema)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.spring-data-cassandra ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
