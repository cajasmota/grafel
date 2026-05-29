<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.ginkgo` — Ginkgo

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3216) | `internal/custom/golang/test_frameworks.go` | spec/container/hook patterns are emitted flat with description+focus_state props; nesting (spec->container) and spec->production-call edges are not yet resolved |
| Target extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/test_frameworks.go` | ginkgo Describe/Context/When containers, It/Specify specs (incl. focus/pending F-/P-/X- variants), and Before/AfterEach hooks (custom_go_ginkgo); proving fixture testdata/ginkgo_specs.go |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.ginkgo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
