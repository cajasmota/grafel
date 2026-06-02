<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.cassandra` — Apache Cassandra (schema)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_drivers_other.go` | — |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_drivers_other.go` | Cassandra/Scylla CQL session.execute("... FROM table") across C#/PHP/Rust/Python/Java/Ruby/JS parses the FROM/INTO/UPDATE table into a Class:<Table> resource node + QUERIES dependency edge from the connecting function (emitCQLTargets). Runtime-built CQL honest-skipped. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.cassandra ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
