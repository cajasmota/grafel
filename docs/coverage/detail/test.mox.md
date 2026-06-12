<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.mox` — Mox

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: detection-only catalogue entry; no build/test-graph extraction. See target_extraction note. |
| Target extraction | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: Mox (behaviour mocking) is DETECTION-cataloged in test_patterns.yaml (hex_package mox; import Mox / Mox.defmock / expect( / stub( / verify_on_exit!). No TESTS-edge target extraction yet — mock_extraction (resolving expect(Mock, :fun, ...) to a TESTS edge on the behaviour callback) is unimplemented; test_patterns.yaml at the language root is a catalogue, not loaded by the framework-rule loader (loader.go only ingests frameworks//orms//queues/ subdir files). Follow-up: wire Mox into internal/extractors/cross/testmap/frameworks.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.mox ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
