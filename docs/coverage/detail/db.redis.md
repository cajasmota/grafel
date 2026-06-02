<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.redis` — Redis (keys)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/redis_pubsub_edges.go` | — |
| Resource extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/redis_pubsub_edges.go` | — |

## Code-level coverage

This infra record tracks datastore-level extraction (resources, dependency
attribution). The deep, code-level coverage for this technology lives in the
per-language driver/ORM records below — each one is a separate detail page.

| Record | Language | Kind | Status |
|--------|----------|------|--------|
| [`lang.c-cpp.driver.redis-plus-plus`](./lang.c-cpp.driver.redis-plus-plus.md) | C/C++ | driver | 1 full, 3 missing, 7 n/a |
| [`lang.csharp.driver.redis`](./lang.csharp.driver.redis.md) | C# | driver | 1 full, 3 missing, 7 n/a |
| [`lang.elixir.driver.redix`](./lang.elixir.driver.redix.md) | elixir | driver | 4 missing, 7 n/a |
| [`lang.go.driver.redis`](./lang.go.driver.redis.md) | go | driver | 2 partial, 3 missing, 6 n/a |
| [`lang.java.orm.spring-data-redis`](./lang.java.orm.spring-data-redis.md) | java | orm | 6 missing, 5 n/a |
| [`lang.jsts.driver.redis`](./lang.jsts.driver.redis.md) | JS/TS | driver | 1 full, 3 missing, 7 n/a |
| [`lang.php.driver.redis`](./lang.php.driver.redis.md) | php | driver | 1 full, 3 missing, 7 n/a |
| [`lang.python.driver.redis`](./lang.python.driver.redis.md) | python | driver | 1 full, 3 missing, 7 n/a |
| [`lang.ruby.driver.redis`](./lang.ruby.driver.redis.md) | ruby | driver | 1 full, 3 missing, 7 n/a |
| [`lang.rust.driver.redis`](./lang.rust.driver.redis.md) | rust | driver | 1 full, 3 missing, 7 n/a |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.redis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
