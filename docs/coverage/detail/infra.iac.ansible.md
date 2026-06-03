<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.iac.ansible` — Ansible (playbooks)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** IaC / Provisioning
- **Capability cells:** 7

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-05-28` | — | `internal/extractors/yaml/extractor.go` | — |
| Iac cross stack reference | — `not_applicable` | — | — | — | Ansible has no cross-stack / remote-state construct and the yaml extractor emits no cross-stack reference. Honest-missing. |
| Iac environment region account | — `not_applicable` | — | — | — | Ansible playbooks pass region/account targeting as ad-hoc task-module args (e.g. aws_region:, region:) or environment/inventory vars, not a uniform stack-level deployment target; the yaml extractor emits generic task/resource entities and stamps no region/account/provider/runtime environment-targeting property. Honest-missing. |
| Iac event source wiring | — `not_applicable` | — | — | — | Ansible playbooks model event-driven invocation via task modules, not a function-event-source binding; the yaml extractor emits generic task/resource entities with no event-source→function trigger edge or trigger-type attribution. Honest-missing. |
| Iac iam grant attribution | — `not_applicable` | — | — | — | Ansible playbooks model IAM via task modules (e.g. iam_policy), not a grant-call idiom; the yaml extractor emits generic task/resource entities with no grantee→target grant edge or grant-method attribution. Honest-missing. |
| Iac output export extraction | — `not_applicable` | — | — | — | Ansible playbooks have no stack-output/export construct (register / set_fact are runtime task vars, not published stack outputs); the yaml extractor emits no output/export entity. Honest-missing. |
| Resource extraction | 🟢 `partial` | `2026-05-28` | — | `internal/extractors/yaml/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.iac.ansible ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
