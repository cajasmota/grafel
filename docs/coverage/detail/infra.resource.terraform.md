<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.resource.terraform` — Terraform / OpenTofu / Vault / Nomad / Packer / Waypoint

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `dependency_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/hcl/relationships.go` |
| `resource_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/hcl/extractor.go`<br>`internal/extractors/hcl/relationships.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.resource.terraform ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
