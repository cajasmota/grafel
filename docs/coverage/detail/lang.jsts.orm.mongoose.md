<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.mongoose` — Mongoose

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 13

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/orms/mongoose.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_test.go`<br>`internal/custom/javascript/mongoose.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | — | 3844 | `internal/custom/javascript/mongoose.go`<br>`internal/engine/orm_queries_jsts_mongoose_populate.go`<br>`internal/engine/orm_queries_jsts_mongoose_populate_test.go` | scanJSMongoosePopulateJoins (#3844) now parses the definition-side ref: field in both new Schema({...}) literals and @nestjs/mongoose @Prop({ref:'X'}) decorators, couples it to a static .populate('field') traversal, and emits a JOINS_COLLECTION edge (Class:Model -> Class:ref) matching the $lookup contract; reMongoosePopulate also still captures the populate call as a query entity |
| Foreign key extraction | — `not_applicable` | — | 3064 | — | MongoDB is a document-oriented database; there is no relational FK concept |
| Lazy loading recognition | — `not_applicable` | — | 3064 | — | Mongoose/MongoDB has no lazy-loading mechanism |
| Relationship extraction | ✅ `full` | — | 3844 | `internal/custom/javascript/mongoose.go`<br>`internal/engine/orm_queries_jsts_mongoose_populate.go`<br>`internal/engine/orm_queries_jsts_mongoose_populate_test.go` | schema-level ref: declarations (the definition side of Mongoose associations) are now parsed from new Schema({...}) literals and @nestjs/mongoose @Prop({ref:'X'}) decorators and, when traversed by a static .populate('field'), emitted as JOINS_COLLECTION reference-join edges (#3844); dynamic ref / dynamic populate stay unresolved (honest-partial boundary) |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Framework-specific

### Aggregation Joins

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Aggregation join extraction | ✅ `full` | — | 3844 | `internal/engine/orm_queries_jsts_mongo_agg.go`<br>`internal/engine/orm_queries_jsts_mongo_agg_test.go` | scanJSMongoAggregation parses Model.aggregate([...]) pipelines (inline array, same-scope variable binding, and fluent .build() builder forms), emitting one SCOPE.DataAccess stage node per stage and a JOINS_COLLECTION edge (Class:Model -> Class:from) for every $lookup / $graphLookup with a static from; matches the Python pymongo/motor contract and feeds shared_db_coupling.go (Pass 8.8). Dynamic from is honestly skipped |
| Populate reference join extraction | ✅ `full` | — | 3844 | `internal/engine/orm_queries_jsts_mongoose_populate.go`<br>`internal/engine/orm_queries_jsts_mongoose_populate_test.go` | scanJSMongoosePopulateJoins emits a JOINS_COLLECTION edge for Mongoose ref:/@Prop(ref) fields that are traversed by a static .populate('field') — the dominant NestJS-target join idiom — bringing ref/populate to parity with the $lookup join contract |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.mongoose ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
