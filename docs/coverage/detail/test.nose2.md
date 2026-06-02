<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.nose2` — nose2

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | — `not_applicable` | — | 3078 | — | — |
| Target extraction | 🔴 `missing` | — | 3078 | — | ApplyNose2Targets removed (#3655): never wired into cmd/archigraph/index.go's applyPass list, so it emitted zero edges in production. Its edges were also test-entity self-loops (FromID==ToID), which ComputeCoverage discards (ToID not a production entity). nose2 TestCase methods are already discovered and linked to production by ApplyTestsViaImports. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.nose2 ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
