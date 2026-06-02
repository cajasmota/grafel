<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.driver.neo4j` — neo4rs

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-02` | — | `internal/custom/rust/neo4j.go`<br>`internal/custom/rust/neo4j_test.go` | Node labels in Cypher patterns inside neo4rs query()/Query::new() strings surfaced as SCOPE.Schema nodes. Soft schema recovered by regex over the query string, hence partial. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | raw/NoSQL driver — no ORM relationship/association/FK/lazy-load layer |
| Foreign key extraction | — `not_applicable` | — | — | — | raw/NoSQL driver — no ORM relationship/association/FK/lazy-load layer |
| Lazy loading recognition | — `not_applicable` | — | — | — | raw/NoSQL driver — no ORM relationship/association/FK/lazy-load layer |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/rust/neo4j.go`<br>`internal/custom/rust/neo4j_test.go` | Cypher relationship patterns in neo4rs query('...')/Query::new('...') strings (plain + raw literals) promoted to traversable GRAPH_RELATES edges between node-label entities (reNeo4jRustCypherTriple); statically-resolvable topology full, dynamic/untyped relations honest-partial. Completes Neo4j GRAPH_RELATES set (epic #3606, #3618). Value-asserting test TestRustNeo4jGraphRelatesEdge: Person -GRAPH_RELATES(ACTED_IN,OUTGOING)-> Movie; left-arrow flips source; single-node MATCH yields no edge. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | `internal/custom/rust/neo4j.go`<br>`internal/custom/rust/neo4j_test.go` | query('CYPHER') / Query::new('CYPHER') call sites captured as SCOPE.Operation/query with a coarse verb sniffed from the leading Cypher clause. Dynamically-built (format!) query strings not fully recoverable, so partial. |

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
(or use `go run ./tools/coverage update lang.rust.driver.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
