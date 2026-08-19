<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.driver.mongodb` — mongodb (PHP driver)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [PHP](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | — `not_applicable` | — | — | — | Schema-less or server-side schema only; raw PHP driver has no PHP-level schema model to extract. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Foreign key extraction | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Relationship extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/grafel/issues/3849) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_php_mongo_agg.go`<br>`internal/engine/orm_queries_php_mongo_agg_test.go` | Doctrine MongoDB ODM mapping-reference annotations/attributes now extracted (scanPHPMongoAggregation, #3849): @ReferenceMany/@ReferenceOne/@EmbedMany/@EmbedOne (or #[ODM\ReferenceMany(targetDocument: Author::class)]) on a property of a @Document/#[Document] class emit a JOINS_COLLECTION edge owning-Document -> targetDocument (Class:Book -> Class:Author, via=reference), mirroring the Mongoose-ref / Mongoid-association convention. Honest-partial: dynamic targetDocument -> no edge; embedded-doc shape topology + cardinality not modelled. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/grafel/issues/3645) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_drivers_other.go`<br>`internal/engine/orm_queries_drivers_other_test.go`<br>`internal/engine/orm_queries_php_mongo_agg.go`<br>`internal/engine/orm_queries_php_mongo_agg_test.go` | Driver topology: $mongo->selectCollection('db','coll') / ->collection('x') captured via scanPHPDrivers; QUERIES edge to Class:<Collection>; dynamic names honest-skipped. Aggregation $lookup joins now extracted too (scanPHPMongoAggregation, #3849): each $lookup emits a JOINS_COLLECTION edge aggregating-collection -> from collection (Class:Book -> Class:Author) plus a SCOPE.DataAccess stage entity, matching the Python/Java/Go/Mongoose contract and completing the cross-language Mongo epic #3837. Two idioms: Doctrine ODM fluent createAggregationBuilder(Book::class)->lookup('authors')->localField(..)->foreignField(..)->alias(..); and Laravel-MongoDB (jenssegers) raw Book::raw(fn($c)=>$c->aggregate([['$lookup'=>['from'=>'authors',..]]])) PHP-array pipelines. Honest-partial: dynamic from/dynamic collection -> no edge. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
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
(or use `go run ./tools/coverage update lang.php.driver.mongodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
