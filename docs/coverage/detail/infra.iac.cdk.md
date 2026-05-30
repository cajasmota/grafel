<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.cdk` — AWS CDK

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3512) | `internal/engine/cdk_edges.go` | CDK-TS dependency edges implemented: DEPENDS_ON from grant calls (bucket.grantRead(fn)), Lambda addEventSource, and construct vars passed through props; mirrors the hcl extractor's depends_on->DEPENDS_ON edge kind. CDK-Python/Java/Go/C# pending. Was citing the dormant rules/cdk/_manifest.yaml. |
| Resource extraction | 🟢 `partial` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3512) | `internal/engine/cdk_edges.go`<br>`internal/engine/rules/javascript_typescript/frameworks/aws_cdk.yaml` | CDK-TS implemented (applyCDKEdges): SCOPE.InfraResource per construct named by LogicalId. Stack/App scope via rules/javascript_typescript/frameworks/aws_cdk.yaml. CDK-Python/Java/Go/C# pending. Was citing the dormant rules/cdk/_manifest.yaml (knowledge metadata, never fired). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.cdk ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
