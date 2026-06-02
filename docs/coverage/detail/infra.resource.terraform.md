<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.resource.terraform` — Terraform / OpenTofu / Vault / Nomad / Packer / Waypoint

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-28` | — | `internal/extractors/hcl/relationships.go` | — |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl/extractor.go`<br>`internal/extractors/hcl/relationships.go` | resource blocks now carry the uniform resource_category metadata from the shared types.IaCResourceCategory classifier (#3549), so aws_db_instance→datastore, aws_sqs_queue→queue, aws_lambda_function→function are queryable cross-tool. Entity Kind stays SCOPE.Component/resource (edges unchanged). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.resource.terraform ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
