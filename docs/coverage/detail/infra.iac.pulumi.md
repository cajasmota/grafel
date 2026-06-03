<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.pulumi` — Pulumi

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 8

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go`<br>`internal/engine/pulumi_edges_go_net.go` | Pulumi-TS + Pulumi-Python (applyPulumiEdges / applyPulumiEdgesPython) and Pulumi-Go + Pulumi-C# (applyPulumiEdgesGoNet, #3550): DEPENDS_ON from output references (TS other.arn/.id; Go/C# .Arn/.Id/.ID() passed into a resource's args), explicit dependsOn/depends_on (TS/Py) and DependsOn lists (Go pulumi.DependsOn([]pulumi.Resource{...}) / C# CustomResourceOptions.DependsOn). StackReference cross-stack nodes for TS/Py (collapsed onto pulumi-stack:<ref>). Mirrors the hcl extractor's DEPENDS_ON edge kind. Pulumi-Java pending; Go/C# StackReference + ComponentResource deferred. Was citing the dormant rules/pulumi/_manifest.yaml (never fired: loader keys rules by top-level dir, no file is tagged 'pulumi'). |
| Iac cross stack reference | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go` | `new pulumi.StackReference("org/project/stack")` is matched by pulumiTSStackRefRe (pulumi_edges.go:122-125) and pulumiPyStackRefRe (:158-160) and emits a DEPENDS_ON to a `pulumi-stack:<ref>` cross-stack node (pulumiCrossStackNodeID, :93-96) — the cross-stack join. Partial: Pulumi-TS + Pulumi-Python only; Go/C# StackReference deferred (per resource_extraction notes). |
| Iac environment region account | — `not_applicable` | — | — | — | Pulumi sets region/account via provider config (aws:region) in Pulumi.<stack>.yaml / explicit provider constructors and runtime in the function resource args; pulumi_edges.go extracts resource constructors and output-ref dependency edges but does not parse stack config or runtime and stamps no region/account/provider/runtime environment-targeting property. Honest-missing. |
| Iac event source wiring | — `not_applicable` | — | — | — | Pulumi declares event sources programmatically (aws.lambda.EventSourceMapping, aws.cloudwatch.EventRule) as ordinary resources; pulumi_edges.go emits no dedicated event-source→function trigger edge or trigger-type attribution. Honest-missing. |
| Iac iam grant attribution | — `not_applicable` | — | — | — | Pulumi IAM is modelled via RolePolicyAttachment / Policy resource constructors and output-ref dependency edges (pulumi_edges_go_net.go:82-85); the extractor emits no grantee→target edge carrying a grant=<method> property — IAM relations are indistinguishable from any other inter-resource dependency. Honest-missing. |
| Iac output export extraction | — `not_applicable` | — | — | — | The Pulumi extractor emits no stack-output/export node: `producer.Id`/`.Arn` 'output refs' are inter-resource dependency edges into another resource's args (pulumi_edges_go_net.go:82-85), not extraction of a stack's published outputs/exports as entities. Honest-missing. |
| Iac stack app topology | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/4200) | `internal/engine/pulumi_edges.go` | Component/module composition entity is extracted: each `class X extends pulumi.ComponentResource` (TS) / `class X(pulumi.ComponentResource)` (Py) subclass is emitted as a component-scoped topology entity (resource_scope=component / resource_category=component — the module boundary node) via applyPulumiEdges Pass 3 (pulumiTSComponentRe → emitResource(name,"pulumi.ComponentResource","component",…), pulumi_edges.go:117-119,358-364) and applyPulumiEdgesPython Pass 3 (pulumiPyComponentRe, :154-155,458-464). Partial: the ComponentResource composition boundary node is emitted and queryable, but no explicit ComponentResource→child-resource containment edge is emitted (child resources are flat resource nodes). |
| Resource extraction | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go`<br>`internal/engine/pulumi_edges_go_net.go`<br>`internal/engine/rules/javascript_typescript/frameworks/pulumi.yaml`<br>`internal/engine/rules/python/frameworks/pulumi.yaml` | Pulumi-TS + Pulumi-Python (applyPulumiEdges / applyPulumiEdgesPython) and Pulumi-Go + Pulumi-C# (applyPulumiEdgesGoNet, #3550): SCOPE.InfraResource per resource constructor named by its logical-name string literal (construct_type + uniform resource_category from the shared types.IaCResourceCategory classifier; Go factory pkg.NewType maps to pkg.Type via New-strip). ComponentResource subclasses recorded as component-scoped nodes for TS/Py. Program-scope idioms via rules/{javascript_typescript,python}/frameworks/pulumi.yaml. Pulumi-Java pending. Was over-stamped via the dormant rules/pulumi/_manifest.yaml. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.pulumi ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
