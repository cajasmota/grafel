<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.spring-boot` — Spring Boot (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/java_auth_policy.go` | — |
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/kotlin/frameworks/spring_boot_kotlin.yaml`<br>`internal/engine/spring_routes_kotlin.go` | — |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/spring_routes_kotlin.go` | — |
| `middleware_coverage` | ❌ `missing` | — | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.spring-boot ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
