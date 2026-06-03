<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.cloudformation` — AWS CloudFormation

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 7

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-30` | — | `internal/engine/iac_cloudformation_edges.go` | — |
| Iac cross stack reference | ✅ `full` | `2026-06-04` | — | `internal/engine/iac_cloudformation_edges.go` | `!ImportValue Name` and `{ "Fn::ImportValue": Name }` are matched by cfnImportValueShortRe/cfnImportValueLongRe (iac_cloudformation_edges.go:314-315) and emit a consumer-side DEPENDS_ON edge (cross_stack=true) to the same `cfn-export:<name>` node a producing stack's Export collapses onto (iac_cloudformation_edges.go:549-557) — the cross-stack join. |
| Iac environment region account | — `not_applicable` | — | — | — | CloudFormation templates carry no region/account in the template body (region/account are supplied at deploy time by the stack/stackset); a Lambda's Runtime sits in its resource Properties but the extractor emits generic resource/dependency edges and stamps no region/account/provider/runtime environment-targeting property. Honest-missing. |
| Iac event source wiring | — `not_applicable` | — | — | — | CloudFormation declares event sources via AWS::Lambda::EventSourceMapping and AWS::Events::Rule resources plus Ref/GetAtt; the extractor emits generic resource dependency edges with no dedicated event-source→function trigger edge or trigger-type attribution. Honest-missing. |
| Iac iam grant attribution | — `not_applicable` | — | — | — | CloudFormation models IAM declaratively via AWS::IAM::Role/Policy resources and Ref/GetAtt; there is no grant-call idiom and the extractor emits no grantee→target edge carrying a grant=<method> property — IAM relations surface as generic resource dependency edges, indistinguishable from any other dependency. Honest-missing. |
| Iac output export extraction | ✅ `full` | `2026-06-04` | — | `internal/engine/iac_cloudformation_edges.go` | `Outputs.<O>.Export.Name` is scanned by cfnCollectExportNames (iac_cloudformation_edges.go:814-844) and emitted as a producer-side `cfn-export:<name>` SCOPE.Config entity with side=producer + export_name metadata (iac_cloudformation_edges.go:573-580). |
| Resource extraction | ✅ `full` | `2026-05-30` | — | `internal/engine/iac_cloudformation_edges.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.cloudformation ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
