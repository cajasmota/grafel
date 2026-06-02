<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.resource.cloudformation` — AWS CloudFormation

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-30` | — | `internal/engine/iac_cloudformation_edges.go` | — |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/engine/iac_cloudformation_edges.go` | cfnResourceKind now derives the SCOPE.Datastore/Queue/ServerlessFunction entity Kind from the shared types.IaCResourceCategory classifier and stamps the uniform resource_category property (#3549), so the CFN Kind and the cross-tool category can never diverge. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.resource.cloudformation ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
