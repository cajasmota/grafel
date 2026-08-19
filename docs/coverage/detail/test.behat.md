<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.behat` — Behat

Auto-generated. Back to [summary](../summary.md).

- **Language:** [PHP](../by-language/php.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | — | — | `internal/custom/php/test_data.go` | Behat Context class deps (implements Context/extends RawMinkContext) + step annotation targets scanned. |
| Target extraction | 🟢 `partial` | — | — | `internal/custom/php/test_data.go`<br>`internal/engine/rules/php/test_patterns.yaml` | Feature/Scenario declarations in .feature files + step annotation patterns in PHP context classes extracted. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.behat ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
