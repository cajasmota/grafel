<!-- DO NOT EDIT — generated from docs/coverage.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.spring-boot` — Spring Boot / Spring MVC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_coverage` | `full` | `2026-05-28` | — | — | `internal/engine/java_auth_policy.go` |
| `endpoint_synthesis` | `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/spring_routes.go` |
| `handler_attribution` | `full` | `2026-05-28` | — | — | `internal/engine/spring_routes.go` |

## Provenance

This record is sourced from `docs/coverage.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.spring-boot ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
