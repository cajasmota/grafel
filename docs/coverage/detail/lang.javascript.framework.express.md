<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.javascript.framework.express` — Express

Auto-generated. Back to [summary](../summary.md).

- **Language:** [javascript](../by-language/javascript.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — |
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/express.yaml`<br>`internal/extractors/javascript/framework_dsl.go` |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/express.yaml` |
| `middleware_coverage` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/express.yaml` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.javascript.framework.express ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
