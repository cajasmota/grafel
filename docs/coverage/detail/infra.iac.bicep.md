<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.bicep` — Azure Bicep

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 9

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-30` | — | `internal/extractors/bicep/extractor.go` | DEPENDS_ON edges from symbolic-name property references (foo.id / foo.properties.x / foo.outputs.y) and explicit dependsOn:[...] arrays between resources/modules (mirrors hcl depends_on->DEPENDS_ON edge kind); module 'path.bicep' -> IMPORTS edge. Edges emitted as Format-A structural-refs bound via byLocation to sibling entities in the same file. |
| Iac cross stack reference | — `not_applicable` | — | — | — | Bicep has no cross-stack / remote-state construct in the grammar this extractor parses; symbolic-name references and `existing` resources are intra-template / same-deployment lookups, not cross-stack joins. Honest-missing. |
| Iac environment region account | — `not_applicable` | — | — | — | Bicep expresses region as each resource's `location` and account/subscription via targetScope / deployment scope; the Bicep extractor emits resource/param entities and symbolic-name DEPENDS_ON edges but does not read the location expression or targetScope and stamps no region/account/provider/runtime environment-targeting property. Honest-missing. |
| Iac event source wiring | — `not_applicable` | — | — | — | Bicep declares event sources (eventGrid subscriptions, serviceBus, storage queues) as ordinary resources; the extractor emits DEPENDS_ON edges from symbolic-name references with no dedicated event-source→function trigger edge or trigger-type attribution distinguishing a trigger from any other resource dependency. Honest-missing. |
| Iac iam grant attribution | — `not_applicable` | — | — | — | Bicep models IAM via roleAssignments resources, not a grant-call idiom; the extractor emits DEPENDS_ON edges from symbolic-name references with no grant=<method> attribution distinguishing an IAM grant from any other resource dependency. Honest-missing. |
| Iac output export extraction | ✅ `full` | `2026-06-04` | — | `internal/extractors/bicep/extractor.go` | Bicep `output <name> <type> = ...` declarations are extracted as SCOPE.Schema/output entities by `reOutput` (internal/extractors/bicep/extractor.go:80) + `extractOutputs` (internal/extractors/bicep/extractor.go:357), named by the output identifier — the values a Bicep module/template publishes to its caller. |
| Iac resource property extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/grafel/issues/4199) | `internal/extractors/bicep/extractor.go` | Typed resource property-bag fields are stamped onto the resource entity Metadata: the deployed name: attribute (reNameAttr -> deployed_name) and the @<apiVersion> of the Azure type (api_version), plus resource_category, in `extractResources` (internal/extractors/bicep/extractor.go:161). Two genuine typed property-bag values are stamped per resource; the broader bag (sku/location/kind/properties{}) is not, so partial. |
| Iac stack app topology | ✅ `full` | `2026-06-24` | [link](https://github.com/cajasmota/grafel/issues/4200) | `internal/extractors/bicep/extractor.go` | Module-composition topology is extracted: each 'module <name> <path> = {…}' declaration is emitted as a SCOPE.Component / subtype=module composition entity (reModule -> extractModules) carrying source=<path> plus an IMPORTS containment edge. Local paths bind to the sibling child scope:component:file:bicep:<path>.bicep node. Module-REGISTRY references are now classified (#5372): br:/ts: full refs and the bicepconfig moduleAlias forms br/<alias>:… / ts/<alias>:… are parsed (classifyModuleRegistry) into module_registry (acr | mcr | template-spec) + registry_scheme + registry_ref + registry_tag (+ registry_alias) Metadata, and route to an external scope:component:external:bicep:<scheme>:<ref>:<tag> module node instead of a bogus local-file edge. Full for the Bicep module composition idiom across local + registry + template-spec sources. |
| Resource extraction | ✅ `full` | `2026-06-24` | — | `internal/extractors/bicep/extractor.go`<br>`internal/extractors/bicep/kinds.go` | Regex/line-based .bicep extractor (no tree-sitter grammar vendored): SCOPE.InfraResource per 'resource' decl named by symbolic name, Kind-stable with uniform resource_category from the shared types.IaCResourceCategory classifier (#3549; bicepResourceCoarseScope delegates to it, resource_scope kept as an alias); azure_rp_type + api_version + deployed_name on Metadata. SCOPE.Component/module per 'module' decl, SCOPE.Schema for param/var/output. Handles 'existing' resources and [for ...] loops. bicepconfig.json is routed to this extractor (classifier #5372) and parsed (extractBicepConfig) into a SCOPE.Config entity + one SCOPE.Schema/module-alias entity per moduleAliases.br / moduleAliases.ts registry alias (registry / modulePath / subscription / resourceGroup on Metadata), so registry-aliased module refs are resolvable. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.bicep ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
