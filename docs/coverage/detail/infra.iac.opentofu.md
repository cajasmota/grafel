<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.opentofu` — OpenTofu (HCL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | Full dependency_attribution parity with Terraform via the shared hcl extractor: depends_on->DEPENDS_ON edges, for_each/count USES iteration-source edges, module I/O data-flow edges, and data.terraform_remote_state cross-stack DEPENDS_ON all fire for .tofu identically to .tf because entities carry Language="terraform". Same downstream IaC engine passes apply (iac_sns_edges, event_bus_edges, dynamic_patterns_terraform are gated on lang=="terraform"). |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/extractors/hcl` | OpenTofu (#3553) is the Apache-licensed Terraform fork with byte-for-byte identical HCL; the classifier routes .tofu / .tofu.json to the shared "terraform" language token, so .tofu files flow through the same hcl/terraform tree-sitter extractor as .tf with zero extra code. Full resource_extraction parity: resource/data/module/provider/variable/output/locals blocks, dynamic-block child entities, terraform{} settings, and the uniform resource_category classifier (#3549) all apply unchanged. Cited at the extractor package; classifier routing in internal/classifier/classifier.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.opentofu ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
