<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.neo4j` — Neo4j

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | 3828 | `internal/engine/orm_queries_datastore_infra.go`<br>`internal/engine/orm_queries_jsts_drivers.go` | No resource/dependency extraction yet for this datastore; tracked in #3828 (sibling datastores done — genuine build-gap). |
| Resource extraction | 🟢 `partial` | `2026-06-02` | 3828 | `internal/engine/orm_queries_datastore_infra.go`<br>`internal/engine/orm_queries_jsts_drivers.go` | Neo4j session.run("MATCH (n:Label) ...") Cypher (gated on neo4j / bolt:// / GraphDatabase / neomodel / neogma) parses the first node label into a Class:<Label> resource node + QUERIES dependency edge from the connecting function. JS via scanJSNeo4j (orm_queries_jsts_drivers.go); C#/PHP/Rust/Python/Java/Ruby/Go via emitCypherTargets (orm_queries_datastore_infra.go). Parameterised labels / runtime Cypher honest-skipped. |

## Code-level coverage

This infra record tracks datastore-level extraction (resources, dependency
attribution). The deep, code-level coverage for this technology lives in the
per-language driver/ORM records below — each one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.csharp.driver.neo4j`](./lang.csharp.driver.neo4j.md) | C# | driver | 4 missing, 7 n/a |
| [`lang.elixir.driver.neo4j`](./lang.elixir.driver.neo4j.md) | elixir | driver | 1 full, 2 partial, 3 missing, 5 n/a |
| [`lang.go.driver.neo4j`](./lang.go.driver.neo4j.md) | go | driver | 1 full, 2 partial, 3 missing, 5 n/a |
| [`lang.java.orm.neo4j`](./lang.java.orm.neo4j.md) | java | orm | 1 full, 3 partial, 4 missing, 3 n/a |
| [`lang.jsts.driver.neo4j`](./lang.jsts.driver.neo4j.md) | JS/TS | driver | 2 full, 2 partial, 3 missing, 4 n/a |
| [`lang.php.driver.neo4j`](./lang.php.driver.neo4j.md) | php | driver | 1 full, 2 partial, 3 missing, 5 n/a |
| [`lang.python.driver.neo4j`](./lang.python.driver.neo4j.md) | python | driver | 1 full, 2 partial, 4 missing, 4 n/a |
| [`lang.ruby.driver.neo4j`](./lang.ruby.driver.neo4j.md) | ruby | driver | 1 full, 2 partial, 4 missing, 4 n/a |
| [`lang.rust.driver.neo4j`](./lang.rust.driver.neo4j.md) | rust | driver | 1 full, 2 partial, 3 missing, 5 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
