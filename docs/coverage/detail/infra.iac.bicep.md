<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.bicep` — Azure Bicep

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 7

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-30` | — | `internal/extractors/bicep/extractor.go` | DEPENDS_ON edges from symbolic-name property references (foo.id / foo.properties.x / foo.outputs.y) and explicit dependsOn:[...] arrays between resources/modules (mirrors hcl depends_on->DEPENDS_ON edge kind); module 'path.bicep' -> IMPORTS edge. Edges emitted as Format-A structural-refs bound via byLocation to sibling entities in the same file. |
| Iac cross stack reference | — `not_applicable` | — | — | — | Bicep has no cross-stack / remote-state construct in the grammar this extractor parses; symbolic-name references and `existing` resources are intra-template / same-deployment lookups, not cross-stack joins. Honest-missing. |
| Iac environment region account | — `not_applicable` | — | — | — | Bicep expresses region as each resource's `location` and account/subscription via targetScope / deployment scope; the Bicep extractor emits resource/param entities and symbolic-name DEPENDS_ON edges but does not read the location expression or targetScope and stamps no region/account/provider/runtime environment-targeting property. Honest-missing. |
| Iac event source wiring | — `not_applicable` | — | — | — | Bicep declares event sources (eventGrid subscriptions, serviceBus, storage queues) as ordinary resources; the extractor emits DEPENDS_ON edges from symbolic-name references with no dedicated event-source→function trigger edge or trigger-type attribution distinguishing a trigger from any other resource dependency. Honest-missing. |
| Iac iam grant attribution | — `not_applicable` | — | — | — | Bicep models IAM via roleAssignments resources, not a grant-call idiom; the extractor emits DEPENDS_ON edges from symbolic-name references with no grant=<method> attribution distinguishing an IAM grant from any other resource dependency. Honest-missing. |
| Iac output export extraction | ✅ `full` | `2026-06-04` | — | `internal/extractors/bicep/extractor.go` | Bicep `output <name> <type> = ...` declarations are extracted as SCOPE.Schema/output entities by reOutput + extractOutputs (extractor.go:77-78,121), named by the output identifier — the values a Bicep module/template publishes to its caller. |
| Resource extraction | ✅ `full` | `2026-05-31` | — | `internal/extractors/bicep/extractor.go`<br>`internal/extractors/bicep/kinds.go` | Regex/line-based .bicep extractor (no tree-sitter grammar vendored): SCOPE.InfraResource per 'resource' decl named by symbolic name, Kind-stable with uniform resource_category from the shared types.IaCResourceCategory classifier (#3549; bicepResourceCoarseScope now delegates to it, resource_scope kept as an alias); azure_rp_type + api_version + deployed_name on Metadata. SCOPE.Component/module per 'module' decl, SCOPE.Schema for param/var/output. Handles 'existing' resources and [for ...] loops. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.bicep ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
