<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.driver.neo4j` — neo4j-php-client

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-02` | — | `internal/custom/php/neo4j.go`<br>`internal/custom/php/neo4j_test.go` | Node labels in Cypher patterns ((n:Person),(:Movie)) inside ->run/Statement::create strings surfaced as SCOPE.Schema nodes. Soft schema recovered by regex over the query string, hence partial. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Foreign key extraction | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Raw PHP driver — no ORM relation layer; association/relationship/lazy-loading do not apply to raw connection clients. |
| Relationship extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/php/neo4j.go`<br>`internal/custom/php/neo4j_test.go` | Cypher relationship patterns in laudis ->run('...') strings promoted to traversable GRAPH_RELATES edges between node-label entities (reNeo4jPHPCypherTriple); statically-resolvable topology full, dynamic/untyped/interpolated relations honest-partial. Completes Neo4j GRAPH_RELATES set (epic #3606, #3618) alongside go/java/py/jsts/csharp/ruby. Value-asserting test TestPHPNeo4jGraphRelatesEdge: Person -GRAPH_RELATES(ACTED_IN,OUTGOING)-> Movie; left-arrow flips source; single-node MATCH yields no edge. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | `internal/custom/php/neo4j.go`<br>`internal/custom/php/neo4j_test.go` | ->run('CYPHER') / Statement::create('CYPHER') call sites captured as SCOPE.Operation/query with a coarse verb sniffed from the leading Cypher clause. Dynamically-built query strings not fully recoverable, so partial. |

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
(or use `go run ./tools/coverage update lang.php.driver.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
