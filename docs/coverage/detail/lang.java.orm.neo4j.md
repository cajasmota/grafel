<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.neo4j` — Neo4j (Java driver)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/java/orms/neo4j.yaml` | — |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/neo4j.go` | No Neo4j Java ORM extractor; @Node annotation for node entity extraction not implemented. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/neo4j.go` | No Neo4j Java ORM extractor (Spring Data Neo4j @Node/@Relationship annotations not handled). Tracked in issue #3001. |
| Foreign key extraction | — `not_applicable` | — | 3098 | `internal/custom/java/neo4j.go` | Neo4j is a graph database with no foreign key concept; foreign_key_extraction is not applicable |
| Lazy loading recognition | — `not_applicable` | — | 3098 | `internal/custom/java/neo4j.go` | Neo4j Spring Data has no lazy-loading concept equivalent to relational ORMs; not applicable |
| Relationship extraction | ✅ `full` | — | 3611 | `internal/custom/java/neo4j.go`<br>`internal/custom/java/neo4j_test.go` | @Relationship(type=,direction=) fields are extracted AND emitted as traversable GRAPH_RELATES graph-schema edges owner-@Node -> target-@Node (mirrors JOINS_COLLECTION for graph DBs); the app's domain graph topology is now a navigable subgraph rather than opaque string props. Full for same-file @Node targets (value-asserting test TestNeo4jGraphRelatesEdge: Person -GRAPH_RELATES(ACTED_IN)-> Movie); cross-file target resolution is honest-partial (deferred to the resolver / future cross-file pass, kept as target_node props only). |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | 3098 | `internal/custom/java/neo4j.go` | Neo4j graph database has no SQL migration files; migration_parsing is not applicable |
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
(or use `go run ./tools/coverage update lang.java.orm.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
