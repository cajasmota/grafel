<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.neo4j` — Neo4j

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_datastore_infra.go`<br>`internal/engine/orm_queries_jsts_drivers.go` | — |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_datastore_infra.go`<br>`internal/engine/orm_queries_jsts_drivers.go` | Neo4j session.run("MATCH (n:Label) ...") Cypher (gated on neo4j / bolt:// / GraphDatabase / neomodel / neogma) parses the first node label into a Class:<Label> resource node + QUERIES dependency edge from the connecting function. JS via scanJSNeo4j (orm_queries_jsts_drivers.go); C#/PHP/Rust/Python/Java/Ruby/Go via emitCypherTargets (orm_queries_datastore_infra.go). Parameterised labels / runtime Cypher honest-skipped. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
