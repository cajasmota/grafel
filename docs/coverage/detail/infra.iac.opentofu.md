<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.opentofu` — OpenTofu (HCL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 9

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | Full dependency_attribution parity with Terraform via the shared hcl extractor: depends_on->DEPENDS_ON edges, for_each/count USES iteration-source edges, module I/O data-flow edges, and data.terraform_remote_state cross-stack DEPENDS_ON all fire for .tofu identically to .tf because entities carry Language="terraform". Same downstream IaC engine passes apply (iac_sns_edges, event_bus_edges, dynamic_patterns_terraform are gated on lang=="terraform"). |
| Iac cross stack reference | ✅ `full` | `2026-06-04` | — | `internal/extractors/hcl` | Full parity with Terraform: `data.terraform_remote_state.*.outputs.*` in .tofu files emits the same cross-stack DEPENDS_ON edge (cross_stack=true) via extractRemoteStateDeps (terraform_deep.go:221-260), since entities carry Language="terraform". |
| Iac environment region account | — `not_applicable` | — | — | — | OpenTofu (shared hcl extractor) sets region inside the `provider` block (e.g. region = "us-east-1") and account/state via `backend`; extractProviderBlock records only the provider *name* label (extractor.go:402-416) — it never reads the region attribute — and terraform_deep.go captures only the backend *type* label (terraform_deep.go:367-372), so no region/account/runtime value is stamped as an environment-targeting property. Honest-missing. |
| Iac event source wiring | — `not_applicable` | — | — | — | OpenTofu (shared hcl extractor) declares event sources via aws_lambda_event_source_mapping / aws_cloudwatch_event_rule resources and interpolation/depends_on references, emitting generic DEPENDS_ON edges with no dedicated event-source→function trigger edge or trigger-type attribution. HCL has no event-source-binding idiom. Honest-missing. |
| Iac iam grant attribution | — `not_applicable` | — | — | — | Full parity with Terraform (shared hcl extractor): IAM is modelled declaratively via aws_iam_role_policy / aws_iam_*_policy_attachment resources and depends_on / interpolation references, emitting generic DEPENDS_ON edges with no grant=<method> attribution. No grant-call idiom exists in HCL. Honest-missing. |
| Iac output export extraction | ✅ `full` | `2026-06-04` | — | `internal/extractors/hcl` | Full parity with Terraform: `output "name" {}` blocks in .tofu files flow through the same extractOutputBlock (extractor.go:371-391) as .tf because the classifier routes .tofu to the shared "terraform" language token, emitting SCOPE.Schema/output entities identically. |
| Iac resource property extraction | 🟢 `partial` | `2026-06-03` | 4229 | `internal/extractors/hcl/extractor.go`<br>`internal/extractors/hcl/resource_properties.go` | Epic #4194: OpenTofu shares the hcl/terraform tree-sitter extractor, so .tofu/.tofu.json get the SAME curated scalar property stamping as .tf with zero extra code. stampResourceScalarProperties (resource_properties.go) stamps the curated allow-list (instance_type/runtime/memory_size/timeout/count/engine/port/...) of literal scalars (string_lit/numeric_lit/bool_lit) onto resource Properties; references/interpolations/collections are skipped (stay edges). Same value-asserting tests as terraform (resource_properties_test.go). PARTIAL: curated subset by design. DEPLOY-DEFERRED. |
| Iac stack app topology | ✅ `full` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/4200) | `internal/extractors/hcl/extractor.go`<br>`internal/extractors/hcl/relationships.go` | Full parity with Terraform (shared hcl extractor): each `module "<name>"` block in .tofu files is emitted as a SCOPE.Component / subtype=module composition entity (extractModuleBlock, hcl/extractor.go:297-319) plus a file→module IMPORTS containment edge (import_kind=module, hcl/relationships.go:148-168). Full for the HCL `module` composition idiom. |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | OpenTofu (#3553) is the Apache-licensed Terraform fork with byte-for-byte identical HCL; the classifier routes .tofu / .tofu.json to the shared "terraform" language token, so .tofu files flow through the same hcl/terraform tree-sitter extractor as .tf with zero extra code. Full resource_extraction parity: resource/data/module/provider/variable/output/locals blocks, dynamic-block child entities, terraform{} settings, and the uniform resource_category classifier (#3549) all apply unchanged. Cited at the extractor package; classifier routing in internal/classifier/classifier.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.opentofu ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
