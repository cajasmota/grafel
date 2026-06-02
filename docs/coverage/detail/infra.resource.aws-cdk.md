<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.resource.aws-cdk` — AWS CDK

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Resource extraction | 🟢 `partial` | `2026-05-31` | [link](https://github.com/cajasmota/archigraph/issues/3512) | `internal/engine/cdk_edges.go`<br>`internal/engine/cdk_edges_jvm_go_net.go`<br>`internal/engine/rules/javascript_typescript/frameworks/aws_cdk.yaml`<br>`internal/engine/rules/python/frameworks/aws_cdk.yaml` | CDK-TS + CDK-Python (applyCDKEdges / applyCDKEdgesPython) and CDK-Java + CDK-Go + CDK-C# (applyCDKEdgesJVMGoNet, #3550): SCOPE.InfraResource per construct named by its 'LogicalId' literal (construct_type + uniform resource_category from the shared types.IaCResourceCategory classifier, #3549; resource_scope kept as an alias). Java covers new-form + Type.Builder.create(...) fluent form; Go covers pkg.NewType(scope, jsii.String("Id"), ...) (New-strip to pkg.Type); C# covers new Type(this, "Id", ...). Java/C# bare construct types (Bucket/Table/Function) aliased to provider-qualified forms (s3.Bucket/dynamodb.Table/lambda.Function) so the classifier fires. grant*/Grant* calls -> grantee DEPENDS_ON resource. All five JSII bindings now extract; remains partial (single-file regex; event-source/L1-escape-hatch/cross-file deferred). TS/JS+Python depth deferred to follow-up. Previously over-stamped full citing the hcl/Terraform extractor, which cannot parse .ts/.py. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.resource.aws-cdk ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
