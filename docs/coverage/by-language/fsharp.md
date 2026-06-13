<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# F#

**Frameworks**: 2 · **Tools**: 1 · **ORMs**: 0 · **Other**: 2

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
| [Giraffe / Saturn (F# HTTP)](../detail/lang.fsharp.framework.giraffe.md) | 🟡 3/7 | 🔴 0/1 | 🟢 4/4 | ✅ 1/1 | 🔴 0/24 | 🟡 2/13 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Fable Elmish/Feliz (F# frontend)](../detail/lang.fsharp.framework.elmish.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/24 | 🟡 6/14 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Expecto / xUnit (F#)](../detail/test.fsharp-expecto.md) | 🔴 | — | — | — | ✅ | |

## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [F#](../detail/lang.fsharp.core.md) | [language](../by-category/language.md) | 🟢 | |

### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [DataAnnotations (F# records)](../detail/lang.fsharp.validation.dataannotations.md) | 🟢 1/1 | ✅ 4/4 | |
