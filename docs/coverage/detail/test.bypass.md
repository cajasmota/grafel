<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.bypass` — Bypass

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: detection-only catalogue entry. See target_extraction note. |
| Target extraction | 🔴 `missing` | — | 4916 | `internal/engine/rules/elixir/test_patterns.yaml` | #4916: Bypass (local HTTP server for stubbing outbound calls in tests) is DETECTION-cataloged in test_patterns.yaml (hex_package bypass; Bypass.open() / Bypass.expect( / Bypass.expect_once( / Bypass.down(). No target extraction yet — attributing Bypass.expect stubs to the outbound HTTPoison/Finch/Req call under test (http_mocking) is unimplemented. Follow-up: testmap http-mock resolver. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.bypass ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
