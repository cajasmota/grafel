<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.framework.wordpress` — WordPress

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — | — |
| `endpoint_synthesis` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/php/frameworks/wordpress.yaml` | — |
| `handler_attribution` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/php/frameworks/wordpress.yaml` | — |
| `middleware_coverage` | ❌ `missing` | — | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.framework.wordpress ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
