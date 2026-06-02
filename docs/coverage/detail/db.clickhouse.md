<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.clickhouse` — ClickHouse

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_datastore_infra.go` | — |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/orm_queries_datastore_infra.go` | ClickHouse client.execute/query("... FROM table") (clickhouse-driver/clickhouse-connect/clickhouse-go, gated on clickhouse import / clickhouse:// / :8123) parses the SQL table into a Class:<Table> resource node + QUERIES dependency edge from the connecting function (emitClickHouseTargets, mirrors emitCQLTargets). Tableless/runtime SQL honest-skipped. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.clickhouse ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
