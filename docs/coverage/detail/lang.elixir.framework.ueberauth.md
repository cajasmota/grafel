<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.ueberauth` — Ueberauth (Elixir OAuth)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [security](../by-category/security.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth policy | 🟢 `partial` | `2026-05-31` | — | `internal/custom/elixir/ueberauth.go`<br>`internal/custom/elixir/ueberauth_test.go` | plug Ueberauth entrypoint + configured Ueberauth.Strategy.* OAuth providers + handle_request!/handle_callback! handlers emitted as auth entities (auth=true, auth_provider=<provider>, auth_method=oauth2) consumed by archigraph_auth_coverage. |
| Secret detection | 🔴 `missing` | — | — | — | — |
| SQL injection | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.ueberauth ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
