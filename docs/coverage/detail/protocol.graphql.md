<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.graphql` — GraphQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `cross_repo_linkage` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/http_endpoint_match.go` |
| `method_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/graphql_subscriptions.go` |
| `service_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/graphql_subscriptions.go`<br>`internal/engine/rules/graphql/frameworks/graphql_schema.yaml` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.graphql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
