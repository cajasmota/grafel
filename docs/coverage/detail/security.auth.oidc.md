<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `security.auth.oidc` — OIDC (OpenID Connect)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [security](../by-category/security.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_policy` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/java_auth_policy.go` |
| `secret_detection` | ❌ `missing` | — | — | — | — |
| `sql_injection` | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update security.auth.oidc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
