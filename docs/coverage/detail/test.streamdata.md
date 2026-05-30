<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.streamdata` — StreamData (property tests)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap StreamData property tests emit TESTS edges via cross/testmap extractor (#3473). |
| Target extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap StreamData property-test extraction: property "..." do leaves with subject-from-module-name + body call resolution (#3473). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.streamdata ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
