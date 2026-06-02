<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.driver.neo4j` — bolt_sips (Neo4j)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-02` | — | `internal/custom/elixir/neo4j.go`<br>`internal/custom/elixir/neo4j_test.go` | Node labels in Cypher patterns inside Bolt.Sips.query!/Boltx.query strings surfaced as SCOPE.Schema nodes. Soft schema recovered by regex over the query string, hence partial. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Raw driver has no association/relationship concept; Ecto handles associations independently. |
| Foreign key extraction | — `not_applicable` | — | — | — | Foreign key awareness lives in Ecto schema layer, not the raw driver. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Raw driver; no lazy loading concept. |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/elixir/neo4j.go`<br>`internal/custom/elixir/neo4j_test.go` | Cypher relationship patterns in Bolt.Sips.query!/Boltx.query strings promoted to traversable GRAPH_RELATES edges between node-label entities (reNeo4jExCypherTriple); statically-resolvable topology full, dynamic/interpolated relations honest-partial. Completes Neo4j GRAPH_RELATES set (epic #3606, #3618). Value-asserting test TestExNeo4jGraphRelatesEdge: Person -GRAPH_RELATES(ACTED_IN,OUTGOING)-> Movie; left-arrow flips source; single-node MATCH yields no edge. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | `internal/custom/elixir/neo4j.go`<br>`internal/custom/elixir/neo4j_test.go` | Bolt.Sips.query!(conn,'CYPHER') call sites captured as SCOPE.Operation/query with a coarse verb sniffed from the leading Cypher clause. Dynamically-built query strings not fully recoverable, so partial. |

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
(or use `go run ./tools/coverage update lang.elixir.driver.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
