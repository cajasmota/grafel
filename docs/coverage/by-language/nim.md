<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# nim

**Frameworks**: 1 · **Tools**: 0 · **ORMs**: 4 · **Other**: 1

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
| [Jester / Prologue (Nim HTTP)](../detail/lang.nim.framework.jester.md) | 🟡 3/6 | 🔴 0/1 | 🔴 0/4 | ✅ 1/1 | 🔴 0/24 | 🔴 0/13 | |


## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Allographer (Nim query/schema builder)](../detail/lang.nim.orm.allographer.md) | 🟡 5/6 | |
| [Debby (Nim ORM)](../detail/lang.nim.orm.debby.md) | 🟡 5/9 | |
| [Norm (Nim ORM)](../detail/lang.nim.orm.norm.md) | 🟡 6/9 | |
| [ormin (Nim compile-time ORM)](../detail/lang.nim.orm.ormin.md) | 🟡 4/8 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Nim](../detail/lang.nim.core.md) | [language](../by-category/language.md) | 🟢 | |
