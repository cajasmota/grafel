<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.terraform` — Terraform (HCL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | #3527 deepened: for_each/count meta-args emit USES iteration-source edges (each.*/count.*/self.* pseudo-refs suppressed); module I/O data-flow edges (module.this USES module.x tagged with the consuming input arg); data.terraform_remote_state.X.outputs.Y → cross-stack DEPENDS_ON (cross_stack=true); dynamic blocks emit child dynamic_block entities with CONTAINS+CALLS; terraform{} required_providers → per-provider IMPORTS(import_kind=required_provider, version). Remaining gaps: module input → child variable binding is intra-file CALLS only (no cross-module-file output/variable resolution); moved/import blocks not yet emitted as rename/adopt edges. |
| Iac cross stack reference | ✅ `full` | `2026-06-04` | — | `internal/extractors/hcl` | `data.terraform_remote_state.<name>.outputs.<key>` consumption is detected by extractRemoteStateDeps/remoteStateName (terraform_deep.go:221-269) and emits one cross-stack DEPENDS_ON edge per distinct remote-state name (cross_stack=true, remote_state=<name>) — deliberately NOT a generic data-source CALLS edge; it wires one stack to another stack's published outputs. |
| Iac output export extraction | ✅ `full` | `2026-06-04` | — | `internal/extractors/hcl` | `output "name" {}` blocks are extracted by extractOutputBlock (extractor.go:175-176,371-391) as SCOPE.Schema entities with Subtype=output, named by the output identifier — the values a Terraform module/root publishes for consumption. |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | #3527: dynamic "x" {} nested blocks now emitted as SCOPE.Component/dynamic_block child entities (previously dropped by the top-level-only walker); terraform{} settings captured as a SCOPE.Component/terraform_settings entity (required_providers+backend+required_version) instead of being dropped; lifecycle/provisioner meta-blocks recorded in resource Metadata.meta_blocks. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.terraform ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
