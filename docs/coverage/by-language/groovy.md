<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# groovy

**Frameworks**: 1 · **Tools**: 1 · **ORMs**: 1 · **Other**: 1

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
| [Grails / Ratpack (Groovy HTTP)](../detail/lang.groovy.framework.grails.md) | 🟡 3/7 | 🔴 0/1 | 🟡 3/4 | ✅ 1/1 | 🔴 0/24 | 🔴 0/13 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Spock](../detail/test.spock.md) | 🔴 | — | — | — | 🔴 | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [GORM (Grails ORM)](../detail/lang.groovy.orm.gorm.md) | 🟡 8/10 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Groovy (base language)](../detail/lang.groovy.base.md) | [language](../by-category/language.md) | ✅ | |
