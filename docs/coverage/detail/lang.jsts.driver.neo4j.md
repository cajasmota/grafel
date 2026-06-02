<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.driver.neo4j` — neo4j-driver (JS) / neogma OGM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | `2026-06-02` | 3610 | `internal/custom/javascript/neogma.go`<br>`internal/custom/javascript/neogma_test.go` | neogma OGM (not just the raw driver): each ModelFactory({ label, schema, relationships }) call is extracted as a SCOPE.Schema/node keyed on its Neo4j `label`. Regex over the balanced config object; partial (schema field types not individually emitted). |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3610 | `internal/custom/javascript/neogma.go` | neogma relationships.<key> entries are extracted as SCOPE.Component/relationship entities carrying relation_type, direction, and target_model/target_node. |
| Foreign key extraction | — `not_applicable` | — | — | — | graph DB — no foreign-key concept |
| Lazy loading recognition | — `not_applicable` | — | — | — | graph DB — no lazy-loading concept |
| Relationship extraction | ✅ `full` | `2026-06-02` | 3610 | `internal/custom/javascript/neogma.go`<br>`internal/custom/javascript/neogma_test.go` | neogma ModelFactory relationships ({ model: Target, name: 'REL_TYPE', direction: 'out'|'in' }) are extracted AND emitted as traversable GRAPH_RELATES graph-schema edges owner-node -> target-node (mirrors the Java SDN template #3663 / JOINS_COLLECTION for graph DBs); the domain graph topology is a navigable subgraph rather than opaque string props. Full for same-file ModelFactory `model:` bindings (value-asserting test TestNeogmaGraphRelatesEdge: Person -GRAPH_RELATES(ACTED_IN,OUTGOING)-> Movie; direction 'in' -> INCOMING). Cross-file model references are honest-partial (kept as target_model props only). Reverses the #3635 datastore-pass downgrade for this OGM. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts_drivers.go`<br>`internal/engine/orm_queries_jsts_drivers_test.go` | Includes Cypher node-label attribution: session.run('MATCH (n:Label) ...') resolves the graph node label as the queried resource and maps MATCH/CREATE/MERGE/SET/DELETE clauses to canonical operations. |

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
(or use `go run ./tools/coverage update lang.jsts.driver.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
