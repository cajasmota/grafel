<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.serverless-framework` — Serverless Framework

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-30` | — | `internal/engine/serverless_edges.go`<br>`internal/engine/serverless_framework_edges.go` | — |
| Iac cross stack reference | — `not_applicable` | — | — | — | The Serverless Framework parser emits no cross-stack / Fn::ImportValue reference. Honest-missing. |
| Iac output export extraction | — `not_applicable` | — | — | — | The Serverless Framework parser extracts functions/events/handlers; it does not parse the `resources.Outputs` CloudFormation block into output/export entities (the 'exports' matches in serverless_edges.go are JS handler exports, not stack outputs). Honest-missing. |
| Resource extraction | ✅ `full` | `2026-05-30` | — | `internal/engine/serverless_framework_edges.go`<br>`internal/engine/serverless_framework_parse.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.serverless-framework ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
