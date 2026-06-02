<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.snowflake` — Snowflake

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_datastore_infra.go` | — |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_datastore_infra.go` | Snowflake cursor.execute/query("... FROM table") (snowflake-connector-python, SQLAlchemy snowflake:// dialect, gosnowflake, JDBC; gated on snowflake / snowflake:// / snowflakecomputing.com) parses the SQL table into a Class:<Table> resource node + QUERIES dependency edge from the connecting function (emitSnowflakeTargets, mirrors emitCQLTargets). Tableless/runtime SQL honest-skipped. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.snowflake ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
