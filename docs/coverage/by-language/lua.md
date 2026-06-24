<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# lua

**Frameworks**: 4 · **Tools**: 2 · **ORMs**: 0 · **Other**: 1

Back to [summary](../summary.md).

### Legend

Each group column shows `glyph covered/applicable` — **covered** = capabilities with extraction, **applicable** = covered + missing (not-applicable capabilities are excluded from both). The glyph is the group's **support level**:

| Glyph | Level | Meaning |
|---|---|---|
| ✅ | **Comprehensive** | every applicable capability is `full` — fixture-proven, resolves the general case |
| 🟢 | **Supported** | every applicable capability is extracted; some only *heuristically* (detected by pattern, not full AST/data-flow resolution) |
| 🟡 | **Partial** | some capabilities extracted, some still missing |
| 🔴 | **Not extracted** | nothing extracted yet |
| — | **N/A** | capability does not apply to this framework |

Examples: `🟢 20/20` = fully supported, some capabilities heuristic · `🟡 12/20` = 8 not yet extracted. Detail pages use the same palette **per cell** (✅ full · 🟢 heuristic/partial · 🔴 missing · — n/a).

## Frameworks


### Backend HTTP

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [Apache APISIX](../detail/lang.lua.framework.apisix.md) | 🟡 2/7 | ✅ 1/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/24 | 🟡 3/12 | |
| [Kong](../detail/lang.lua.framework.kong.md) | 🟡 2/7 | ✅ 1/1 | 🔴 0/4 | 🟢 1/1 | 🔴 0/24 | 🟡 7/12 | |
| [Lapis](../detail/lang.lua.framework.lapis.md) | 🟡 3/7 | ✅ 1/1 | — | ✅ 1/1 | 🟡 18/22 | 🟡 6/11 | |
| [OpenResty](../detail/lang.lua.framework.openresty.md) | 🟡 3/7 | ✅ 1/1 | — | ✅ 1/1 | 🟡 18/22 | 🟡 6/11 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [LuaRocks](../detail/lang.lua.tool.luarocks.md) | — | — | ✅ | ✅ | — | |
| [busted](../detail/test.busted.md) | ✅ | — | — | — | ✅ | |

## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Lua (base language)](../detail/lang.lua.base.md) | [language](../by-category/language.md) | ✅ | |
