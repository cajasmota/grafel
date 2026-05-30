<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.resource.aws-cdk` — AWS CDK

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Resource extraction | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3512) | `internal/engine/cdk_edges.go`<br>`internal/engine/rules/javascript_typescript/frameworks/aws_cdk.yaml`<br>`internal/engine/rules/python/frameworks/aws_cdk.yaml` | CDK-TS + CDK-Python: applyCDKEdges (TS) / applyCDKEdgesPython emit SCOPE.InfraResource per construct named by its 'LogicalId' literal (construct_type + coarse resource_scope). CDK-Java/Go/C# not yet implemented. Previously over-stamped full citing the hcl/Terraform extractor, which cannot parse .ts/.py. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.resource.aws-cdk ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
