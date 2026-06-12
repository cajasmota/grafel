<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.faker` — Faker / ExMachina

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: detection-only catalogue entry. See target_extraction note. |
| Target extraction | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: Faker (random test data) and ExMachina (factory library, FactoryBot analogue) are DETECTION-cataloged in test_patterns.yaml under category test_data (hex_package faker/ex_machina; use ExMachina(.Ecto) / factory :user do / insert!/ build( / params_for( / Faker.*.). No target extraction yet — resolving factory :x -> the Ecto schema it builds, or insert!(:user) -> the schema fixture, is unimplemented. Follow-up: testmap factory-to-schema resolver. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.faker ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
