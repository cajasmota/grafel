<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.orm.mongoid` — Mongoid

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/ruby/orms/mongoid.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/routes.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/engine/orm_queries_ruby_mongoid_agg.go`<br>`internal/engine/orm_queries_ruby_mongoid_agg_test.go` | Mongoid association macros (belongs_to, has_many, has_one, embeds_many, embeds_one, embedded_in, has_and_belongs_to_many) declared inside an `include Mongoid::Document` class now emit a native JOINS_COLLECTION relation edge Class:<owning model> -> Class:<associated model> (scanRubyMongoidAssociations, #3847), matching the Mongoose ref/populate contract. Target model is the explicit `class_name:` option else the camelised-singular of the association symbol (has_many :line_items -> Class:LineItem). Honest-partial: dynamic class_name -> falls back to symbol; a macro outside a Mongoid::Document class is gated out. |
| Foreign key extraction | — `not_applicable` | — | — | — | Mongoid uses document references, not relational foreign keys |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/ruby/activerecord.go` | Mongoid associations are lazy by default; includes/eager_load markers detected. Part of #3282. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/engine/orm_queries_ruby_mongoid_agg.go`<br>`internal/engine/orm_queries_ruby_mongoid_agg_test.go` | Mongoid relationship macros (belongs_to/has_many/embeds_many/embeds_one/embedded_in/has_one/has_and_belongs_to_many) emit JOINS_COLLECTION relation edges via scanRubyMongoidAssociations (#3847, epic #3837), the Ruby sibling of the Mongoose ref/populate relation pass. Part of #3282. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_ruby_mongoid_agg.go`<br>`internal/engine/orm_queries_ruby_mongoid_agg_test.go` | Mongoid `Model.collection.aggregate([...])` pipeline $lookup/$graphLookup joins now extracted (scanRubyMongoidAggregation, #3847, epic #3837): each $lookup emits a JOINS_COLLECTION edge aggregating-collection -> from collection (Class:Book -> Class:Author) plus a SCOPE.DataAccess stage entity, matching the Python/Mongoose/Go/Java contract. Ruby hash-rocket (`'from' => 'authors'`) and symbol-colon separators both parsed; aggregating collection resolved from the model class name or `store_in collection: 'books'`. Honest-partial: dynamic from / variable-bound pipeline -> no edge; general query-verb attribution still missing (#3645). |

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
(or use `go run ./tools/coverage update lang.ruby.orm.mongoid ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
