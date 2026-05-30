<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.pulumi` — Pulumi

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go` | Pulumi-TS + Pulumi-Python dependency edges implemented (applyPulumiEdges / applyPulumiEdgesPython): DEPENDS_ON from output references (other.arn/.id/.url passed into a resource's args), explicit dependsOn/depends_on lists, and StackReference cross-stack nodes (collapsed onto pulumi-stack:<ref>). Mirrors the hcl extractor's DEPENDS_ON edge kind. Pulumi-Go/C#/Java pending. Was citing the dormant rules/pulumi/_manifest.yaml (never fired: loader keys rules by top-level dir, no file is tagged 'pulumi'). |
| Resource extraction | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go`<br>`internal/engine/rules/javascript_typescript/frameworks/pulumi.yaml`<br>`internal/engine/rules/python/frameworks/pulumi.yaml` | Pulumi-TS + Pulumi-Python implemented (applyPulumiEdges / applyPulumiEdgesPython): SCOPE.InfraResource per resource constructor named by its logical-name string literal (construct_type + coarse resource_scope); ComponentResource subclasses recorded as component-scoped nodes. Program-scope idioms via rules/{javascript_typescript,python}/frameworks/pulumi.yaml. Pulumi-Go/C#/Java pending. Was over-stamped via the dormant rules/pulumi/_manifest.yaml. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.pulumi ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
