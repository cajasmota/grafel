<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.mockito` — Mockito

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | — | — | — |
| Target extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3753) | `internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | Mockito when(svc.method()).thenReturn(...) stubs resolve to a medium-confidence TESTS edge on the stubbed production method via testmap mockSetupREs; direct calls on the SUT in @Test bodies resolve high. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.mockito ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
