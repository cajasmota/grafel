<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.wallaby` — Wallaby

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: detection-only catalogue entry. See target_extraction note. |
| Target extraction | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: Wallaby (browser/E2E feature testing) is DETECTION-cataloged in test_patterns.yaml (hex_package wallaby/hound; use Wallaby.Feature / import Wallaby.Query / visit( / click( / fill_in( / assert_text(; test/features/ convention). No TESTS-edge or E2E-route target extraction yet (the elixir tests_route_e2e extractor covers Phoenix route reachability, not Wallaby sessions). Follow-up: testmap wiring + Wallaby session-to-route attribution. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.wallaby ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
