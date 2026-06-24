<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# elm

**Frameworks**: 1 · **Tools**: 1 · **ORMs**: 0 · **Other**: 0

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


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [The Elm Architecture (Elm frontend)](../detail/lang.elm.framework.tea.md) | 🔴 0/3 | ✅ 1/1 | 🔴 0/24 | 🟡 2/8 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [elm.json (Elm package manager)](../detail/lang.elm.tool.elm-json.md) | — | — | — | ✅ | — | |
