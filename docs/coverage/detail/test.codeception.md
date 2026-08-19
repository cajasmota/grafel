<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.codeception` — Codeception

Auto-generated. Back to [summary](../summary.md).

- **Language:** [PHP](../by-language/php.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | — | — | `internal/custom/php/test_data.go` | Codeception module imports + Actor type dependencies (AcceptanceTester/FunctionalTester/UnitTester) scanned. |
| Target extraction | 🟢 `partial` | — | — | `internal/custom/php/test_data.go`<br>`internal/engine/rules/php/test_patterns.yaml` | Cest class declarations + public test methods with Actor params extracted. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.codeception ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
