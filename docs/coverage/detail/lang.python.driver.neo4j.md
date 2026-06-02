<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.driver.neo4j` — neo4j (Python driver) / neomodel OGM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-02` | 3609 | `internal/custom/python/neo4j_neomodel.go`<br>`internal/custom/python/neo4j_neomodel_test.go` | neomodel OGM (not just the raw driver): each StructuredNode subclass is extracted as a SCOPE.Schema/node (the graph node label) and each *Property() attribute as a SCOPE.Schema/property. Regex over class bodies; partial (no inheritance/mixin StructuredNode resolution). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3609 | `internal/custom/python/neo4j_neomodel.go` | neomodel RelationshipTo/RelationshipFrom attributes are extracted as SCOPE.Component/relationship entities carrying relation_type, direction, and target_node. |
| Foreign key extraction | — `not_applicable` | — | — | — | graph DB — no foreign-key concept |
| Lazy loading recognition | — `not_applicable` | — | — | — | graph DB — no lazy-loading concept |
| Relationship extraction | ✅ `full` | `2026-06-02` | 3609 | `internal/custom/python/neo4j_neomodel.go`<br>`internal/custom/python/neo4j_neomodel_test.go` | neomodel RelationshipTo/RelationshipFrom('Target','REL_TYPE') fields are extracted AND emitted as traversable GRAPH_RELATES graph-schema edges owner-node -> target-node (mirrors the Java SDN template #3663 / JOINS_COLLECTION for graph DBs); the domain graph topology is a navigable subgraph rather than opaque string props. Full for same-file StructuredNode targets (value-asserting test TestNeomodelGraphRelatesEdge: Person -GRAPH_RELATES(ACTED_IN,OUTGOING)-> Movie; RelationshipFrom -> INCOMING). Cross-file target labels are honest-partial (kept as target_node props only). Reverses the #3635 datastore-pass downgrade for this OGM. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

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
[`db.neo4j`](./db.neo4j.md) infra record (Neo4j),
which tracks datastore-level extraction for the same technology.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.driver.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
