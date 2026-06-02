<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.driver.neo4j` — neo4j-ruby-driver / activegraph OGM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-02` | 3614 | `internal/custom/ruby/neo4j_activegraph.go`<br>`internal/custom/ruby/neo4j_activegraph_test.go` | activegraph / neo4j.rb OGM (not just the raw driver): each class that includes ActiveGraph::Node (or legacy Neo4j::ActiveNode) is extracted as a SCOPE.Schema/node (the graph node label) and each `property :name` declaration as a SCOPE.Schema/property. Regex over class bodies; partial (no inheritance/mixin node resolution). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3614 | `internal/custom/ruby/neo4j_activegraph.go` | activegraph has_many / has_one :out|:in associations are extracted as SCOPE.Component/relationship entities carrying relation_type, direction, and target_node (model_class). |
| Foreign key extraction | — `not_applicable` | — | — | — | graph DB — no foreign-key concept |
| Lazy loading recognition | — `not_applicable` | — | — | — | graph DB — no lazy-loading concept |
| Relationship extraction | ✅ `full` | `2026-06-02` | 3614 | `internal/custom/ruby/neo4j_activegraph.go`<br>`internal/custom/ruby/neo4j_activegraph_test.go` | activegraph has_many/has_one(:out|:in, type:, model_class:) associations are extracted AND emitted as traversable GRAPH_RELATES graph-schema edges owner-node -> target-node (mirrors the Python neomodel template #3670 / Java SDN #3663 / JOINS_COLLECTION for graph DBs); the domain graph topology is a navigable subgraph rather than opaque string props. Full for same-file ActiveGraph::Node targets (value-asserting test TestActiveGraphGraphRelatesEdge: Person -GRAPH_RELATES(ACTED_IN,OUTGOING)-> Movie; has_one :in -> INCOMING). Cross-file / dynamic model_class targets are honest-partial (kept as target_node props only). Reverses the #3635 datastore-pass downgrade for this OGM. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.driver.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
