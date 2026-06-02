<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.doctest` — doctest (stdlib)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | — `not_applicable` | — | 3078 | — | — |
| Target extraction | 🔴 `missing` | — | 3078 | — | ApplyDoctestTargets removed (#3655): it was never wired into cmd/archigraph/index.go's applyPass list, so it emitted zero edges in production. Its edges were also test-entity self-loops (FromID==ToID), which ComputeCoverage discards because the ToID is not a production entity. Doctest tests-via-docstring is not yet extracted. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.doctest ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
