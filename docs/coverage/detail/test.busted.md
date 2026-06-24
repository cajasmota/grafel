<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.busted` — busted

Auto-generated. Back to [summary](../summary.md).

- **Language:** [lua](../by-language/lua.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | ✅ `full` | `2026-06-24` | 5365 | `internal/engine/http_endpoint_e2e_testmap_4749_lua_test.go`<br>`internal/engine/tests_edges.go`<br>`internal/extractors/cross/testmap/frameworks_lua.go` | busted test cases link to production via TESTS edges: high-confidence direct-call resolution from the it() body, plus e2e route TESTS edges (test -> HTTP endpoint, #4749) verified for GET/POST in http_endpoint_e2e_testmap_4749_lua_test.go. |
| Target extraction | ✅ `full` | `2026-06-24` | 5365 | `internal/custom/lua/testing.go`<br>`internal/extractors/cross/testmap/frameworks_lua.go`<br>`internal/extractors/cross/testmap/frameworks_lua_test.go` | busted BDD describe()/it()/pending() leaf cases are extracted as test cases (lua_testing extractor) and, in testmap/frameworks_lua.go, each it() body is balance-parsed (Lua keyword-balanced block extractor) and scanned for direct production calls; the enclosing describe() subject is the naming-convention fallback when the leaf body has no resolvable call. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.busted ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
