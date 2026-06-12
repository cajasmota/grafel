<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.spring-data-mongo` — Spring Data MongoDB

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-11` | [link](https://github.com/cajasmota/archigraph/issues/4283) | `internal/extractors/java/nosql_model.go`<br>`internal/extractors/java/nosql_model_test.go` | Native Go pass (#4283): a class-level @Document(collection="users")/@Document("users") emits a SCOPE.Schema model entity (Subtype "schema") named after the class, collection name on the `collection` property and `store=mongodb`. Field membership reuses the base extractor's SCOPE.Schema/field children via CONTAINS (BuildSchemaFieldStructuralRef); @Field("e") override names + @Id recorded in model Properties (field.<n>.column / field.<n>.id). Complements the existing query_attribution pass (#4271) which already reads the same @Document annotation. Honest limit: dynamic/non-literal collection -> model emitted without a `collection` property. |
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
| Query attribution | ✅ `full` | `2026-06-05` | [link](https://github.com/cajasmota/archigraph/issues/4271) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_drivers_other.go`<br>`internal/engine/orm_queries_drivers_other_test.go`<br>`internal/engine/orm_queries_java_mongo_agg.go`<br>`internal/engine/orm_queries_java_mongo_agg_test.go` | Two complementary native Go passes. (1) Aggregation $lookup joins (scanJavaSpringMongoAggregation, #3845): each $lookup emits a JOINS_COLLECTION edge aggregating-collection -> from collection plus a SCOPE.DataAccess stage entity (fluent LookupOperation, positional Aggregation.lookup, @Aggregation(pipeline={...}) string pipelines). (2) Per-method query attribution (scanJavaSpringDataMongo, #4271): @Document("books")/@Document(collection="books") entity emits a QUERIES edge class -> Class:<collection>, and @Query("{...}") on a MongoRepository method emits method -> Class:<collection> where the collection is resolved from the file's @Document entity (the JSON @Query body is a filter, not a collection name). Gated on org.springframework.data.mongodb / MongoRepository / MongoTemplate. Honest limit: dynamic .from(var)/dynamic collection/non-literal @Query -> no edge. |

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
[`db.mongodb`](./db.mongodb.md) hub record (MongoDB (collections)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.spring-data-mongo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
