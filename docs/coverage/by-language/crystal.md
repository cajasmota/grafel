<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# crystal

**Frameworks**: 2 · **Tools**: 1 · **ORMs**: 5 · **Other**: 1

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
| [Kemal (Crystal HTTP)](../detail/lang.crystal.framework.kemal.md) | 🟡 3/7 | 🔴 0/1 | 🟡 3/4 | ✅ 1/1 | 🟡 13/24 | 🟡 3/10 | |
| [Lucky (Crystal web framework)](../detail/lang.crystal.framework.lucky.md) | 🟡 2/7 | 🔴 0/1 | 🔴 0/4 | ✅ 1/1 | 🔴 0/24 | 🔴 0/13 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [shards (Crystal)](../detail/pkg.shards.md) | — | — | ✅ | ✅ | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Avram (Lucky Crystal ORM)](../detail/lang.crystal.orm.avram.md) | ✅ 9/9 | |
| [Clear (Crystal ORM)](../detail/lang.crystal.orm.clear.md) | ✅ 9/9 | |
| [Crecto (Crystal ORM)](../detail/lang.crystal.orm.crecto.md) | ✅ 9/9 | |
| [Granite (Crystal ORM)](../detail/lang.crystal.orm.granite.md) | ✅ 10/10 | |
| [Jennifer (Crystal ORM)](../detail/lang.crystal.orm.jennifer.md) | ✅ 9/9 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Crystal](../detail/lang.crystal.core.md) | [language](../by-category/language.md) | 🟢 | |
