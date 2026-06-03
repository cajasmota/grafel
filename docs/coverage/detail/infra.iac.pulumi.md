<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.pulumi` — Pulumi

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go`<br>`internal/engine/pulumi_edges_go_net.go` | Pulumi-TS + Pulumi-Python (applyPulumiEdges / applyPulumiEdgesPython) and Pulumi-Go + Pulumi-C# (applyPulumiEdgesGoNet, #3550): DEPENDS_ON from output references (TS other.arn/.id; Go/C# .Arn/.Id/.ID() passed into a resource's args), explicit dependsOn/depends_on (TS/Py) and DependsOn lists (Go pulumi.DependsOn([]pulumi.Resource{...}) / C# CustomResourceOptions.DependsOn). StackReference cross-stack nodes for TS/Py (collapsed onto pulumi-stack:<ref>). Mirrors the hcl extractor's DEPENDS_ON edge kind. Pulumi-Java pending; Go/C# StackReference + ComponentResource deferred. Was citing the dormant rules/pulumi/_manifest.yaml (never fired: loader keys rules by top-level dir, no file is tagged 'pulumi'). |
| Iac cross stack reference | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go` | `new pulumi.StackReference("org/project/stack")` is matched by pulumiTSStackRefRe (pulumi_edges.go:122-125) and pulumiPyStackRefRe (:158-160) and emits a DEPENDS_ON to a `pulumi-stack:<ref>` cross-stack node (pulumiCrossStackNodeID, :93-96) — the cross-stack join. Partial: Pulumi-TS + Pulumi-Python only; Go/C# StackReference deferred (per resource_extraction notes). |
| Iac output export extraction | — `not_applicable` | — | — | — | The Pulumi extractor emits no stack-output/export node: `producer.Id`/`.Arn` 'output refs' are inter-resource dependency edges into another resource's args (pulumi_edges_go_net.go:82-85), not extraction of a stack's published outputs/exports as entities. Honest-missing. |
| Resource extraction | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go`<br>`internal/engine/pulumi_edges_go_net.go`<br>`internal/engine/rules/javascript_typescript/frameworks/pulumi.yaml`<br>`internal/engine/rules/python/frameworks/pulumi.yaml` | Pulumi-TS + Pulumi-Python (applyPulumiEdges / applyPulumiEdgesPython) and Pulumi-Go + Pulumi-C# (applyPulumiEdgesGoNet, #3550): SCOPE.InfraResource per resource constructor named by its logical-name string literal (construct_type + uniform resource_category from the shared types.IaCResourceCategory classifier; Go factory pkg.NewType maps to pkg.Type via New-strip). ComponentResource subclasses recorded as component-scoped nodes for TS/Py. Program-scope idioms via rules/{javascript_typescript,python}/frameworks/pulumi.yaml. Pulumi-Java pending. Was over-stamped via the dormant rules/pulumi/_manifest.yaml. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.pulumi ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
