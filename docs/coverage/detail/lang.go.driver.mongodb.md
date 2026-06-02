<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.driver.mongodb` — mongo-go-driver

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/mongo_driver.go`<br>`internal/custom/golang/mongo_redis_test.go` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3214 | `internal/custom/golang/mongo_driver.go`<br>`internal/custom/golang/mongo_redis_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | — |
| Foreign key extraction | — `not_applicable` | — | — | — | — |
| Lazy loading recognition | — `not_applicable` | — | — | — | — |
| Relationship extraction | — `not_applicable` | — | — | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-02` | 3214 | `internal/custom/golang/mongo_driver.go`<br>`internal/custom/golang/mongo_redis_test.go`<br>`internal/engine/orm_queries_go_mongo_agg.go`<br>`internal/engine/orm_queries_go_mongo_agg_test.go`<br>`internal/engine/rules/go/orms/mongo_driver.yaml` | Base driver: collection method call sites (Find/Aggregate/Insert/Update/Delete/...) captured with the CRUD verb but not bound to a concrete collection (regex-only, hence partial). Aggregation pipelines (scanGoMongoAggregation, #3846): each $lookup/$graphLookup stage emits a JOINS_COLLECTION edge aggregating-collection -> `from` collection plus a SCOPE.DataAccess stage entity, matching the Python/JS/Mongoose contract. Handles both bson.D tuple form (bson.D{{"$lookup", bson.D{{"from","authors"},...}}}) and bson.M map form (bson.M{"$lookup": bson.M{"from": "authors"}}) in mongo.Pipeline{...} / []bson.D{...} / []bson.M{...} literals. The aggregating collection resolves from an inline db.Collection("books") receiver or a same-function `coll := db.Collection("books")` binding, so the JOINS_COLLECTION FromID lands on the named collection node (Class:Book -> Class:Author). Pipeline resolved from an inline slice literal OR a same-function `pipeline := mongo.Pipeline{...}` binding. Value-asserting tests TestGoMongoAgg_BsonD_InlineCollection_LookupEdge + TestGoMongoAgg_BsonM_CollVarBinding_LookupEdge assert the joined-collection node ids. Honest-partial: dynamic `from`, dynamic/unresolvable collection, and builder-produced pipelines stay unresolved (no fabricated edge). |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Datastore

This driver/ORM record provides code-level coverage for the
[`db.mongodb`](./db.mongodb.md) infra record (MongoDB (collections)),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.driver.mongodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
