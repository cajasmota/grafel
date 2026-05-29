<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.testify` — testify

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/test_frameworks.go`<br>`internal/engine/tests_edges.go` | suite/case/assertion patterns carry suite linkage props; combined with tests_edges.go TESTS-edge propagation for testify suite methods |
| Target extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/test_frameworks.go` | testify Suite struct + suite.Run registration + receiver-method test cases + assert/require assertion extraction (custom_go_testify); proving fixture testdata/testify_suite.go |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.testify ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
