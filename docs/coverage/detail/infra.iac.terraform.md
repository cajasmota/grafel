<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.terraform` — Terraform (HCL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | #3527 deepened: for_each/count meta-args emit USES iteration-source edges (each.*/count.*/self.* pseudo-refs suppressed); module I/O data-flow edges (module.this USES module.x tagged with the consuming input arg); data.terraform_remote_state.X.outputs.Y → cross-stack DEPENDS_ON (cross_stack=true); dynamic blocks emit child dynamic_block entities with CONTAINS+CALLS; terraform{} required_providers → per-provider IMPORTS(import_kind=required_provider, version). Remaining gaps: module input → child variable binding is intra-file CALLS only (no cross-module-file output/variable resolution); moved/import blocks not yet emitted as rename/adopt edges. |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | #3527: dynamic "x" {} nested blocks now emitted as SCOPE.Component/dynamic_block child entities (previously dropped by the top-level-only walker); terraform{} settings captured as a SCOPE.Component/terraform_settings entity (required_providers+backend+required_version) instead of being dropped; lifecycle/provisioner meta-blocks recorded in resource Metadata.meta_blocks. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.terraform ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
