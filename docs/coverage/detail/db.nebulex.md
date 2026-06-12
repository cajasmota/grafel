<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `db.nebulex` — Nebulex (cache)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [databases](../by-category/databases.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/extras.yaml` | #4916: detection-only catalogue entry; no cache dependency attribution. See resource_extraction note. |
| Resource extraction | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/extras.yaml` | #4916: Nebulex (primary Elixir caching lib; local ETS / Redis / distributed multi-level) is DETECTION-cataloged in extras.yaml under category caching (hex_package nebulex/nebulex_redis_adapter/nebulex_local_adapter; use Nebulex.Cache / @decorate cacheable(/cache_put(/cache_evict( / Cache.put|get|delete(). No cache-resource entity extraction yet — emitting a cache resource node per use Nebulex.Cache module + attributing @decorate cacheable functions is unimplemented. extras.yaml at the language root is a catalogue, not loaded by the framework-rule loader. Follow-up: Nebulex resource extractor. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update db.nebulex ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
