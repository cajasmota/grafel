<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.pest` — Pest

Auto-generated. Back to [summary](../summary.md).

- **Language:** [PHP](../by-language/php.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | — | — | `internal/custom/php/test_data.go` | uses(ClassName::class) declarations + beforeEach/afterEach hooks + arch() architectural constraints scanned. |
| Target extraction | 🟢 `partial` | — | — | `internal/custom/php/test_data.go`<br>`internal/engine/rules/php/test_patterns.yaml` | it()/test() declarations + describe() blocks + dataset() declarations extracted. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.pest ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
