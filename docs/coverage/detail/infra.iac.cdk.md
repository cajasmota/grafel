<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.cdk` — AWS CDK

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3512) | `internal/engine/cdk_edges.go` | CDK-TS + CDK-Python dependency edges implemented: DEPENDS_ON from grant calls (bucket.grantRead(fn) / data.grant_read(fn)), Lambda event sources (addEventSource / add_event_source), and construct vars passed through props/kwargs; mirrors the hcl extractor's depends_on->DEPENDS_ON edge kind. CDK-Java/Go/C# pending. Python branch: applyCDKEdgesPython. |
| Iac cross stack reference | — `not_applicable` | — | — | — | The CDK extractor emits no cross-stack / Fn.importValue / exportValue idiom; cross-construct edges are intra-app dependency edges, not cross-stack references. Honest-missing. |
| Iac output export extraction | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/3512) | `internal/engine/rules/javascript_typescript/frameworks/aws_cdk.yaml`<br>`internal/engine/rules/python/frameworks/aws_cdk.yaml` | CfnOutput is extracted as a SCOPE.Config entity named by its OutputId literal via the entity rule `(?:new\s+)?(?:cdk\.)?CfnOutput\s*\(\s*\w+\s*,\s*["']([^"']+)["']` (aws_cdk.yaml:54-58 TS, :51-52 Py, name_group=1). Partial: covers CDK-TS + CDK-Python only; CDK-Java/Go/C# CfnOutput pending (same gap as resource_extraction). |
| Resource extraction | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3512) | `internal/engine/cdk_edges.go`<br>`internal/engine/rules/javascript_typescript/frameworks/aws_cdk.yaml` | CDK-TS + CDK-Python implemented (applyCDKEdges / applyCDKEdgesPython): SCOPE.InfraResource per construct named by its 'LogicalId' literal (construct_type + coarse resource_scope). Stack/App scope via rules/{javascript_typescript,python}/frameworks/aws_cdk.yaml. CDK-Java/Go/C# pending. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.cdk ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
