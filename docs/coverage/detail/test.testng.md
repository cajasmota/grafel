<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.testng` — TestNG

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | — | — | — |
| Target extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3753) | `internal/engine/rules/java/test_patterns.yaml`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | TestNG @Test methods in *Test.java files route through testmap detectJUnit (filename hint); org.testng import-hint not yet registered, so import-only TestNG files are a known partial miss. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.testng ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
