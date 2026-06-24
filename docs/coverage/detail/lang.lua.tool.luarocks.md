<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.lua.tool.luarocks` — LuaRocks

Auto-generated. Back to [summary](../summary.md).

- **Language:** [lua](../by-language/lua.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | ✅ `full` | `2026-06-24` | 5365 | `internal/extractors/cross/manifest/rockspec.go`<br>`internal/extractors/cross/manifest/rockspec_test.go` | luarocks.lock is a Lua return-table of exactly-pinned resolved versions (luarocks install --lock, 3.3+). parseLuarocksLock enumerates the dependencies map (bracketed ["name"]= and bare name= forms), emitting each as dependency_kind=locked — the resolved transitive closure the rockspec never names (#2865 contract). |
| Manifest parsing | ✅ `full` | `2026-06-24` | 5365 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/rockspec.go`<br>`internal/extractors/cross/manifest/rockspec_test.go` | *.rockspec is a Lua table literal; parseRockspec mines the dependencies / build_dependencies / test_dependencies arrays (each a list of "<name> <op> <ver>" constraint strings), splitting the rock name from its version constraint. build/test deps are flagged is_dev=true. Classified .rockspec -> lua so the file reaches the _cross_manifest cross-language pass; emits DEPENDS_ON + DEPENDS_ON_PACKAGE edges + SBOM package nodes like every other ecosystem. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.lua.tool.luarocks ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
