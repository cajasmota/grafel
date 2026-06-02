<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.resource.pulumi` — Pulumi

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Resource extraction | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3528) | `internal/engine/pulumi_edges.go`<br>`internal/engine/pulumi_edges_go_net.go`<br>`internal/engine/rules/javascript_typescript/frameworks/pulumi.yaml`<br>`internal/engine/rules/python/frameworks/pulumi.yaml` | Pulumi-TS + Pulumi-Python (applyPulumiEdges) and Pulumi-Go + Pulumi-C# (applyPulumiEdgesGoNet, #3550): SCOPE.InfraResource per resource constructor named by its logical-name string literal (construct_type + uniform resource_category from the shared types.IaCResourceCategory classifier, #3549; resource_scope kept as an alias). Go covers v, err := pkg.NewType(ctx, "name", ...) (New-strip to pkg.Type) + output-ref/DependsOn edges; C# covers new Provider.Svc.Type("name", ...) + output-ref/DependsOn edges. Pulumi-Java not yet implemented; Go/C# StackReference + ComponentResource deferred. TS/JS+Python depth deferred to follow-up. Previously over-stamped full citing the hcl/Terraform extractor, which cannot parse .ts/.py. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.resource.pulumi ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
