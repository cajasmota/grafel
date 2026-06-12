<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.cassandra` — Apache Cassandra (schema)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | 3828 | `internal/engine/orm_queries_drivers_other.go` | No resource/dependency extraction yet for this datastore; tracked in #3828 (sibling datastores done — genuine build-gap). |
| Resource extraction | 🟢 `partial` | `2026-06-02` | 3828 | `internal/engine/orm_queries_drivers_other.go` | Cassandra/Scylla CQL session.execute("... FROM table") across C#/PHP/Rust/Python/Java/Ruby/JS parses the FROM/INTO/UPDATE table into a Class:<Table> resource node + QUERIES dependency edge from the connecting function (emitCQLTargets). Runtime-built CQL honest-skipped. |

## Related extraction records

This hub record tracks the technology at a high level. The deep, code-level
coverage for this technology lives in the per-language records below — each
one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.csharp.driver.cassandra`](./lang.csharp.driver.cassandra.md) | C# | driver | 1 full, 1 partial, 3 missing, 6 n/a |
| [`lang.elixir.driver.xandra`](./lang.elixir.driver.xandra.md) | elixir | driver | 1 full, 3 missing, 7 n/a |
| [`lang.go.driver.cassandra`](./lang.go.driver.cassandra.md) | go | driver | 2 partial, 3 missing, 6 n/a |
| [`lang.java.orm.spring-data-cassandra`](./lang.java.orm.spring-data-cassandra.md) | java | orm | 2 full, 4 missing, 5 n/a |
| [`lang.jsts.driver.cassandra`](./lang.jsts.driver.cassandra.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.cassandra`](./lang.php.driver.cassandra.md) | php | driver | 1 full, 3 missing, 7 n/a |
| [`lang.python.driver.cassandra`](./lang.python.driver.cassandra.md) | python | driver | 1 full, 3 missing, 7 n/a |
| [`lang.ruby.driver.cassandra`](./lang.ruby.driver.cassandra.md) | ruby | driver | 1 full, 3 missing, 7 n/a |
| [`lang.rust.driver.cassandra`](./lang.rust.driver.cassandra.md) | rust | driver | 1 full, 3 missing, 7 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.cassandra ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
