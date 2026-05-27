<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.javascript.orm.prisma` — Prisma

Auto-generated. Back to [summary](../summary.md).

- **Language:** [javascript](../by-language/javascript.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `migration_parsing` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/prisma/_manifest.yaml` |
| `model_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/orms/prisma.yaml`<br>`internal/engine/rules/javascript_typescript/orms/prisma_client_js.yaml`<br>`internal/engine/rules/prisma/_manifest.yaml` |
| `query_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/orm_queries_jsts.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.javascript.orm.prisma ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
