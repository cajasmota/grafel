<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.hibernate` — Hibernate ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `migration_parsing` | ❌ `missing` | — | — | — | — |
| `model_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/orm_field_edges.go`<br>`internal/engine/rules/java/orms/hibernate_core.yaml` |
| `query_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/orm_queries.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.hibernate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
