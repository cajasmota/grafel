<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.orm.efcore` — Entity Framework Core

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ❌ `missing` | — | — | — | — |
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/csharp/frameworks/entity_framework_core.yaml`<br>`internal/engine/rules/csharp/orms/entity_framework_core.yaml` | — |
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_other.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.orm.efcore ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
