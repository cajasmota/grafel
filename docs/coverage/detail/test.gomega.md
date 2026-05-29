<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.gomega` — Gomega

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3216) | `internal/custom/golang/test_frameworks.go` | assertion patterns capture matcher + polarity but the asserted subject expression is not resolved to a production entity, so no TESTS/REFERENCES edge is synthesised yet |
| Target extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/test_frameworks.go` | gomega Expect/Ω/Eventually/Consistently assertions with polarity (To/ToNot/Should/ShouldNot) and matcher-constructor name extraction (custom_go_gomega); proving fixture testdata/gomega_matchers.go |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.gomega ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
