<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.serverless-framework` — Serverless Framework

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 9

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-05-30` | — | `internal/engine/serverless_edges.go`<br>`internal/engine/serverless_framework_edges.go` | — |
| Iac cross stack reference | — `not_applicable` | — | — | — | The Serverless Framework parser emits no cross-stack / Fn::ImportValue reference. Honest-missing. |
| Iac environment region account | ✅ `full` | `2026-06-04` | [link](https://github.com/cajasmota/grafel/issues/4201) | `internal/engine/serverless_framework_edges.go`<br>`internal/engine/serverless_framework_parse.go` | The Serverless Framework parser reads the `provider:` block's runtime and region (`parseProviderBlock`, internal/engine/serverless_framework_parse.go:125-152 → slsManifest.runtime / .region) and the edge pass stamps the deployment target onto every emitted lambda function entity's Properties: provider="aws-lambda" (internal/engine/serverless_framework_edges.go), runtime=<provider.runtime> (:153-155) and region=<provider.region> (:156-158). Full for provider/runtime/region environment-targeting; account is not present in a serverless.yml provider block (it is resolved at deploy time), so the three targeting fields the manifest carries are all stamped. |
| Iac event source wiring | ✅ `full` | `2026-06-04` | [link](https://github.com/cajasmota/grafel/issues/4198) | `internal/engine/serverless_framework_parse.go` | The Serverless Framework parser reads each function's `events:` list (`parseEventsBlock`, internal/engine/serverless_framework_parse.go:247-286) and emits a TRIGGERS edge source→function per non-http trigger carrying event_type=<kind>: sqs (queue entity → fn, internal/engine/serverless_framework_edges.go), sns (topic → fn, :270-275), stream/kinesis (stream → fn, :296-301) and schedule (ScheduledJob → fn, :321-327). `slsTriggersEdgeKind` (internal/engine/serverless_framework_edges.go:59) is "TRIGGERS". Full for the events-block trigger idiom — each edge wires which event source (queue/topic/stream/schedule) invokes which function, with the trigger type recorded on event_type. |
| Iac iam grant attribution | — `not_applicable` | — | — | — | Serverless Framework IAM is configured declaratively (provider.iam / iamRoleStatements) and via embedded CloudFormation resources; the parser extracts functions/events/handlers and emits no grantee→target grant edge or grant=<method> attribution. Honest-missing. |
| Iac output export extraction | — `not_applicable` | — | — | — | The Serverless Framework parser extracts functions/events/handlers; it does not parse the `resources.Outputs` CloudFormation block into output/export entities (the 'exports' matches in serverless_edges.go are JS handler exports, not stack outputs). Honest-missing. |
| Iac resource property extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/grafel/issues/4199) | `internal/engine/serverless_framework_edges.go` | Each function entity is stamped with its typed function-config properties: handler, runtime, region, service (parseProviderBlock + parseFunctionBody -> fnProps, internal/engine/serverless_framework_edges.go) — genuine per-function settings on the entity. memory/timeout/environment are not parsed/stamped (`slsFunction`, internal/engine/serverless_framework_parse.go:23-28, has only name/handler/events), so partial. |
| Iac stack app topology | — `not_applicable` | — | — | — | A serverless.yml declares a single `service` deployment unit (slsManifest.service) plus a flat `functions:` list; the parser emits per-function lambda entities and event-source/trigger edges but models no stack/app/module composition hierarchy — there is no sub-stack or module node and no parent→child / app→stack containment relationship between deployment units. Honest-missing. |
| Resource extraction | ✅ `full` | `2026-05-30` | — | `internal/engine/serverless_framework_edges.go`<br>`internal/engine/serverless_framework_parse.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.serverless-framework ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
